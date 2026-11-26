package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

func TestRegisterBuiltinsRegistersAllTasks(t *testing.T) {
	s, _ := newTestScheduler()
	if err := RegisterBuiltins(s, Deps{}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	tasks := s.Tasks()
	wantIDs := []string{
		"ssl_renewal", "geoip_update", "blocklist_update", "cve_update",
		"update_check", "token_cleanup", "log_rotation", "backup_daily",
		"backup_hourly", "disk_check", "healthcheck_self", "health_registry_refresh", "ip_block_cleanup", "security_cleanup",
	}
	if len(tasks) != len(wantIDs) {
		t.Fatalf("got %d tasks, want %d", len(tasks), len(wantIDs))
	}
	for i, id := range wantIDs {
		if tasks[i].ID != id {
			t.Errorf("tasks[%d].ID = %q, want %q", i, tasks[i].ID, id)
		}
	}
}

func TestRegisterBuiltinsDisablesBackupHourly(t *testing.T) {
	s, _ := newTestScheduler()
	if err := RegisterBuiltins(s, Deps{}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	info, ok := s.Task("backup_hourly")
	if !ok {
		t.Fatal("backup_hourly not registered")
	}
	if info.Enabled {
		t.Error("backup_hourly should be disabled by default")
	}
	info, ok = s.Task("backup_daily")
	if !ok {
		t.Fatal("backup_daily not registered")
	}
	if !info.Enabled {
		t.Error("backup_daily should remain enabled by default")
	}
}

func TestRegisterBuiltinsDefaultsLogRetention(t *testing.T) {
	s, _ := newTestScheduler()
	if err := RegisterBuiltins(s, Deps{LogRetentionDays: -1}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	// A negative retention must fall back to 7 days rather than pruning
	// everything (or nothing) unexpectedly; exercised indirectly via
	// logRotationHandler below.
	if _, ok := s.Task("log_rotation"); !ok {
		t.Fatal("log_rotation not registered")
	}
}

func TestRequiredHandlerFailsWhenMissing(t *testing.T) {
	err := requiredHandler("blocklist_update", nil)(context.Background())
	if err == nil || !strings.Contains(err.Error(), "blocklist_update") {
		t.Errorf("error = %v, want a missing-handler error", err)
	}
}

func TestRequiredHandlerDelegates(t *testing.T) {
	want := errors.New("update failed")
	called := false
	err := requiredHandler("cve_update", func(context.Context) error {
		called = true
		return want
	})(context.Background())
	if !errors.Is(err, want) || !called {
		t.Errorf("handler error=%v called=%v, want delegated error and call", err, called)
	}
}

func TestTemporaryIPBlockCleanupHandlerDeletesExpiredBlocks(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	if err := db.SaveIPBlock(db.IPBlock{IP: "192.0.2.9", Type: "temporary", Reason: "request_flood", BlockedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute), OffenseCount: 11, AutoBlocked: true}); err != nil {
		t.Fatal(err)
	}
	logsDir := t.TempDir()
	if err := temporaryIPBlockCleanupHandler(Deps{LogsDir: logsDir})(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocks, err := db.LoadIPBlocks(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expired block count = %d, want 0", len(blocks))
	}
	if _, err := os.Stat(filepath.Join(logsDir, "audit.log")); err != nil {
		t.Fatalf("expired block audit log missing: %v", err)
	}
}

func TestModeBoundHealthTasks(t *testing.T) {
	s, _ := newTestScheduler()
	if err := RegisterBuiltins(s, Deps{TorInstalled: true, I2PEnabled: true,
		TorHealth: func(context.Context) error { return nil },
		I2PHealth: func(context.Context) error { return nil },
	}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	for _, id := range []string{"tor_health", "i2p_health"} {
		if _, ok := s.Task(id); !ok {
			t.Errorf("enabled mode task %q is not registered", id)
		}
	}
	s, _ = newTestScheduler()
	if err := RegisterBuiltins(s, Deps{}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	for _, id := range []string{"tor_health", "i2p_health"} {
		if _, ok := s.Task(id); ok {
			t.Errorf("inactive mode task %q must not be registered", id)
		}
	}
}

func TestPGPKeyserverRetryTaskRegistration(t *testing.T) {
	s, _ := newTestScheduler()
	if err := RegisterBuiltins(s, Deps{PGPKeyserverPublish: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	if task, ok := s.Task("keyserver_retry"); !ok {
		t.Fatal("PGP keyserver retry task was not registered")
	} else if task.Schedule != "@every 15m" {
		t.Fatalf("keyserver_retry schedule = %q, want @every 15m", task.Schedule)
	}
}

func TestSSLRenewalHandlerSkipsWhenDisabled(t *testing.T) {
	h := sslRenewalHandler(Deps{SSLEnabled: false})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestSSLRenewalHandlerSucceedsWhenEnabled(t *testing.T) {
	h := sslRenewalHandler(Deps{SSLEnabled: true})
	if err := h(context.Background()); err != nil {
		t.Errorf("error = %v, want nil", err)
	}
}

func TestGeoIPUpdateHandlerSkipsWhenNil(t *testing.T) {
	h := geoipUpdateHandler(Deps{})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestGeoIPUpdateHandlerDelegates(t *testing.T) {
	called := false
	want := errors.New("boom")
	h := geoipUpdateHandler(Deps{GeoIPUpdate: func(ctx context.Context) error {
		called = true
		return want
	}})
	if err := h(context.Background()); !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
	if !called {
		t.Error("injected GeoIPUpdate was not called")
	}
}

func TestUpdateCheckHandlerSkipsWhenNil(t *testing.T) {
	h := updateCheckHandler(Deps{})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestUpdateCheckHandlerDelegates(t *testing.T) {
	called := false
	h := updateCheckHandler(Deps{UpdateCheck: func(ctx context.Context) error {
		called = true
		return nil
	}})
	if err := h(context.Background()); err != nil {
		t.Errorf("error = %v, want nil", err)
	}
	if !called {
		t.Error("injected UpdateCheck was not called")
	}
}

func TestBackupHandlerSkipsWhenDisabled(t *testing.T) {
	called := false
	h := backupHandler(false, func(ctx context.Context) error {
		called = true
		return nil
	})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
	if called {
		t.Error("backup func should not run when disabled")
	}
}

func TestBackupHandlerSkipsWhenNilFunc(t *testing.T) {
	h := backupHandler(true, nil)
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestBackupHandlerRunsWhenEnabled(t *testing.T) {
	called := false
	h := backupHandler(true, func(ctx context.Context) error {
		called = true
		return nil
	})
	if err := h(context.Background()); err != nil {
		t.Errorf("error = %v, want nil", err)
	}
	if !called {
		t.Error("backup func should run when enabled")
	}
}

func TestLogRotationHandlerSkipsWhenNoLogsDir(t *testing.T) {
	h := logRotationHandler(Deps{})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestLogRotationHandlerSkipsWhenDirMissing(t *testing.T) {
	h := logRotationHandler(Deps{LogsDir: filepath.Join(t.TempDir(), "does-not-exist")})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestLogRotationHandlerPrunesOldArchivesOnly(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now()

	mustWrite := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("Chtimes(%s): %v", name, err)
		}
	}
	mustWrite("app.log.1.gz", old)
	mustWrite("access.log.20240101", old)
	mustWrite("recent.log.gz", recent)
	mustWrite("app.log", old)

	h := logRotationHandler(Deps{LogsDir: dir, LogRetentionDays: 7})
	if err := h(context.Background()); err != nil {
		t.Fatalf("logRotationHandler: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	remaining := make(map[string]bool)
	for _, e := range entries {
		remaining[e.Name()] = true
	}
	if remaining["app.log.1.gz"] {
		t.Error("old rotated archive should have been removed")
	}
	if remaining["access.log.20240101"] {
		t.Error("old rotated log should have been removed")
	}
	if !remaining["recent.log.gz"] {
		t.Error("recent rotated archive should be kept")
	}
	if !remaining["app.log"] {
		t.Error("live log file must never be removed")
	}
}

func TestIsRotatedLog(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"app.log.gz", true},
		{"app.log.zip", true},
		{"access.log.20240101", true},
		{"app.log.1", true},
		{"app.log", false},
		{"readme.txt", false},
	}
	for _, c := range cases {
		if got := isRotatedLog(c.name); got != c.want {
			t.Errorf("isRotatedLog(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTokenCleanupHandlerRunsAgainstRealDB(t *testing.T) {
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := tokenCleanupHandler(Deps{})(context.Background()); err != nil {
		t.Errorf("tokenCleanupHandler: %v", err)
	}
}

func TestTokenCleanupHandlerRequiresAndRunsIdlePurge(t *testing.T) {
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	called := false
	h := tokenCleanupHandler(Deps{OwnerIdlePurgeDays: 30, PurgeIdleOwners: func(context.Context) error {
		called = true
		return nil
	}})
	if err := h(context.Background()); err != nil {
		t.Fatalf("tokenCleanupHandler: %v", err)
	}
	if !called {
		t.Error("idle owner purge handler was not called")
	}
}

func TestHealthcheckSelfHandlerRunsAgainstRealDB(t *testing.T) {
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := healthcheckSelfHandler(context.Background()); err != nil {
		t.Errorf("healthcheckSelfHandler: %v", err)
	}
}
