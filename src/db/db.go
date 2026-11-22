package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	conn             *sql.DB
	mu               sync.RWMutex
	securityReportMu sync.Mutex
)

const (
	// readTimeout bounds simple SELECT queries (AI.md PART 10 Query Timeouts).
	readTimeout = 5 * time.Second
	// writeTimeout bounds INSERT/UPDATE/DELETE queries (AI.md PART 10).
	writeTimeout = 10 * time.Second
)

// ConnectionConfig selects SQLite or a remote libSQL database.
type ConnectionConfig struct {
	Driver string
	URL    string
	Token  string
}

// readCtx returns a context with the standard read-query deadline.
func readCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), readTimeout)
}

// writeCtx returns a context with the standard write-query deadline.
func writeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), writeTimeout)
}

// Init opens (or creates) the SQLite database and runs schema migrations
func Init(dataDir string, configured ...ConnectionConfig) error {
	mu.Lock()
	defer mu.Unlock()
	connection := ConnectionConfig{Driver: "sqlite"}
	if len(configured) > 0 {
		connection = configured[0]
		if strings.TrimSpace(connection.Driver) == "" {
			connection.Driver = "sqlite"
		}
	}
	driverName := normalizeDatabaseDriver(connection.Driver)
	if driverName != "sqlite" && driverName != "libsql" {
		return fmt.Errorf("unsupported server.database.driver %q", connection.Driver)
	}

	// DATABASE_DIR is an init-only override (AI.md PART 5 "Init-Only Variables",
	// PART 26): when set it takes precedence over the default {data_dir}/db
	// location. Read once at startup.
	dsn := connection.URL
	if driverName == "sqlite" {
		dbDir := filepath.Join(dataDir, "db")
		if v := strings.TrimSpace(os.Getenv("DATABASE_DIR")); v != "" {
			dbDir = v
		}
		if err := os.MkdirAll(dbDir, 0750); err != nil {
			return fmt.Errorf("failed to create db directory: %w", err)
		}
		if dsn == "" {
			dsn = filepath.Join(dbDir, "server.db")
		}
	} else {
		var err error
		dsn, err = libSQLURL(dsn, connection.Token)
		if err != nil {
			return err
		}
	}
	c, err := sql.Open(registerMetricsDriverFor(driverName), dsn)
	if err != nil {
		return errors.New("failed to open configured database")
	}

	// Bound the connection pool. SQLite serializes writers, so a single open
	// connection avoids "database is locked" contention while WAL still allows
	// concurrent readers (AI.md PART 10).
	maxOpen := 1
	if driverName == "libsql" {
		maxOpen = 10
	}
	c.SetMaxOpenConns(maxOpen)
	c.SetMaxIdleConns(maxOpen)
	c.SetConnMaxLifetime(0)

	// Verify the connection is actually reachable before proceeding.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := c.PingContext(pingCtx); err != nil {
		pingCancel()
		_ = c.Close()
		return errors.New("failed to connect to configured database")
	}
	pingCancel()

	// Schema initialization and migrations use the AI.md PART 10 migration
	// timeout, separately from the short connection ping.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if driverName == "sqlite" {
		if _, err := c.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			_ = c.Close()
			return fmt.Errorf("failed to set WAL mode: %w", err)
		}
		if _, err := c.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
			_ = c.Close()
			return fmt.Errorf("failed to enable foreign keys: %w", err)
		}
	}

	// Reconcile schema drift from earlier releases before the CREATE TABLE IF
	// NOT EXISTS statements run — a table that already exists is never altered
	// by IF NOT EXISTS, so incompatible legacy tables are dropped first.
	if err := migrateSchema(ctx, c); err != nil {
		_ = c.Close()
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	if err := createSchema(ctx, c); err != nil {
		_ = c.Close()
		return fmt.Errorf("failed to create schema: %w", err)
	}

	conn = c
	return nil
}

func normalizeDatabaseDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "sqlite", "sqlite2", "sqlite3":
		return "sqlite"
	case "libsql", "turso":
		return "libsql"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

func libSQLURL(rawURL, token string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "libsql" && parsed.Scheme != "https") {
		return "", errors.New("server.database.url is invalid for libSQL; use libsql:// or https:// URL form")
	}
	query := parsed.Query()
	if token != "" && query.Get("authToken") == "" {
		query.Set("authToken", token)
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}

// Close closes the database connection
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		current := conn
		conn = nil
		return current.Close()
	}
	return nil
}

// ConnectionStats reports the current database pool state for operational
// metrics without exposing the connection itself to other packages.
func ConnectionStats() (open, inUse int) {
	mu.RLock()
	current := conn
	mu.RUnlock()
	if current == nil {
		return 0, 0
	}
	stats := current.Stats()
	return stats.OpenConnections, stats.InUse
}

