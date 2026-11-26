package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	mrand "math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	conn             *sql.DB
	readConn         *sql.DB
	remoteReadConn   *sql.DB
	sqliteConn       bool
	localSQLitePath  string
	localCache       *remoteSQLiteCache
	mu               sync.RWMutex
	securityReportMu sync.Mutex
)

const (
	// readTimeout bounds simple SELECT queries (AI.md PART 10 Query Timeouts).
	readTimeout = 5 * time.Second
	// writeTimeout bounds INSERT/UPDATE/DELETE queries (AI.md PART 10).
	writeTimeout = 10 * time.Second
)

type queryExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ConnectionConfig selects SQLite or a remote libSQL database.
type ConnectionConfig struct {
	Driver string
	URL    string
	Token  string
	Pool   PoolConfig
	Cache  CacheConfig
}

// CacheConfig enables an optional local SQLite mirror for a remote libSQL store.
type CacheConfig struct {
	Enabled      bool
	Path         string
	SyncInterval time.Duration
	OfflineMode  bool
	MaxAge       time.Duration
}

// PoolConfig configures reusable database connections.
type PoolConfig struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
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
	localSQLitePath = ""
	connection := ConnectionConfig{Driver: "sqlite"}
	if len(configured) > 0 {
		connection = configured[0]
		if strings.TrimSpace(connection.Driver) == "" {
			connection.Driver = "sqlite"
		}
	}
	pool := connection.Pool
	if pool.MaxOpen < 1 {
		pool.MaxOpen = 25
	}
	if pool.MaxIdle < 1 || pool.MaxIdle > pool.MaxOpen {
		pool.MaxIdle = min(5, pool.MaxOpen)
	}
	if pool.MaxLifetime <= 0 {
		pool.MaxLifetime = 5 * time.Minute
	}
	if pool.MaxIdleTime <= 0 {
		pool.MaxIdleTime = time.Minute
	}
	driverName := normalizeDatabaseDriver(connection.Driver)
	if driverName != "sqlite" && driverName != "libsql" {
		return fmt.Errorf("unsupported server.database.driver %q", connection.Driver)
	}
	if driverName == "sqlite" && connection.Cache.Enabled {
		return errors.New("server.database.cache is supported only with server.database.driver=libsql")
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
		if connection.Cache.Enabled {
			connection.Cache.Path = ResolveLocalCachePath(dataDir, connection.Cache.Path)
		}
		var err error
		dsn, err = libSQLURL(dsn, connection.Token)
		if err != nil {
			return err
		}
	}
	writeDSN, readDSN := dsn, dsn
	sqlitePath := ""
	if driverName == "sqlite" {
		sqlitePath = sqliteDatabasePath(dsn)
		writeDSN = sqliteDSN(dsn, "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_txlock=immediate")
		readDSN = sqliteDSN(dsn, "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=query_only(1)")
	}
	c, err := sql.Open(registerMetricsDriverFor(driverName), writeDSN)
	if err != nil {
		return errors.New("failed to open configured database")
	}
	r, err := sql.Open(registerMetricsDriverFor(driverName), readDSN)
	if err != nil {
		_ = c.Close()
		return errors.New("failed to open configured database reader")
	}

	// Bound the connection pool. SQLite serializes writers, so a single open
	// connection avoids "database is locked" contention while WAL still allows
	// concurrent readers (AI.md PART 10).
	writeMaxOpen := pool.MaxOpen
	writeMaxIdle := pool.MaxIdle
	if driverName == "sqlite" {
		writeMaxOpen = 1
		writeMaxIdle = 1
	}
	c.SetMaxOpenConns(writeMaxOpen)
	c.SetMaxIdleConns(writeMaxIdle)
	c.SetConnMaxLifetime(writerMaxLifetime(driverName, pool.MaxLifetime))
	c.SetConnMaxIdleTime(pool.MaxIdleTime)
	r.SetMaxOpenConns(pool.MaxOpen)
	r.SetMaxIdleConns(pool.MaxIdle)
	r.SetConnMaxLifetime(pool.MaxLifetime)
	r.SetConnMaxIdleTime(pool.MaxIdleTime)

	// Verify the connection is actually reachable before proceeding.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	remoteOnline := true
	if err := c.PingContext(pingCtx); err != nil {
		if driverName != "libsql" || !connection.Cache.Enabled || !connection.Cache.OfflineMode {
			pingCancel()
			_ = c.Close()
			_ = r.Close()
			return errors.New("failed to connect to configured database")
		}
		remoteOnline = false
	}
	if remoteOnline {
		if err := r.PingContext(pingCtx); err != nil {
			if driverName != "libsql" || !connection.Cache.Enabled || !connection.Cache.OfflineMode {
				pingCancel()
				_ = c.Close()
				_ = r.Close()
				return errors.New("failed to connect to configured database reader")
			}
			remoteOnline = false
		}
	}
	if err := pingCtx.Err(); err != nil && remoteOnline {
		pingCancel()
		_ = c.Close()
		_ = r.Close()
		return errors.New("failed to connect to configured database")
	}
	pingCancel()

	// Schema initialization and migrations use the AI.md PART 10 migration
	// timeout, separately from the short connection ping.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if driverName == "sqlite" {
		var journalMode string
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
			_ = c.Close()
			_ = r.Close()
			return fmt.Errorf("failed to set WAL mode: %w", err)
		}
		if !strings.EqualFold(journalMode, "wal") {
			_ = c.Close()
			_ = r.Close()
			return fmt.Errorf("SQLite database at %s does not support WAL mode (reported %q)", dsn, journalMode)
		}
		if _, err := c.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
			_ = c.Close()
			_ = r.Close()
			return fmt.Errorf("failed to enable foreign keys: %w", err)
		}
	}

	if remoteOnline {
		// Reconcile schema drift and create the schema in one write transaction.
		if err := writeWithDB(ctx, c, driverName == "sqlite", "database schema", func(tx *sql.Tx) error {
			if err := migrateSchema(ctx, tx); err != nil {
				return fmt.Errorf("failed to migrate schema: %w", err)
			}
			if err := createSchema(ctx, tx); err != nil {
				return fmt.Errorf("failed to create schema: %w", err)
			}
			for _, statement := range []string{
				"CREATE UNIQUE INDEX IF NOT EXISTS idx_security_thanks_tracking_id ON security_thanks(tracking_id)",
				"CREATE UNIQUE INDEX IF NOT EXISTS idx_app_secrets_name ON app_secrets(name) WHERE name IS NOT NULL",
			} {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					return fmt.Errorf("failed to create database index: %w", err)
				}
			}
			if err := ensureColumn(ctx, tx, "app_secrets", "name", "TEXT"); err != nil {
				return fmt.Errorf("failed to migrate secret names: %w", err)
			}
			if err := migrateCanonicalState(ctx, tx); err != nil {
				return fmt.Errorf("failed to migrate canonical server state: %w", err)
			}
			if err := ensureColumn(ctx, tx, "api_tokens", "token_last4", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to migrate owner token confirmation metadata: %w", err)
			}
			return nil
		}); err != nil {
			_ = c.Close()
			_ = r.Close()
			return err
		}
	}

	conn = c
	remoteReadConn = r
	readConn = r
	sqliteConn = driverName == "sqlite"
	localSQLitePath = sqlitePath
	localCache = nil
	if driverName == "libsql" && connection.Cache.Enabled {
		cacheRemote, err := sql.Open("libsql", dsn)
		if err != nil {
			conn, readConn, remoteReadConn = nil, nil, nil
			_ = c.Close()
			_ = r.Close()
			return errors.New("failed to open local cache remote connection")
		}
		cache, _, err := openRemoteSQLiteCache(ctx, cacheRemote, dsn, connection.Cache)
		if err != nil {
			conn, readConn, remoteReadConn = nil, nil, nil
			_ = c.Close()
			_ = r.Close()
			_ = cacheRemote.Close()
			return fmt.Errorf("initialize local database cache: %w", err)
		}
		localCache = cache
		readConn = cache.reader
	}
	if err := loadRateLimitEvents(ctx, readConn); err != nil {
		conn, readConn = nil, nil
		sqliteConn = false
		_ = c.Close()
		_ = r.Close()
		return fmt.Errorf("failed to load rate-limit counters: %w", err)
	}
	if err := loadHotTokenUses(ctx, readConn); err != nil {
		conn, readConn = nil, nil
		sqliteConn = false
		_ = c.Close()
		_ = r.Close()
		return fmt.Errorf("failed to load owner-token counters: %w", err)
	}
	startHotCounterFlusher()
	return nil
}

