package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var cacheTables = []string{
	"rate_limits", "rate_limit_events", "audit_log", "scheduler_tasks", "scheduler_history", "backups",
	"app_secrets", "app_secret_revocations", "pgp_key_metadata", "security_reports", "security_thanks", "owners", "api_tokens", "ip_blocks",
}

var cacheTableColumns = map[string][]string{
	"rate_limits":            {"key", "count", "window_start", "updated_at"},
	"rate_limit_events":      {"id", "key", "occurred_at"},
	"audit_log":              {"id", "timestamp", "level", "category", "action", "actor_ip", "target_type", "target_id", "details", "success"},
	"scheduler_tasks":        {"id", "name", "enabled", "schedule", "last_run", "next_run", "last_status", "last_error", "run_count", "fail_count"},
	"scheduler_history":      {"id", "task_id", "started_at", "finished_at", "status", "error", "duration_ms"},
	"backups":                {"id", "filename", "filepath", "size_bytes", "type", "created_at", "checksum", "notes"},
	"app_secrets":            {"name", "value", "version", "updated_at"},
	"app_secret_revocations": {"name", "version", "node_id", "revoked_at"},
	"pgp_key_metadata":       {"id", "fingerprint", "created_at", "expires_at", "last_rotated_at", "keyservers_published", "revoked"},
	"security_reports":       {"tracking_id", "security_id", "token_hash", "body_enc", "body_alg", "contact_enc", "severity", "component", "status", "researcher_comment", "disclosure_date", "credit_pref", "credit_name", "cve_requested", "created_at", "updated_at", "token_last_used", "closed_at"},
	"security_thanks":        {"tracking_id", "credit", "severity", "disclosed_on"},
	"owners":                 {"id", "created_at", "last_seen_at"},
	"api_tokens":             {"id", "owner_id", "kind", "name", "token_hash", "token_prefix", "token_last4", "token_enc", "created_at", "rotated_at", "expires_at", "last_used_at", "revoked_at", "revoked_reason"},
	"ip_blocks":              {"ip", "cidr", "type", "reason", "blocked_at", "expires_at", "offense_count", "auto_blocked"},
}

type remoteSQLiteCache struct {
	writer       *sql.DB
	reader       *sql.DB
	remote       *sql.DB
	path         string
	remoteDSN    string
	readDSN      string
	interval     time.Duration
	maxAge       time.Duration
	offline      bool
	mu           sync.Mutex
	syncMu       sync.Mutex
	readSyncMu   sync.RWMutex
	lastSync     time.Time
	dirty        bool
	syncing      bool
	stop         chan struct{}
	done         chan struct{}
	syncDebounce *time.Timer
}

