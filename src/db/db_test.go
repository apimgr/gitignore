package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestInitCreatesSchemaAndClose(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	dataDir := t.TempDir()
	if err := Init(dataDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		if err := Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	dbPath := filepath.Join(dataDir, "db", "server.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected db file at %s: %v", dbPath, err)
	}

	if conn == nil {
		t.Fatal("conn is nil after Init")
	}
	if err := conn.Ping(); err != nil {
		t.Errorf("conn.Ping: %v", err)
	}
	entries, err := ListSecurityThanks()
	if err != nil || len(entries) != 0 {
		t.Fatalf("ListSecurityThanks() before disclosure = %v, %v", entries, err)
	}
	if _, err := conn.Exec(`INSERT INTO security_thanks (tracking_id, credit, severity, disclosed_on) VALUES ('sec_test', 'Researcher', 'high', '2026-01-02')`); err != nil {
		t.Fatal(err)
	}
	entries, err = ListSecurityThanks()
	if err != nil || len(entries) != 1 || entries[0].Credit != "Researcher" || !strings.HasPrefix(entries[0].Date, "2026-01-02") || entries[0].Severity != "high" {
		t.Fatalf("ListSecurityThanks() = %v, %v", entries, err)
	}
	exerciseOwnerTokenDatabaseLifecycle(t)

	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()

	want := map[string]bool{
		"rate_limits":            false,
		"rate_limit_events":      false,
		"audit_log":              false,
		"scheduler_tasks":        false,
		"scheduler_history":      false,
		"backups":                false,
		"app_secrets":            false,
		"app_secret_revocations": false,
		"owners":                 false,
		"api_tokens":             false,
		"security_thanks":        false,
	}
	forbidden := map[string]bool{
		"server_config": true, "server_admin_credentials": true,
		"server_admin_sessions": true, "user_accounts": true,
		"user_tokens": true, "user_sessions": true, "user_invites": true,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
		if forbidden[name] {
			t.Errorf("unexpected scaffold table %q", name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected table %q to exist", name)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var secretVersionDefault string
	versionRows, err := conn.Query(`PRAGMA table_info(app_secrets)`)
	if err != nil {
		t.Fatalf("inspect app_secrets columns: %v", err)
	}
	for versionRows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := versionRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "version" && defaultValue.Valid {
			secretVersionDefault = defaultValue.String
		}
	}
	if err := versionRows.Close(); err != nil {
		t.Fatal(err)
	}
	if secretVersionDefault != "1" {
		t.Fatalf("app_secrets.version default = %q, want 1", secretVersionDefault)
	}
}

func TestSQLiteWriterHasNoConnectionLifetime(t *testing.T) {
	if got := writerMaxLifetime("sqlite", 5*time.Minute); got != 0 {
		t.Fatalf("SQLite writer connection lifetime = %s, want no lifetime", got)
	}
	if got := writerMaxLifetime("libsql", 5*time.Minute); got != 5*time.Minute {
		t.Fatalf("libSQL writer connection lifetime = %s, want configured lifetime", got)
	}
}

func TestWriteRollsBackSQLiteMutations(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
	writeErr := errors.New("force rollback")
	err := Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO app_secrets(name, value) VALUES(?, ?)", "write_rollback_probe", "value"); err != nil {
			return err
		}
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("Write() error = %v, want %v", err, writeErr)
	}
	var count int
	if err := Read(context.Background(), func(ctx context.Context, reader *sql.DB) error {
		return reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM app_secrets WHERE name = ?", "write_rollback_probe").Scan(&count)
	}); err != nil {
		t.Fatalf("read rollback probe: %v", err)
	}
	if count != 0 {
		t.Fatalf("rollback probe rows = %d, want 0", count)
	}
}

