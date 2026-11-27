package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

func TestRunListenerRequestAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	base := t.TempDir()
	configDir := filepath.Join(base, "config")
	dataDir := filepath.Join(base, "data")
	cacheDir := filepath.Join(base, "cache")
	logsDir := filepath.Join(base, "logs")
	backupDir := filepath.Join(base, "backup")
	for _, directory := range []string{configDir, dataDir, cacheDir, logsDir, backupDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{
		"DATA_DIR": dataDir, "CACHE_DIR": cacheDir, "LOG_DIR": logsDir,
		"BACKUP_DIR": backupDir, "DATABASE_DIR": filepath.Join(dataDir, "db"),
	} {
		oldValue, wasSet := os.LookupEnv(key)
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if wasSet {
				_ = os.Setenv(key, oldValue)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	cfg, err := config.LoadFile(filepath.Join(configDir, "server.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Build.Version = "test"
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.I2P.Enabled = false
	cfg.Server.Tor.Expose = false
	cfg.Server.Security.Blocklists.Enabled = false
	cfg.Runtime = config.RuntimeOptions{
		ConfigDir: configDir, DataDir: dataDir, CacheDir: cacheDir, LogDir: logsDir,
		BackupDir: backupDir, ConfigPath: filepath.Join(configDir, "server.yml"),
		Startup: config.StartupSettings{PIDFile: false},
	}
	go func() { done <- run(ctx, *cfg, listener, ready) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("app did not become ready")
	}
	response, err := http.Get("http://" + listener.Addr().String() + "/server/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("app shutdown returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not shut down after cancellation")
	}
}