func openRemoteSQLiteCache(ctx context.Context, remote *sql.DB, remoteDSN string, cfg CacheConfig) (*remoteSQLiteCache, bool, error) {
	if !cfg.Enabled {
		return nil, true, nil
	}
	if cfg.Path == "" {
		return nil, true, errors.New("server.database.cache.path is required when the local cache is enabled")
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = 30 * time.Second
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = time.Hour
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o700); err != nil {
		return nil, true, err
	}
	writeDSN := sqliteDSN(cfg.Path, "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	readDSN := sqliteDSN(cfg.Path, "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=query_only(1)")
	writer, err := sql.Open(registerMetricsDriverFor("sqlite"), writeDSN)
	if err != nil {
		return nil, true, err
	}
	reader, err := sql.Open(cacheReaderDriver, readDSN)
	if err != nil {
		_ = writer.Close()
		return nil, true, err
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)
	reader.SetMaxOpenConns(25)
	reader.SetMaxIdleConns(5)
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, true, err
	}
	if err := os.Chmod(cfg.Path, 0o600); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, true, err
	}
	if err := reader.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, true, err
	}
	if err := writeWithDB(ctx, writer, true, "local cache schema", func(tx *sql.Tx) error {
		if err := migrateSchema(ctx, tx); err != nil {
			return err
		}
		if err := createSchema(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ip_blocks (
ip TEXT PRIMARY KEY, cidr TEXT, type TEXT NOT NULL DEFAULT 'temporary', reason TEXT NOT NULL,
blocked_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, offense_count INTEGER NOT NULL DEFAULT 1,
auto_blocked INTEGER NOT NULL DEFAULT 1)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_ip_blocks_expiry ON ip_blocks(expires_at)")
		return err
	}); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, true, err
	}
	cache := &remoteSQLiteCache{writer: writer, reader: reader, remote: remote, path: cfg.Path, remoteDSN: remoteDSN, readDSN: readDSN, interval: cfg.SyncInterval, maxAge: cfg.MaxAge, offline: cfg.OfflineMode}
	if err := cache.readLastSync(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, true, err
	}
	remoteOnline := true
	if err := cache.syncNow(ctx); err != nil {
		remoteOnline = false
		if !cfg.OfflineMode {
			_ = writer.Close()
			_ = reader.Close()
			return nil, false, err
		}
		if cache.isExpired() {
			_ = writer.Close()
			_ = reader.Close()
			return nil, false, fmt.Errorf("local database cache is older than server.database.cache.max_age: %w", err)
		}
	}
	cache.start()
	return cache, remoteOnline, nil
}

func (c *remoteSQLiteCache) readLastSync(ctx context.Context) error {
	err := writeWithDB(ctx, c.writer, true, "local cache metadata schema", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS cache_metadata (name TEXT PRIMARY KEY, value TEXT NOT NULL)")
		return err
	})
	if err != nil {
		return err
	}
	var stamp, dirty string
	stampErr := c.reader.QueryRowContext(ctx, "SELECT value FROM cache_metadata WHERE name = 'last_sync'").Scan(&stamp)
	if stampErr != nil && !errors.Is(stampErr, sql.ErrNoRows) {
		return stampErr
	}
	dirtyErr := c.reader.QueryRowContext(ctx, "SELECT value FROM cache_metadata WHERE name = 'dirty'").Scan(&dirty)
	if dirtyErr != nil && !errors.Is(dirtyErr, sql.ErrNoRows) {
		return dirtyErr
	}
	c.mu.Lock()
	if stampErr == nil {
		c.lastSync, _ = time.Parse(time.RFC3339Nano, stamp)
	}
	c.dirty = dirty == "1"
	c.mu.Unlock()
	return nil
}

func (c *remoteSQLiteCache) syncNow(ctx context.Context) error {
	return c.syncNowWithPolicy(ctx, false)
}

func (c *remoteSQLiteCache) syncNowWithPolicy(ctx context.Context, keepDirtyOnFetchFailure bool) error {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	c.mu.Lock()
	wasDirty := c.dirty
	c.mu.Unlock()
	if err := c.beginSyncFetch(ctx, wasDirty || keepDirtyOnFetchFailure); err != nil {
		return err
	}
	rowsByTable, err := c.fetchSnapshot(ctx)
	if err != nil {
		c.failSyncFetch(wasDirty || keepDirtyOnFetchFailure)
		return err
	}
	if err := c.beginSyncApply(ctx); err != nil {
		return err
	}
	c.readSyncMu.Lock()
	defer c.readSyncMu.Unlock()
	if err := c.applySnapshot(ctx, rowsByTable); err != nil {
		c.failSyncApply()
		return err
	}
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	err = writeWithDB(ctx, c.writer, true, "local cache sync metadata", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO cache_metadata(name, value) VALUES ('last_sync', ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value", stamp); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO cache_metadata(name, value) VALUES ('dirty', '0') ON CONFLICT(name) DO UPDATE SET value = excluded.value")
		return err
	})
	if err != nil {
		c.failSyncApply()
		return err
	}
	c.mu.Lock()
	c.lastSync = now
	c.dirty = false
	c.syncing = false
	c.mu.Unlock()
	return nil
}