// migrateSchema drops tables whose on-disk shape predates the current schema
// so the subsequent CREATE TABLE IF NOT EXISTS can recreate them correctly.
func migrateSchema(ctx context.Context, c *sql.DB) error {
	// The original server_scheduler_state used (task, last_run, next_run,
	// status). PART 18 replaced it with a task_id-keyed table carrying run/fail
	// counts and status detail. The table was never populated (the scheduler
	// was dead code), so dropping it loses no data.
	hasTaskID, err := columnExists(ctx, c, "server_scheduler_state", "task_id")
	if err != nil {
		return err
	}
	tableExists, err := columnExists(ctx, c, "server_scheduler_state", "task")
	if err != nil {
		return err
	}
	if tableExists && !hasTaskID {
		if _, err := c.ExecContext(ctx, "DROP TABLE server_scheduler_state"); err != nil {
			return fmt.Errorf("failed to drop legacy scheduler table: %w", err)
		}
	}
	securityTable, err := columnExists(ctx, c, "security_reports", "tracking_id")
	if err != nil {
		return err
	}
	if securityTable {
		hasBody, err := columnExists(ctx, c, "security_reports", "body_enc")
		if err != nil {
			return err
		}
		if !hasBody {
			hasLegacyBody, err := columnExists(ctx, c, "security_reports", "encrypted_body")
			if err != nil {
				return err
			}
			if hasLegacyBody {
				if _, err := c.ExecContext(ctx, "ALTER TABLE security_reports RENAME COLUMN encrypted_body TO body_enc"); err != nil {
					return err
				}
			}
		}
		for _, column := range []struct{ name, definition string }{
			{"security_id", "TEXT NOT NULL DEFAULT ''"}, {"body_alg", "TEXT NOT NULL DEFAULT 'aes-256-gcm'"}, {"contact_enc", "BLOB"},
			{"credit_pref", "TEXT NOT NULL DEFAULT 'no'"}, {"credit_name", "TEXT"}, {"cve_requested", "BOOLEAN NOT NULL DEFAULT FALSE"},
			{"researcher_comment", "TEXT NOT NULL DEFAULT ''"}, {"updated_at", "DATETIME NOT NULL DEFAULT ''"}, {"token_last_used", "DATE"},
		} {
			exists, err := columnExists(ctx, c, "security_reports", column.name)
			if err != nil {
				return err
			}
			if !exists {
				if _, err := c.ExecContext(ctx, "ALTER TABLE security_reports ADD COLUMN "+column.name+" "+column.definition); err != nil {
					return err
				}
			}
		}
		hasLegacyComment, err := columnExists(ctx, c, "security_reports", "status_comment")
		if err != nil {
			return err
		}
		if hasLegacyComment {
			if _, err := c.ExecContext(ctx, "UPDATE security_reports SET researcher_comment = status_comment WHERE researcher_comment = '' AND status_comment <> ''"); err != nil {
				return err
			}
		}
		hasLegacyStatusDay, err := columnExists(ctx, c, "security_reports", "last_status_access_day")
		if err != nil {
			return err
		}
		if hasLegacyStatusDay {
			if _, err := c.ExecContext(ctx, "UPDATE security_reports SET token_last_used = last_status_access_day WHERE token_last_used IS NULL AND last_status_access_day <> ''"); err != nil {
				return err
			}
		}
		if _, err := c.ExecContext(ctx, `UPDATE security_reports SET updated_at = COALESCE(NULLIF(updated_at, ''), created_at, CURRENT_TIMESTAMP)
			WHERE updated_at IS NULL OR updated_at = ''`); err != nil {
			return err
		}
	}
	hasLegacyThanksID, err := columnExists(ctx, c, "security_thanks", "id")
	if err != nil {
		return err
	}
	if hasLegacyThanksID {
		tx, err := c.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, statement := range []string{
			`CREATE TABLE security_thanks_new (tracking_id TEXT PRIMARY KEY, credit TEXT NOT NULL, severity TEXT NOT NULL, disclosed_on DATE NOT NULL)`,
			`INSERT OR IGNORE INTO security_thanks_new (tracking_id, credit, severity, disclosed_on) SELECT tracking_id, credit, severity, disclosed_on FROM security_thanks`,
			`DROP TABLE security_thanks`,
			`ALTER TABLE security_thanks_new RENAME TO security_thanks`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate security acknowledgments: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit security acknowledgment migration: %w", err)
		}
	}
	return nil
}

// columnExists reports whether the named column is present on the table. A
// missing table yields false with no error.
func columnExists(ctx context.Context, c *sql.DB, table, column string) (bool, error) {
	rows, err := c.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dfltValue        sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, rows.Err()
		}
	}
	return false, rows.Err()
}