func writerMaxLifetime(driver string, configured time.Duration) time.Duration {
	if driver == "sqlite" {
		return 0
	}
	return configured
}

func sqliteDSN(raw, options string) string {
	dsn := raw
	if dsn != ":memory:" && !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + options
}

// migrateCanonicalState copies legacy state into the canonical schema without
// deleting any source rows or tables. Each operation is safe to repeat.
func migrateCanonicalState(ctx context.Context, c queryExecutor) error {
	legacyScheduler, err := columnExists(ctx, c, "server_scheduler_state", "task_id")
	if err != nil {
		return err
	}
	if legacyScheduler {
		_, err = c.ExecContext(ctx, `INSERT INTO scheduler_tasks
			(id, name, enabled, schedule, last_run, next_run, last_status, last_error, run_count, fail_count)
			SELECT task_id, task_name, enabled, schedule,
				CASE WHEN last_run IS NULL OR last_run = '' THEN NULL ELSE CAST(strftime('%s', last_run) AS INTEGER) END,
				CASE WHEN next_run IS NULL OR next_run = '' THEN NULL ELSE CAST(strftime('%s', next_run) AS INTEGER) END,
				last_status, last_error, run_count, fail_count
			FROM server_scheduler_state WHERE task_id <> ''
			ON CONFLICT(id) DO NOTHING`)
		if err != nil {
			return err
		}
	} else {
		legacyTask, err := columnExists(ctx, c, "server_scheduler_state", "task")
		if err != nil {
			return err
		}
		if legacyTask {
			_, err = c.ExecContext(ctx, `INSERT INTO scheduler_tasks
				(id, name, enabled, schedule, last_run, next_run, last_status, last_error)
				SELECT task, task, 1, '',
					CASE WHEN last_run IS NULL OR last_run = '' THEN NULL ELSE CAST(strftime('%s', last_run) AS INTEGER) END,
					CASE WHEN next_run IS NULL OR next_run = '' THEN NULL ELSE CAST(strftime('%s', next_run) AS INTEGER) END,
					COALESCE(status, ''), ''
				FROM server_scheduler_state WHERE task <> ''
				ON CONFLICT(id) DO NOTHING`)
			if err != nil {
				return err
			}
		}
	}
	legacySecretKey, err := columnExists(ctx, c, "app_secrets", "key")
	if err != nil {
		return err
	}
	canonicalSecretName, err := columnExists(ctx, c, "app_secrets", "name")
	if err != nil {
		return err
	}
	if legacySecretKey && canonicalSecretName {
		_, err = c.ExecContext(ctx, "UPDATE app_secrets SET name = key WHERE name IS NULL OR name = ''")
		if err != nil {
			return err
		}
	}
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
	stopHotCounterFlusher()
	if client := rateLimitRedis.Swap(nil); client != nil {
		if err := client.Close(); err != nil {
			return err
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		current := conn
		conn = nil
		reader := readConn
		readConn = nil
		remoteReader := remoteReadConn
		remoteReadConn = nil
		cache := localCache
		localCache = nil
		isSQLite := sqliteConn
		sqliteConn = false
		localSQLitePath = ""
		if cache != nil {
			if err := cache.close(); err != nil {
				_ = current.Close()
				if remoteReader != nil {
					_ = remoteReader.Close()
				}
				return err
			}
		}
		if remoteReader != nil && remoteReader != reader && remoteReader != current {
			if err := remoteReader.Close(); err != nil {
				_ = current.Close()
				return err
			}
		}
		if cache == nil && reader != nil && reader != current {
			if isSQLite {
				if _, err := current.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
					_ = current.Close()
					_ = reader.Close()
					return err
				}
			}
			if err := reader.Close(); err != nil {
				_ = current.Close()
				return err
			}
		}
		return current.Close()
	}
	return nil
}