func (c *remoteSQLiteCache) fetchSnapshot(ctx context.Context) (map[string][][]any, error) {
	rowsByTable := make(map[string][][]any, len(cacheTables))
	for _, table := range cacheTables {
		columns, ok := cacheTableColumns[table]
		if !ok || len(columns) == 0 {
			return nil, fmt.Errorf("local cache table %s has no declared columns", table)
		}
		quotedColumns := quoteSQLIdentifiers(columns)
		remoteName := serverObjectName(table, true)
		rows, err := c.remote.QueryContext(ctx, "SELECT "+strings.Join(quotedColumns, ", ")+" FROM \""+remoteName+"\"")
		if err != nil {
			return nil, fmt.Errorf("read remote table %s: %w", remoteName, err)
		}
		data := make([][]any, 0)
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				_ = rows.Close()
				return nil, err
			}
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					values[i] = append([]byte(nil), bytes...)
				}
			}
			data = append(data, values)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		rowsByTable[table] = data
	}
	return rowsByTable, nil
}

func (c *remoteSQLiteCache) failSyncFetch(keepDirty bool) {
	if keepDirty {
		c.failSyncApply()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	err := writeWithDB(ctx, c.writer, true, "restore local cache metadata", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO cache_metadata(name, value) VALUES ('dirty', '0') ON CONFLICT(name) DO UPDATE SET value = excluded.value")
		return err
	})
	c.mu.Lock()
	c.syncing = false
	if err == nil {
		c.dirty = false
	} else {
		c.dirty = true
	}
	c.mu.Unlock()
}

func (c *remoteSQLiteCache) applySnapshot(ctx context.Context, rowsByTable map[string][][]any) error {
	for _, table := range cacheTables {
		columns := cacheTableColumns[table]
		quoted := quoteSQLIdentifiers(columns)
		placeholders := make([]string, len(columns))
		for i := range columns {
			placeholders[i] = "?"
		}
		query := "INSERT OR REPLACE INTO \"" + table + "\" (" + strings.Join(quoted, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
		for {
			var deleted int64
			err := writeWithDB(ctx, c.writer, true, "local cache clear", func(tx *sql.Tx) error {
				result, err := tx.ExecContext(ctx, `DELETE FROM "`+table+`" WHERE rowid IN (SELECT rowid FROM "`+table+`" LIMIT 500)`)
				if err != nil {
					return fmt.Errorf("clear local cache table %s: %w", table, err)
				}
				deleted, err = result.RowsAffected()
				return err
			})
			if err != nil {
				return fmt.Errorf("clear local cache table %s: %w", table, err)
			}
			if deleted < 500 {
				break
			}
			runtime.Gosched()
		}
		rows := rowsByTable[table]
		for start := 0; start < len(rows); start += 500 {
			end := min(start+500, len(rows))
			if err := writeWithDB(ctx, c.writer, true, "local cache import", func(tx *sql.Tx) error {
				for _, values := range rows[start:end] {
					if _, err := tx.ExecContext(ctx, query, values...); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return fmt.Errorf("import local cache table %s: %w", table, err)
			}
			runtime.Gosched()
		}
	}
	return nil
}

func quoteSQLIdentifiers(values []string) []string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return quoted
}

func (c *remoteSQLiteCache) beginSyncApply(ctx context.Context) error {
	c.mu.Lock()
	wasDirty := c.dirty
	c.syncing = true
	c.dirty = true
	c.mu.Unlock()
	err := writeWithDB(ctx, c.writer, true, "mark local cache dirty", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO cache_metadata(name, value) VALUES ('dirty', '1') ON CONFLICT(name) DO UPDATE SET value = excluded.value")
		return err
	})
	if err != nil {
		c.mu.Lock()
		c.syncing = false
		c.dirty = wasDirty
		c.mu.Unlock()
		return err
	}
	return nil
}

func (c *remoteSQLiteCache) beginSyncFetch(ctx context.Context, keepDirty bool) error {
	c.mu.Lock()
	c.syncing = true
	if keepDirty {
		c.dirty = true
	}
	c.mu.Unlock()
	if !keepDirty {
		return nil
	}
	err := writeWithDB(ctx, c.writer, true, "mark local cache dirty", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO cache_metadata(name, value) VALUES ('dirty', '1') ON CONFLICT(name) DO UPDATE SET value = excluded.value")
		return err
	})
	if err != nil {
		c.mu.Lock()
		c.syncing = false
		c.mu.Unlock()
	}
	return err
}

