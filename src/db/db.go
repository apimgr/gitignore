package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	conn *sql.DB
	mu   sync.RWMutex
)

const (
	// readTimeout bounds simple SELECT queries (AI.md PART 10 Query Timeouts).
	readTimeout = 5 * time.Second
	// writeTimeout bounds INSERT/UPDATE/DELETE queries (AI.md PART 10).
	writeTimeout = 10 * time.Second
)

// readCtx returns a context with the standard read-query deadline.
func readCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), readTimeout)
}

// writeCtx returns a context with the standard write-query deadline.
func writeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), writeTimeout)
}

// Init opens (or creates) the SQLite database and runs schema migrations
func Init(dataDir string) error {
	mu.Lock()
	defer mu.Unlock()

	// DATABASE_DIR is an init-only override (AI.md PART 5 "Init-Only Variables",
	// PART 26): when set it takes precedence over the default {data_dir}/db
	// location. Read once at startup.
	dbDir := filepath.Join(dataDir, "db")
	if v := strings.TrimSpace(os.Getenv("DATABASE_DIR")); v != "" {
		dbDir = v
	}
	if err := os.MkdirAll(dbDir, 0750); err != nil {
		return fmt.Errorf("failed to create db directory: %w", err)
	}

	path := filepath.Join(dbDir, "gitignore.db")
	c, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Bound the connection pool. SQLite serializes writers, so a single open
	// connection avoids "database is locked" contention while WAL still allows
	// concurrent readers (AI.md PART 10).
	c.SetMaxOpenConns(1)
	c.SetMaxIdleConns(1)
	c.SetConnMaxLifetime(0)

	// Verify the connection is actually reachable before proceeding.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.PingContext(ctx); err != nil {
		c.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	if _, err := c.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		c.Close()
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := c.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		c.Close()
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Reconcile schema drift from earlier releases before the CREATE TABLE IF
	// NOT EXISTS statements run — a table that already exists is never altered
	// by IF NOT EXISTS, so incompatible legacy tables are dropped first.
	if err := migrateSchema(ctx, c); err != nil {
		c.Close()
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	if err := createSchema(ctx, c); err != nil {
		c.Close()
		return fmt.Errorf("failed to create schema: %w", err)
	}

	conn = c
	return nil
}

// Close closes the database connection
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
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
	return nil
}

// columnExists reports whether the named column is present on the table. A
// missing table yields false with no error.
func columnExists(ctx context.Context, c *sql.DB, table, column string) (bool, error) {
	rows, err := c.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notnull, pk       int
			name, ctype            string
			dfltValue              sql.NullString
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
`)
	return err
}