// SnapshotSQLite creates a consistent point-in-time copy through SQLite's
// online backup mechanism. The caller owns and removes destinationPath.
func SnapshotSQLite(ctx context.Context, destinationPath string) error {
	mu.RLock()
	writer, isSQLite := conn, sqliteConn
	mu.RUnlock()
	if writer == nil {
		return sql.ErrConnDone
	}
	if !isSQLite {
		return errors.New("online SQLite snapshot requested for a remote database")
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return fmt.Errorf("snapshot destination already exists: %s", destinationPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if _, err := writer.ExecContext(ctx, "VACUUM INTO ?", destinationPath); err != nil {
		return fmt.Errorf("create SQLite online backup: %w", err)
	}
	return nil
}

// LocalDatabasePath resolves the configured SQLite database file location.
func LocalDatabasePath(dataDir string) string {
	mu.RLock()
	path, initialized := localSQLitePath, conn != nil && sqliteConn
	mu.RUnlock()
	if initialized && path != "" {
		return path
	}
	dbDir := filepath.Join(dataDir, "db")
	if override := strings.TrimSpace(os.Getenv("DATABASE_DIR")); override != "" {
		dbDir = override
	}
	return filepath.Join(dbDir, "server.db")
}

func sqliteDatabasePath(dsn string) string {
	if dsn == "" || dsn == ":memory:" {
		return ""
	}
	path := dsn
	if strings.HasPrefix(dsn, "file:") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			path = parsed.Path
			if parsed.Opaque != "" {
				path = parsed.Opaque
			}
			if parsed.Host != "" {
				path = "//" + parsed.Host + path
			}
		}
	}
	if path == "" || path == ":memory:" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path, _ = filepath.Abs(path)
	}
	return filepath.Clean(path)
}