func (c *remoteSQLiteCache) failSyncApply() {
	c.mu.Lock()
	c.dirty = true
	c.syncing = false
	c.mu.Unlock()
}

func (c *remoteSQLiteCache) start() {
	c.mu.Lock()
	if c.stop != nil {
		c.mu.Unlock()
		return
	}
	c.stop, c.done = make(chan struct{}), make(chan struct{})
	stop, done := c.stop, c.done
	c.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				if err := c.syncNow(ctx); err != nil {
					log.Printf("database: local cache sync failed: %v", err)
				}
				cancel()
			case <-stop:
				return
			}
		}
	}()
}

func (c *remoteSQLiteCache) scheduleSync() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.syncDebounce != nil {
		c.syncDebounce.Stop()
	}
	c.syncDebounce = time.AfterFunc(500*time.Millisecond, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := c.syncNow(ctx); err != nil {
			log.Printf("database: local cache refresh failed: %v", err)
		}
	})
}

func (c *remoteSQLiteCache) refreshAfterWrite(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.syncNowWithPolicy(refreshCtx, true); err != nil {
		dirtyCtx, dirtyCancel := context.WithTimeout(context.Background(), writeTimeout)
		defer dirtyCancel()
		dirtyErr := c.markDirty(dirtyCtx)
		invalidateErr := c.invalidateCache(dirtyCtx)
		if dirtyErr != nil || invalidateErr != nil {
			log.Printf("WARN database: local cache update after remote write failed: %v; invalidation failed: %v", err, errors.Join(dirtyErr, invalidateErr))
			return
		}
		log.Printf("WARN database: local cache update after remote write failed: %v; cached rows were invalidated", err)
	}
}

func (c *remoteSQLiteCache) markDirty(ctx context.Context) error {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
	return writeWithDB(ctx, c.writer, true, "mark local cache dirty", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO cache_metadata(name, value) VALUES ('dirty', '1') ON CONFLICT(name) DO UPDATE SET value = excluded.value")
		return err
	})
}

func (c *remoteSQLiteCache) invalidateCache(ctx context.Context) error {
	c.readSyncMu.Lock()
	defer c.readSyncMu.Unlock()
	c.mu.Lock()
	c.syncing = true
	c.mu.Unlock()
	for _, table := range cacheTables {
		for {
			var deleted int64
			err := writeWithDB(ctx, c.writer, true, "invalidate local cache", func(tx *sql.Tx) error {
				result, err := tx.ExecContext(ctx, `DELETE FROM "`+table+`" WHERE rowid IN (SELECT rowid FROM "`+table+`" LIMIT 500)`)
				if err != nil {
					return err
				}
				deleted, err = result.RowsAffected()
				return err
			})
			if err != nil {
				c.failSyncApply()
				return err
			}
			if deleted < 500 {
				break
			}
			runtime.Gosched()
		}
	}
	c.mu.Lock()
	c.syncing = false
	c.dirty = true
	c.mu.Unlock()
	return nil
}

func (c *remoteSQLiteCache) fallbackQuery(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	lastSync, maxAge, offline, dirty := c.lastSync, c.maxAge, c.offline, c.dirty
	c.mu.Unlock()
	if !offline || dirty || lastSync.IsZero() || time.Since(lastSync) > maxAge {
		return nil, errors.New("local database cache is unavailable or expired")
	}
	conn, err := c.reader.Driver().Open(c.readDSN)
	if err != nil {
		return nil, err
	}
	queryer, ok := conn.(driver.QueryerContext)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("local cache driver does not support context queries")
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &cacheDriverRows{Rows: rows, conn: conn}, nil
}

