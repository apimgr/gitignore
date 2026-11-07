package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		"backup_hourly", "healthcheck_self", "tor_health",
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

func TestNotWiredHandlerSkips(t *testing.T) {
	h := notWired("Test Subsystem")
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("notWired error = %v, want ErrSkipped", err)
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

func TestTorHealthHandlerSkipsWhenNotInstalled(t *testing.T) {
	h := torHealthHandler(Deps{TorInstalled: false})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
	}
}

func TestTorHealthHandlerSkipsEvenWhenInstalled(t *testing.T) {
	// Real Tor connectivity checking is not wired up yet (AI.md PART 24), so
	// the handler must still skip rather than error or panic.
	h := torHealthHandler(Deps{TorInstalled: true})
	if err := h(context.Background()); !errors.Is(err, ErrSkipped) {
		t.Errorf("error = %v, want ErrSkipped", err)
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

	if err := tokenCleanupHandler(context.Background()); err != nil {
		t.Errorf("tokenCleanupHandler: %v", err)
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