// ResolveLocalCachePath places relative cache paths under the application data directory.
func ResolveLocalCachePath(dataDir, configuredPath string) string {
	path := strings.TrimSpace(os.ExpandEnv(configuredPath))
	if path == "" {
		path = filepath.Join(dataDir, "db", "cache.db")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(dataDir, path)
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}

// IsSQLite reports whether the initialized store uses a local SQLite file.
func IsSQLite() bool {
	mu.RLock()
	defer mu.RUnlock()
	return conn != nil && sqliteConn
}

// LocalCachePath returns the configured local libSQL cache file location.
func LocalCachePath(dataDir string) string {
	mu.RLock()
	cache := localCache
	mu.RUnlock()
	if cache != nil {
		return cache.path
	}
	return filepath.Join(dataDir, "db", "cache.db")
}

// Read runs fn against the query-only handle with the standard read deadline.
// Callers never receive the writer through the read path.
func Read(ctx context.Context, fn func(context.Context, *sql.DB) error) error {
	mu.RLock()
	reader := readConn
	mu.RUnlock()
	if reader == nil {
		return sql.ErrConnDone
	}
	bounded, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return fn(bounded, reader)
}

// Write executes one short transaction and retries SQLite busy/locked failures
// with bounded exponential backoff and jitter.
func Write(ctx context.Context, fn func(*sql.Tx) error) error {
	caller := "unknown"
	if pc, _, _, ok := runtime.Caller(1); ok {
		if fn := runtime.FuncForPC(pc); fn != nil {
			caller = fn.Name()
		}
	}
	mu.RLock()
	writer := conn
	mu.RUnlock()
	if writer == nil {
		return sql.ErrConnDone
	}
	return writeWithDB(ctx, writer, sqliteConn, caller, fn)
}

func writeWithDB(ctx context.Context, writer *sql.DB, isSQLite bool, caller string, fn func(*sql.Tx) error) error {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	var err error
	hadBusy := false
	for attempt := 0; attempt < 5; attempt++ {
		tx, beginErr := writer.BeginTx(ctx, nil)
		if beginErr != nil {
			err = beginErr
		} else if callbackErr := fn(tx); callbackErr != nil {
			_ = tx.Rollback()
			err = callbackErr
		} else {
			err = tx.Commit()
		}
		busy := isSQLite && isSQLiteBusy(err)
		hadBusy = hadBusy || busy
		if !busy || attempt == 4 {
			waited := time.Since(started)
			recordWriteWait(waited, hadBusy)
			if waited > time.Second {
				log.Printf("WARN db.busy wait_ms=%d caller=%s", waited.Milliseconds(), caller)
			}
			return err
		}
		wait := 25*time.Millisecond*time.Duration(1<<attempt) + time.Duration(mrand.Int64N(int64(25*time.Millisecond)))
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	return err
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "sqlite_locked") || strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
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
	mu.RLock()
	reader := readConn
	mu.RUnlock()
	if reader != nil {
		readStats := reader.Stats()
		return stats.OpenConnections + readStats.OpenConnections, stats.InUse + readStats.InUse
	}
	return stats.OpenConnections, stats.InUse
}

// migrateSchema drops tables whose on-disk shape predates the current schema
// so the subsequent CREATE TABLE IF NOT EXISTS can recreate them correctly.
func migrateSchema(ctx context.Context, c queryExecutor) error {
	// Legacy scheduler state is copied into scheduler_tasks after the canonical
	// table is created. Keep the source table intact: schema updates are additive.
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
	// Older builds stored an integer id alongside tracking_id. Keep that
	// historical column and data; the canonical lookup key remains tracking_id.
	return nil
}

// columnExists reports whether the named column is present on the table. A
// missing table yields false with no error.
func columnExists(ctx context.Context, c queryExecutor, table, column string) (bool, error) {
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

func createSchema(ctx context.Context, c queryExecutor) error {
	_, err := c.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS rate_limits (
    key          TEXT PRIMARY KEY,
    count        INTEGER NOT NULL DEFAULT 1,
    window_start INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at   INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_rate_limits_window ON rate_limits(window_start);
CREATE TABLE IF NOT EXISTS rate_limit_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    key          TEXT NOT NULL,
    occurred_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rate_limit_events_key_time ON rate_limit_events(key, occurred_at);

CREATE TABLE IF NOT EXISTS audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    level       TEXT NOT NULL DEFAULT 'info',
    category    TEXT NOT NULL,
    action      TEXT NOT NULL,
    actor_ip    TEXT,
    target_type TEXT,
    target_id   TEXT,
    details     TEXT,
    success     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_category ON audit_log(category);
CREATE INDEX IF NOT EXISTS idx_audit_actor_ip ON audit_log(actor_ip);
CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_log(target_type, target_id);

CREATE TABLE IF NOT EXISTS scheduler_tasks (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    schedule    TEXT NOT NULL,
    last_run    INTEGER,
    next_run    INTEGER,
    last_status TEXT,
    last_error  TEXT,
    run_count   INTEGER NOT NULL DEFAULT 0,
    fail_count  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS scheduler_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     TEXT NOT NULL,
    started_at  INTEGER NOT NULL,
    finished_at INTEGER,
    status      TEXT NOT NULL,
    error       TEXT,
    duration_ms INTEGER
);
CREATE INDEX IF NOT EXISTS idx_scheduler_history_task ON scheduler_history(task_id);
CREATE INDEX IF NOT EXISTS idx_scheduler_history_started ON scheduler_history(started_at);

CREATE TABLE IF NOT EXISTS backups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    filename    TEXT NOT NULL UNIQUE,
    filepath    TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    type        TEXT NOT NULL DEFAULT 'auto',
    created_at  INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    checksum    TEXT,
    notes       TEXT
);
CREATE INDEX IF NOT EXISTS idx_backups_created ON backups(created_at);

CREATE TABLE IF NOT EXISTS ip_blocks (
    ip            TEXT PRIMARY KEY,
    cidr          TEXT,
    type          TEXT NOT NULL DEFAULT 'temporary',
    reason        TEXT NOT NULL,
    blocked_at    INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    offense_count INTEGER NOT NULL DEFAULT 1,
    auto_blocked  INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_ip_blocks_expiry ON ip_blocks(expires_at);

CREATE TABLE IF NOT EXISTS app_secrets (
    name        TEXT PRIMARY KEY,
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

CREATE TABLE IF NOT EXISTS security_thanks (
    tracking_id TEXT PRIMARY KEY,
    credit      TEXT NOT NULL,
    severity    TEXT NOT NULL,
    disclosed_on DATE NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_security_thanks_disclosed_on ON security_thanks(disclosed_on DESC);

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
    token_last4    TEXT NOT NULL DEFAULT '',
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

func ensureColumn(ctx context.Context, c queryExecutor, table, column, definition string) error {
	exists, err := columnExists(ctx, c, table, column)
	if err != nil || exists {
		return err
	}
	_, err = c.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition)
	return err
}

// InstallationSecret returns the persistent 32-byte installation secret,
// generating and storing it on first use.
func InstallationSecret() ([]byte, error) {
	mu.RLock()
	reader := readConn
	if reader == nil {
		mu.RUnlock()
		return nil, fmt.Errorf("database is not initialized")
	}
	mu.RUnlock()

	ctx, cancel := readCtx()
	defer cancel()
	var encoded string
	err := reader.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", "installation_secret").Scan(&encoded)
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
	candidate := base64.StdEncoding.EncodeToString(secret)
	writeContext, writeCancel := writeCtx()
	defer writeCancel()
	if err := Write(writeContext, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(writeContext, "INSERT INTO app_secrets(name, value) VALUES(?, ?) ON CONFLICT(name) DO NOTHING", "installation_secret", candidate)
		return err
	}); err != nil {
		return nil, err
	}
	if err := reader.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", "installation_secret").Scan(&encoded); err != nil {
		return nil, err
	}
	secret, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(secret) != 32 {
		return nil, fmt.Errorf("stored installation secret is invalid")
	}
	return secret, nil
}

// SaveSecurityReport stores report metadata and ciphertext without plaintext.
func SaveSecurityReport(trackingID, securityID, tokenHash string, bodyEnc []byte, bodyAlg string, contactEnc []byte, severity, component, creditPref, creditName string, cveRequested bool) error {
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	ctx, cancel := writeCtx()
	defer cancel()
	var storedCreditName interface{}
	if creditName != "" {
		storedCreditName = creditName
	}
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO security_reports
	(tracking_id, security_id, token_hash, body_enc, body_alg, contact_enc, severity, component, credit_pref, credit_name, cve_requested)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, trackingID, securityID, tokenHash, bodyEnc, bodyAlg, contactEnc, severity, component, creditPref, storedCreditName, cveRequested)
		return err
	})
}