func (c *remoteSQLiteCache) readThrough(rows driver.Rows, ctx context.Context, query string, args []driver.NamedValue) driver.Rows {
	return &readThroughRows{Rows: rows, ctx: ctx, cache: c, query: query, args: append([]driver.NamedValue(nil), args...)}
}

type readThroughRows struct {
	driver.Rows
	ctx    context.Context
	cache  *remoteSQLiteCache
	query  string
	args   []driver.NamedValue
	remote driver.Rows
	tried  bool
	locked bool
}

func (r *readThroughRows) Next(dest []driver.Value) error {
	if !r.locked {
		r.cache.readSyncMu.RLock()
		r.locked = true
		if r.cache.isExpired() {
			_ = r.Rows.Close()
			r.tried = true
			remote, err := r.cache.freshRemoteQuery(r.ctx, r.query, r.args)
			if err != nil {
				r.cache.readSyncMu.RUnlock()
				r.locked = false
				return err
			}
			r.remote = remote
		}
	}
	if r.remote != nil {
		return r.remote.Next(dest)
	}
	err := r.Rows.Next(dest)
	if !errors.Is(err, io.EOF) || r.tried {
		return err
	}
	r.tried = true
	remote, remoteErr := r.cache.freshRemoteQuery(r.ctx, r.query, r.args)
	if remoteErr != nil {
		return remoteErr
	}
	r.remote = remote
	return r.remote.Next(dest)
}

func (r *readThroughRows) Close() error {
	localErr := r.Rows.Close()
	if r.remote != nil {
		if err := r.remote.Close(); localErr == nil {
			localErr = err
		}
	}
	if r.locked {
		r.cache.readSyncMu.RUnlock()
		r.locked = false
	}
	return localErr
}

func (c *remoteSQLiteCache) isExpired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dirty || c.lastSync.IsZero() || time.Since(c.lastSync) > c.maxAge
}

func (c *remoteSQLiteCache) freshRemoteQuery(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	conn, err := c.remote.Driver().Open(c.remoteDSN)
	if err != nil {
		return nil, err
	}
	queryer, ok := conn.(driver.QueryerContext)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("remote database driver does not support context queries")
	}
	rows, err := queryer.QueryContext(ctx, mapServerIdentifiers(query, true), args)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.scheduleSync()
	return &cacheDriverRows{Rows: rows, conn: conn}, nil
}

type cacheDriverRows struct {
	driver.Rows
	conn driver.Conn
}

func (r *cacheDriverRows) Close() error {
	rowsErr := r.Rows.Close()
	connErr := r.conn.Close()
	if rowsErr != nil {
		return rowsErr
	}
	return connErr
}

func (c *remoteSQLiteCache) close() error {
	c.mu.Lock()
	stop, done := c.stop, c.done
	c.stop, c.done = nil, nil
	if c.syncDebounce != nil {
		c.syncDebounce.Stop()
	}
	c.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if err := c.reader.Close(); err != nil {
		_ = c.writer.Close()
		_ = c.remote.Close()
		return err
	}
	if err := c.writer.Close(); err != nil {
		_ = c.remote.Close()
		return err
	}
	return c.remote.Close()
}

// SnapshotLocalCache creates a consistent snapshot of the optional SQLite
// mirror; the caller owns and removes destinationPath.
func SnapshotLocalCache(ctx context.Context, destinationPath string) error {
	mu.RLock()
	cache := localCache
	mu.RUnlock()
	if cache == nil {
		return sql.ErrConnDone
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return fmt.Errorf("snapshot destination already exists: %s", destinationPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if _, err := cache.writer.ExecContext(ctx, "VACUUM INTO ?", destinationPath); err != nil {
		return fmt.Errorf("create local cache snapshot: %w", err)
	}
	return nil
}