func TestLocalDatabasePathUsesConfiguredSQLiteURL(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	dataDir := t.TempDir()
	configuredPath := filepath.Join(t.TempDir(), "custom.sqlite")
	dsn := (&url.URL{Scheme: "file", Path: configuredPath}).String()
	if err := Init(dataDir, ConnectionConfig{Driver: "sqlite", URL: dsn}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
	if got := LocalDatabasePath(dataDir); got != configuredPath {
		t.Fatalf("LocalDatabasePath() = %q, want %q", got, configuredPath)
	}
}

func TestResolveLocalCachePath(t *testing.T) {
	dataDir := t.TempDir()
	if got, want := ResolveLocalCachePath(dataDir, "cache/cache.db"), filepath.Join(dataDir, "cache", "cache.db"); got != want {
		t.Fatalf("relative cache path = %q, want %q", got, want)
	}
	if got, want := ResolveLocalCachePath(dataDir, ""), filepath.Join(dataDir, "db", "cache.db"); got != want {
		t.Fatalf("default cache path = %q, want %q", got, want)
	}
}

func TestAllowRateLimitPersistsAndResetsExpiredWindows(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	for i := 0; i < 2; i++ {
		allowed, err := AllowRateLimit("test:client", 2, 60)
		if err != nil || !allowed {
			t.Fatalf("request %d allowed=%v err=%v", i+1, allowed, err)
		}
	}
	if allowed, err := AllowRateLimit("test:client", 2, 60); err != nil || allowed {
		t.Fatalf("over-limit request allowed=%v err=%v", allowed, err)
	}
	if blocked, err := RateLimitBlocked("test:client", 2, 60); err != nil || !blocked {
		t.Fatalf("RateLimitBlocked() = %v, %v; want true", blocked, err)
	}
	if seconds, err := RateLimitRetryAfter("test:client", 60); err != nil || seconds < 1 || seconds > 60 {
		t.Fatalf("RateLimitRetryAfter() = %d, %v; want 1..60", seconds, err)
	}
	if err := FlushHotCounters(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("UPDATE rate_limit_events SET occurred_at = (strftime('%s', 'now') - 61) * 1000 WHERE key = ?", "test:client"); err != nil {
		t.Fatal(err)
	}
	if err := loadRateLimitEvents(context.Background(), readConn); err != nil {
		t.Fatal(err)
	}
	if allowed, err := AllowRateLimit("test:client", 2, 60); err != nil || !allowed {
		t.Fatalf("expired-window request allowed=%v err=%v", allowed, err)
	}
	if blocked, err := RateLimitBlocked("test:client", 2, 60); err != nil || blocked {
		t.Fatalf("expired RateLimitBlocked() = %v, %v; want false", blocked, err)
	}
}

func TestRateLimitCacheFailureFallsBackAndKeepsLimits(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		MaxRetries:   -1,
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	ConfigureRateLimitCache(client, "test:")
	t.Cleanup(func() {
		ConfigureRateLimitCache(nil, "")
		_ = client.Close()
		rateLimitFallbackMu.Lock()
		delete(rateLimitFallback, "cache-failure:test")
		rateLimitFallbackMu.Unlock()
	})
	if allowed, err := AllowRateLimit("cache-failure:test", 1, 60); err != nil || !allowed {
		t.Fatalf("first fallback request allowed=%v err=%v, want true", allowed, err)
	}
	if allowed, err := AllowRateLimit("cache-failure:test", 1, 60); err != nil || allowed {
		t.Fatalf("over-limit fallback request allowed=%v err=%v, want false", allowed, err)
	}
	if blocked, err := RateLimitBlocked("cache-failure:test", 1, 60); err != nil || !blocked {
		t.Fatalf("fallback blocked=%v err=%v, want true", blocked, err)
	}
	if RateLimitCacheHealthy() {
		t.Fatal("unreachable external cache reported healthy")
	}
}

func TestMigrateLegacySecurityThanksPreservesHistoricalID(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	for _, statement := range []string{
		"DROP TABLE security_thanks",
		"CREATE TABLE security_thanks (id INTEGER PRIMARY KEY AUTOINCREMENT, tracking_id TEXT NOT NULL UNIQUE, credit TEXT NOT NULL, severity TEXT NOT NULL, disclosed_on DATE NOT NULL)",
		"INSERT INTO security_thanks (tracking_id, credit, severity, disclosed_on) VALUES ('sec_legacy', 'Researcher', 'high', '2026-01-02')",
	} {
		if _, err := conn.Exec(statement); err != nil {
			t.Fatalf("prepare legacy table: %v", err)
		}
	}
	if err := migrateSchema(context.Background(), conn); err != nil {
		t.Fatalf("migrateSchema: %v", err)
	}
	var credit string
	if err := conn.QueryRow("SELECT credit FROM security_thanks WHERE tracking_id = 'sec_legacy'").Scan(&credit); err != nil || credit != "Researcher" {
		t.Fatalf("legacy acknowledgment after migration = %q, %v", credit, err)
	}
	hasID, err := columnExists(context.Background(), conn, "security_thanks", "id")
	if err != nil || !hasID {
		t.Fatalf("legacy id column preserved = %t, %v", hasID, err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	if err := Init(dataDir); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	defer func() { _ = Close() }()

	if err := Init(dataDir); err != nil {
		t.Fatalf("second Init on existing db: %v", err)
	}
}

func TestMigrateLegacySchedulerStateKeepsSourceRows(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	if _, err := conn.Exec(`CREATE TABLE server_scheduler_state (
		task_id TEXT PRIMARY KEY, task_name TEXT NOT NULL, schedule TEXT NOT NULL,
		last_run TEXT, last_status TEXT NOT NULL, last_error TEXT NOT NULL,
		next_run TEXT, run_count INTEGER NOT NULL, fail_count INTEGER NOT NULL, enabled INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO server_scheduler_state
		(task_id, task_name, schedule, last_run, last_status, last_error, next_run, run_count, fail_count, enabled)
		VALUES ('legacy_task', 'Legacy Task', '@daily', '2026-01-02T03:04:05Z', 'success', '', NULL, 4, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateCanonicalState(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	if err := migrateCanonicalState(context.Background(), conn); err != nil {
		t.Fatalf("second migration was not idempotent: %v", err)
	}
	state, err := GetSchedulerState("legacy_task")
	if err != nil || state == nil {
		t.Fatalf("GetSchedulerState() = %+v, %v", state, err)
	}
	if state.TaskName != "Legacy Task" || state.RunCount != 4 || state.FailCount != 1 || state.LastRun.Unix() != 1767323045 {
		t.Fatalf("migrated state = %+v", state)
	}
	var legacyRows int
	if err := conn.QueryRow("SELECT COUNT(*) FROM server_scheduler_state WHERE task_id = 'legacy_task'").Scan(&legacyRows); err != nil || legacyRows != 1 {
		t.Fatalf("legacy source row = %d, %v; want preserved", legacyRows, err)
	}
}

func TestMigrateLegacyInstallationSecretKey(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	encoded := base64.StdEncoding.EncodeToString(secret)
	if _, err := conn.Exec("DROP TABLE app_secrets"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("CREATE TABLE app_secrets (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("INSERT INTO app_secrets(key, value) VALUES('installation_secret', ?)", encoded); err != nil {
		t.Fatal(err)
	}
	if err := ensureColumn(context.Background(), conn, "app_secrets", "name", "TEXT"); err != nil {
		t.Fatal(err)
	}
	if err := migrateCanonicalState(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	loaded, err := InstallationSecret()
	if err != nil || !bytes.Equal(loaded, secret) {
		t.Fatalf("migrated installation secret = %x, %v", loaded, err)
	}
}

func TestInitWithDatabaseDirOverride(t *testing.T) {
	dataDir := t.TempDir()
	overrideDir := t.TempDir()
	t.Setenv("DATABASE_DIR", overrideDir)

	if err := Init(dataDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = Close() }()

	dbPath := filepath.Join(overrideDir, "server.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected db file at override path %s: %v", dbPath, err)
	}

	defaultPath := filepath.Join(dataDir, "db", "server.db")
	if _, err := os.Stat(defaultPath); err == nil {
		t.Errorf("did not expect db file at default path %s when DATABASE_DIR is set", defaultPath)
	}
}

func TestCloseWithoutInitIsSafe(t *testing.T) {
	conn = nil
	if err := Close(); err != nil {
		t.Errorf("Close() with nil conn should be a no-op, got: %v", err)
	}
}

func TestInitRejectsUnwritablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root can bypass permission checks")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(parent, 0700) }()

	target := filepath.Join(parent, "nested")
	if err := Init(target); err == nil {
		_ = Close()
		t.Error("expected Init to fail creating db directory under a read-only parent")
	}
}