// ClaimSecurityReportStatus validates the emailed token and permits one status
// page view per UTC day for each report.
func ClaimSecurityReportStatus(trackingID, tokenHash, utcDay string) (string, string, string, error) {
	ctx, cancel := writeCtx()
	defer cancel()
	var status, comment, disclosureDate, lastDay string
	var closedAt sql.NullTime
	err := Write(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT status, researcher_comment, disclosure_date, COALESCE(token_last_used, ''), closed_at
FROM security_reports WHERE tracking_id = ? AND token_hash = ?`, trackingID, tokenHash).Scan(&status, &comment, &disclosureDate, &lastDay, &closedAt); err != nil {
			return err
		}
		if lastDay == utcDay {
			return fmt.Errorf("status link already used today")
		}
		if closedAt.Valid && time.Since(closedAt.Time) > 30*24*time.Hour {
			return fmt.Errorf("status link expired")
		}
		result, err := tx.ExecContext(ctx, "UPDATE security_reports SET token_last_used = ? WHERE tracking_id = ? AND token_hash = ? AND COALESCE(token_last_used, '') <> ?", utcDay, trackingID, tokenHash, utcDay)
		if err != nil {
			return err
		}
		claimed, err := result.RowsAffected()
		if err != nil || claimed != 1 {
			return fmt.Errorf("status link already used today")
		}
		return nil
	})
	if err != nil {
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
	rows, err := readConn.QueryContext(ctx, `SELECT credit, disclosed_on, severity FROM security_thanks ORDER BY disclosed_on DESC, tracking_id DESC`)
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