func createSchema(ctx context.Context, c *sql.DB) error {
	_, err := c.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS server_admin_credentials (
    id          INTEGER PRIMARY KEY,
    username    TEXT NOT NULL,
    pass_hash   TEXT NOT NULL,
    token_hash  TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS server_admin_sessions (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL,
    ip          TEXT
);

CREATE TABLE IF NOT EXISTS server_config (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_by  TEXT DEFAULT 'admin'
);

CREATE TABLE IF NOT EXISTS server_cluster_state (
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    node_id     TEXT NOT NULL DEFAULT '',
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (key, node_id)
);

CREATE TABLE IF NOT EXISTS server_scheduler_state (
    task_id     TEXT PRIMARY KEY,
    task_name   TEXT NOT NULL DEFAULT '',
    schedule    TEXT NOT NULL DEFAULT '',
    last_run    DATETIME,
    last_status TEXT NOT NULL DEFAULT '',
    last_error  TEXT NOT NULL DEFAULT '',
    next_run    DATETIME,
    run_count   INTEGER NOT NULL DEFAULT 0,
    fail_count  INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS server_nodes (
    id          TEXT PRIMARY KEY,
    address     TEXT NOT NULL,
    joined_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_seen   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS server_join_tokens (
    token_hash  TEXT PRIMARY KEY,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME
);

CREATE TABLE IF NOT EXISTS app_secrets (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pgp_key_metadata (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    fingerprint           TEXT NOT NULL,
    created_at             DATETIME NOT NULL,
    expires_at             DATETIME NOT NULL,
    last_rotated_at        DATETIME,
    keyservers_published   TEXT NOT NULL DEFAULT '[]',
    revoked                BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS security_reports (
    tracking_id     TEXT PRIMARY KEY,
    security_id     TEXT NOT NULL DEFAULT '',
    token_hash      TEXT NOT NULL,
    body_enc        BLOB NOT NULL,
    body_alg        TEXT NOT NULL DEFAULT 'aes-256-gcm',
    contact_enc     BLOB,
    severity        TEXT NOT NULL,
    component       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'received',
    researcher_comment TEXT NOT NULL DEFAULT '',
    disclosure_date TEXT NOT NULL DEFAULT '',
    credit_pref     TEXT NOT NULL DEFAULT 'no',
    credit_name     TEXT,
    cve_requested   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    token_last_used DATE,
    closed_at       DATETIME
);
CREATE INDEX IF NOT EXISTS idx_security_reports_status ON security_reports(status, created_at);

CREATE TABLE IF NOT EXISTS security_researcher_acknowledgments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    year            INTEGER NOT NULL,
    display_name    TEXT NOT NULL,
    short_credit    TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS security_thanks (
    tracking_id TEXT PRIMARY KEY,
    credit      TEXT NOT NULL,
    severity    TEXT NOT NULL,
    disclosed_on DATE NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_security_thanks_disclosed_on ON security_thanks(disclosed_on DESC);

CREATE TABLE IF NOT EXISTS user_accounts (
    id          TEXT PRIMARY KEY,
    username    TEXT UNIQUE NOT NULL,
    pass_hash   TEXT NOT NULL,
    email       TEXT UNIQUE,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES user_accounts(id),
    token_hash  TEXT NOT NULL,
    name        TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME
);

CREATE TABLE IF NOT EXISTS user_sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES user_accounts(id),
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL,
    ip          TEXT
);

CREATE TABLE IF NOT EXISTS user_invites (
    code        TEXT PRIMARY KEY,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME,
    used_by     TEXT
);

CREATE TABLE IF NOT EXISTS owners (
    id           TEXT PRIMARY KEY,
    created_at   INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    last_seen_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id       TEXT NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('primary', 'linked')),
    name           TEXT,
    token_hash     TEXT NOT NULL UNIQUE,
    token_prefix   TEXT NOT NULL,
    token_enc      TEXT NOT NULL,
    created_at     INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    rotated_at     INTEGER,
    expires_at     INTEGER,
    last_used_at   INTEGER,
    revoked_at     INTEGER,
    revoked_reason TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_primary ON api_tokens(owner_id) WHERE kind = 'primary' AND revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_prefix ON api_tokens(token_prefix);
CREATE INDEX IF NOT EXISTS idx_api_tokens_owner ON api_tokens(owner_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_active ON api_tokens(revoked_at) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_owner_name ON api_tokens(owner_id, name) WHERE name IS NOT NULL;
`)
	return err
}

// InstallationSecret returns the persistent 32-byte installation secret,
// generating and storing it on first use.
func InstallationSecret() ([]byte, error) {
	mu.RLock()
	if conn == nil {
		mu.RUnlock()
		return nil, fmt.Errorf("database is not initialized")
	}
	c := conn
	mu.RUnlock()

	ctx, cancel := writeCtx()
	defer cancel()
	var encoded string
	err := c.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE key = ?", "installation_secret").Scan(&encoded)
	if err == nil {
		secret, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil || len(secret) != 32 {
			return nil, fmt.Errorf("stored installation secret is invalid")
		}
		return secret, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	encoded = base64.StdEncoding.EncodeToString(secret)
	if _, err := c.ExecContext(ctx, "INSERT INTO app_secrets(key, value) VALUES(?, ?)", "installation_secret", encoded); err != nil {
		return nil, err
	}
	return secret, nil
}

// SaveSecurityReport stores report metadata and ciphertext without plaintext.
func SaveSecurityReport(trackingID, securityID, tokenHash string, bodyEnc []byte, bodyAlg string, contactEnc []byte, severity, component, creditPref, creditName string, cveRequested bool) error {
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	mu.RLock()
	if conn == nil {
		mu.RUnlock()
		return fmt.Errorf("database is not initialized")
	}
	c := conn
	mu.RUnlock()
	ctx, cancel := writeCtx()
	defer cancel()
	var storedCreditName interface{}
	if creditName != "" {
		storedCreditName = creditName
	}
	_, err := c.ExecContext(ctx, `INSERT INTO security_reports
	(tracking_id, security_id, token_hash, body_enc, body_alg, contact_enc, severity, component, credit_pref, credit_name, cve_requested)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, trackingID, securityID, tokenHash, bodyEnc, bodyAlg, contactEnc, severity, component, creditPref, storedCreditName, cveRequested)
	return err
}

// ClaimSecurityReportStatus validates the emailed token and permits one status
// page view per UTC day for each report.
func ClaimSecurityReportStatus(trackingID, tokenHash, utcDay string) (string, string, string, error) {
	mu.RLock()
	if conn == nil {
		mu.RUnlock()
		return "", "", "", fmt.Errorf("database is not initialized")
	}
	c := conn
	mu.RUnlock()
	ctx, cancel := writeCtx()
	defer cancel()
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return "", "", "", err
	}
	defer func() { _ = tx.Rollback() }()
	var status, comment, disclosureDate, lastDay string
	var closedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT status, researcher_comment, disclosure_date, COALESCE(token_last_used, ''), closed_at
FROM security_reports WHERE tracking_id = ? AND token_hash = ?`, trackingID, tokenHash).Scan(&status, &comment, &disclosureDate, &lastDay, &closedAt)
	if err != nil {
		return "", "", "", err
	}
	if lastDay == utcDay {
		return "", "", "", fmt.Errorf("status link already used today")
	}
	if closedAt.Valid && time.Since(closedAt.Time) > 30*24*time.Hour {
		return "", "", "", fmt.Errorf("status link expired")
	}
	result, err := tx.ExecContext(ctx, "UPDATE security_reports SET token_last_used = ? WHERE tracking_id = ? AND token_hash = ? AND COALESCE(token_last_used, '') <> ?", utcDay, trackingID, tokenHash, utcDay)
	if err != nil {
		return "", "", "", err
	}
	claimed, err := result.RowsAffected()
	if err != nil || claimed != 1 {
		return "", "", "", fmt.Errorf("status link already used today")
	}
	if err := tx.Commit(); err != nil {
		return "", "", "", err
	}
	return status, comment, disclosureDate, nil
}

type SecurityThanksEntry struct {
	Credit   string `json:"credit"`
	Date     string `json:"date"`
	Severity string `json:"severity"`
}

func ListSecurityThanks() ([]SecurityThanksEntry, error) {
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return []SecurityThanksEntry{}, nil
	}
	ctx, cancel := readCtx()
	defer cancel()
	rows, err := conn.QueryContext(ctx, `SELECT credit, disclosed_on, severity FROM security_thanks ORDER BY disclosed_on DESC, tracking_id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list security acknowledgments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	entries := make([]SecurityThanksEntry, 0)
	for rows.Next() {
		var entry SecurityThanksEntry
		if err := rows.Scan(&entry.Credit, &entry.Date, &entry.Severity); err != nil {
			return nil, fmt.Errorf("scan security acknowledgment: %w", err)
		}
		if disclosed, err := time.Parse(time.RFC3339, entry.Date); err == nil {
			entry.Date = disclosed.UTC().Format("2006-01-02")
		} else if len(entry.Date) > len("2006-01-02") {
			entry.Date = entry.Date[:len("2006-01-02")]
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read security acknowledgments: %w", err)
	}
	return entries, nil
}
