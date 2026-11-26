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
		"rate_limits":       false,
		"rate_limit_events": false,
		"audit_log":         false,
		"scheduler_tasks":   false,
		"scheduler_history": false,
		"backups":           false,
		"app_secrets":       false,
		"owners":            false,
		"api_tokens":        false,
		"security_thanks":   false,
	}
	forbidden := map[string]bool{
		"server_config": true, "server_admin_credentials": true,
		"server_admin_sessions": true, "user_accounts": true,
		"user_tokens": true, "user_sessions": true, "user_invites": true,
		"app_secret_revocations": true,
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
	secretColumns := make(map[string]bool)
	secretInfoRows, err := conn.Query(`PRAGMA table_info(app_secrets)`)
	if err != nil {
		t.Fatalf("inspect app_secrets columns: %v", err)
	}
	for secretInfoRows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := secretInfoRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		secretColumns[name] = true
	}
	if err := secretInfoRows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(secretColumns) != 3 || !secretColumns["name"] || !secretColumns["value"] || !secretColumns["updated_at"] {
		t.Fatalf("app_secrets columns = %v, want name, value, updated_at", secretColumns)
	}
}

func TestInitGeneratesAllProjectSecretsAndRotatesDueBrowserKeys(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	for _, name := range []string{"installation_secret", "cookie_signing_key", "csrf_token_secret"} {
		secret, err := ApplicationSecret(name)
		if err != nil || len(secret) != 32 {
			t.Fatalf("ApplicationSecret(%q) = %d bytes, %v", name, len(secret), err)
		}
	}
	beforeCookie, err := ApplicationSecret("cookie_signing_key")
	if err != nil {
		t.Fatal(err)
	}
	beforeCSRF, err := ApplicationSecret("csrf_token_secret")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := conn.Exec("UPDATE app_secrets SET updated_at = ? WHERE name IN (?, ?)", old, "cookie_signing_key", "csrf_token_secret"); err != nil {
		t.Fatal(err)
	}
	if err := RotateDueApplicationSecrets(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	afterCookie, err := ApplicationSecret("cookie_signing_key")
	if err != nil || bytes.Equal(beforeCookie, afterCookie) {
		t.Fatalf("cookie signing key did not rotate: %v", err)
	}
	afterCSRF, err := ApplicationSecret("csrf_token_secret")
	if err != nil || bytes.Equal(beforeCSRF, afterCSRF) {
		t.Fatalf("CSRF signing key did not rotate: %v", err)
	}
	var cookiePrevious, csrfPrevious int
	if err := conn.QueryRow("SELECT COUNT(*) FROM app_secrets WHERE name = ?", "cookie_signing_key_previous").Scan(&cookiePrevious); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM app_secrets WHERE name = ?", "csrf_token_secret_previous").Scan(&csrfPrevious); err != nil {
		t.Fatal(err)
	}
	if cookiePrevious != 1 || csrfPrevious != 0 {
		t.Fatalf("previous key rows: cookie=%d csrf=%d", cookiePrevious, csrfPrevious)
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
		if _, err := tx.Exec("INSERT INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?)", "write_rollback_probe", "value", time.Now().UTC().Format(time.RFC3339)); err != nil {
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

func TestRotateInstallationSecretKeepsPreviousDuringGrace(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	old, err := InstallationSecret()
	if err != nil {
		t.Fatal(err)
	}
	next := bytes.Repeat([]byte{0x5a}, 32)
	rotatedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := RotateInstallationSecret(next, rotatedAt); err != nil {
		t.Fatal(err)
	}
	current, err := InstallationSecret()
	if err != nil || !bytes.Equal(current, next) {
		t.Fatalf("current installation secret = %x, %v", current, err)
	}
	candidates, err := InstallationSecretCandidates(rotatedAt.Add(time.Hour))
	if err != nil || len(candidates) != 2 || !bytes.Equal(candidates[0], next) || !bytes.Equal(candidates[1], old) {
		t.Fatalf("grace candidates = %x, %v", candidates, err)
	}
	if err := RotateInstallationSecret(bytes.Repeat([]byte{0x33}, 32), rotatedAt.Add(time.Hour)); err == nil {
		t.Fatal("second rotation succeeded during the grace period")
	}
	candidates, err = InstallationSecretCandidates(rotatedAt.Add(8 * 24 * time.Hour))
	if err != nil || len(candidates) != 1 || !bytes.Equal(candidates[0], next) {
		t.Fatalf("expired grace candidates = %x, %v", candidates, err)
	}
}

func TestRetireExpiredSecretRotationsRemovesGraceRows(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	rotatedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := RotateInstallationSecret(bytes.Repeat([]byte{0x4b}, 32), rotatedAt); err != nil {
		t.Fatal(err)
	}
	if err := RetireExpiredSecretRotations(rotatedAt.Add(8 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM app_secrets WHERE name LIKE 'installation_secret_previous%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired installation secret grace rows remaining = %d", count)
	}
}

func TestCompleteEncryptionKeyRotationReencryptsSensitiveRows(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close() }()
	oldKey := bytes.Repeat([]byte{0x17}, 32)
	nextKey := bytes.Repeat([]byte{0x29}, 32)
	oldEncoded := base64.StdEncoding.EncodeToString(oldKey)
	nextEncoded := base64.StdEncoding.EncodeToString(nextKey)
	token, err := encryptAtRest(oldKey, []byte("owner-token-value"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := encryptAtRest(oldKey, []byte("encrypted report body"))
	if err != nil {
		t.Fatal(err)
	}
	contact, err := encryptAtRest(oldKey, []byte("researcher@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("INSERT INTO owners(id) VALUES(?)", "owner-rotation"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO api_tokens(owner_id, kind, token_hash, token_prefix, token_enc)
		VALUES(?, 'primary', ?, ?, ?)`, "owner-rotation", "hash-rotation", "own_rotation", base64.StdEncoding.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO security_reports(tracking_id, token_hash, body_enc, body_alg, contact_enc, severity, component)
		VALUES(?, ?, ?, 'aes-256-gcm', ?, 'high', 'API')`, "sec_rotation000001", "hash-report", body, contact); err != nil {
		t.Fatal(err)
	}
	if err := PrepareEncryptionKeyRotation(oldEncoded, nextEncoded, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	recovered, needsPersistence, err := PendingEncryptionKeyRotation(oldEncoded)
	if err != nil || !needsPersistence || recovered != nextEncoded {
		t.Fatalf("pending rotation recovery = %q, %v, %v", recovered, needsPersistence, err)
	}
	recovered, needsPersistence, err = PendingEncryptionKeyRotation(nextEncoded)
	if err != nil || needsPersistence || recovered != nextEncoded {
		t.Fatalf("persisted rotation recovery = %q, %v, %v", recovered, needsPersistence, err)
	}
	if err := CompleteEncryptionKeyRotation(nextEncoded); err != nil {
		t.Fatal(err)
	}
	var tokenEncoded string
	if err := conn.QueryRow("SELECT token_enc FROM api_tokens WHERE owner_id = ?", "owner-rotation").Scan(&tokenEncoded); err != nil {
		t.Fatal(err)
	}
	tokenCiphertext, err := base64.StdEncoding.DecodeString(tokenEncoded)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext, err := decryptAtRest(nextKey, tokenCiphertext); err != nil || string(plaintext) != "owner-token-value" {
		t.Fatalf("rotated owner token = %q, %v", plaintext, err)
	}
	var bodyCiphertext, contactCiphertext []byte
	if err := conn.QueryRow("SELECT body_enc, contact_enc FROM security_reports WHERE tracking_id = ?", "sec_rotation000001").Scan(&bodyCiphertext, &contactCiphertext); err != nil {
		t.Fatal(err)
	}
	if plaintext, err := decryptAtRest(nextKey, bodyCiphertext); err != nil || string(plaintext) != "encrypted report body" {
		t.Fatalf("rotated report body = %q, %v", plaintext, err)
	}
	if plaintext, err := decryptAtRest(nextKey, contactCiphertext); err != nil || string(plaintext) != "researcher@example.test" {
		t.Fatalf("rotated report contact = %q, %v", plaintext, err)
	}
	if err := CompleteEncryptionKeyRotation(nextEncoded); err != nil {
		t.Fatalf("idempotent completion: %v", err)
	}
	if _, err := conn.Exec("UPDATE api_tokens SET token_enc = ? WHERE owner_id = ?", base64.StdEncoding.EncodeToString(token), "owner-rotation"); err != nil {
		t.Fatal(err)
	}
	plaintext, rotated, err := DecryptOwnerTokenDuringRotation(base64.StdEncoding.EncodeToString(token), nextEncoded)
	if err != nil || !rotated || string(plaintext) != "owner-token-value" {
		t.Fatalf("previous-key owner token read = %q, %v, %v", plaintext, rotated, err)
	}
	if err := conn.QueryRow("SELECT token_enc FROM api_tokens WHERE owner_id = ?", "owner-rotation").Scan(&tokenEncoded); err != nil {
		t.Fatal(err)
	}
	tokenCiphertext, err = base64.StdEncoding.DecodeString(tokenEncoded)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext, err := decryptAtRest(nextKey, tokenCiphertext); err != nil || string(plaintext) != "owner-token-value" {
		t.Fatalf("opportunistically re-encrypted token = %q, %v", plaintext, err)
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
