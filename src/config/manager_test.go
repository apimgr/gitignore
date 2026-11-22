package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigManagerAppliesChangedConfigAndClassifiesChanges(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.yml")
	if err := os.WriteFile(configPath, []byte("server: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	initial := DefaultConfig()
	initial.Server.Mode = "production"
	initial.Server.BaseURL = "/"
	next := DefaultConfig()
	next.Server.Mode = "development"
	next.Server.BaseURL = "/api"
	next.Server.Port = "64001"
	var applied ConfigChanges
	manager, err := NewConfigManager(ConfigManagerOptions{
		ConfigPath: configPath,
		Initial:    initial,
		Load:       func() (*Config, error) { return next, nil },
		Apply: func(_ *Config, changes ConfigChanges) error {
			applied = changes
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("server:\n  mode: development\n  port: '64001'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.CheckNow(); err != nil {
		t.Fatal(err)
	}
	if manager.Current() == next || manager.Current().Server.Port != initial.Server.Port {
		t.Fatal("restart-required port change became active before restart")
	}
	if manager.Current().Server.Mode != next.Server.Mode || manager.Current().Server.BaseURL != next.Server.BaseURL {
		t.Fatal("hot or graceful configuration changes were not published")
	}
	if !reflect.DeepEqual(applied.HotReload, []string{"server.mode"}) {
		t.Errorf("hot reload paths = %#v, want server.mode", applied.HotReload)
	}
	if !reflect.DeepEqual(applied.GracefulReload, []string{"server.baseurl"}) {
		t.Errorf("graceful reload paths = %#v, want server.baseurl", applied.GracefulReload)
	}
	if !reflect.DeepEqual(applied.RestartRequired, []string{"server.port"}) {
		t.Errorf("restart paths = %#v, want server.port", applied.RestartRequired)
	}
	if !manager.PendingRestart() || !reflect.DeepEqual(manager.RestartSettings(), []string{"server.port"}) {
		t.Fatalf("restart state = pending:%v settings:%v", manager.PendingRestart(), manager.RestartSettings())
	}
}

func TestConfigManagerAccumulatesAndClearsRestartSettings(t *testing.T) {
	manager := newTestConfigManager(t, DefaultConfig(), func() (*Config, error) {
		return DefaultConfig(), nil
	}, nil)
	next := DefaultConfig()
	next.Server.Port = "64001"
	manager.load = func() (*Config, error) { return next, nil }
	writeWatchedConfig(t, manager.configPath, "server:\n  port: '64001'\n")
	if err := manager.CheckNow(); err != nil {
		t.Fatal(err)
	}

	next = DefaultConfig()
	next.Server.Port = "64001"
	next.Server.Address = "127.0.0.1"
	manager.load = func() (*Config, error) { return next, nil }
	writeWatchedConfig(t, manager.configPath, "server:\n  port: '64001'\n  address: 127.0.0.1\n")
	if err := manager.CheckNow(); err != nil {
		t.Fatal(err)
	}
	want := []string{"server.address", "server.port"}
	if got := manager.RestartSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RestartSettings() = %#v, want %#v", got, want)
	}
	manager.ClearPendingRestart()
	if manager.PendingRestart() || len(manager.RestartSettings()) != 0 {
		t.Fatal("ClearPendingRestart did not clear the accumulated reasons")
	}
}

func TestConfigManagerRetriesFailureWithoutReplacingLastGoodConfig(t *testing.T) {
	initial := DefaultConfig()
	loadErr := errors.New("invalid file")
	manager := newTestConfigManager(t, initial, func() (*Config, error) { return nil, loadErr }, nil)
	writeWatchedConfig(t, manager.configPath, "server: [invalid\n")
	if err := manager.CheckNow(); !errors.Is(err, loadErr) {
		t.Fatalf("CheckNow() error = %v, want loader error", err)
	}
	if manager.Current() != initial {
		t.Fatal("failed reload replaced the last-good configuration")
	}

	loaded := DefaultConfig()
	loaded.Server.Mode = "development"
	manager.load = func() (*Config, error) { return loaded, nil }
	if err := manager.CheckNow(); err != nil {
		t.Fatalf("retry after fixing loader failed: %v", err)
	}
	if manager.Current() != loaded {
		t.Fatal("successful retry did not publish the loaded configuration")
	}
}

func TestConfigManagerApplyFailureDoesNotAcceptNewState(t *testing.T) {
	initial := DefaultConfig()
	next := DefaultConfig()
	next.Server.Mode = "development"
	applyErr := errors.New("apply failed")
	applyFailure := true
	manager := newTestConfigManager(t, initial, func() (*Config, error) { return next, nil }, func(*Config, ConfigChanges) error {
		if applyFailure {
			return applyErr
		}
		return nil
	})
	writeWatchedConfig(t, manager.configPath, "server:\n  mode: development\n")
	if err := manager.CheckNow(); !errors.Is(err, applyErr) {
		t.Fatalf("CheckNow() error = %v, want apply error", err)
	}
	if manager.Current() != initial {
		t.Fatal("failed apply replaced the last-good configuration")
	}
	applyFailure = false
	if err := manager.CheckNow(); err != nil {
		t.Fatalf("retry after apply failure failed: %v", err)
	}
	if manager.Current() != next {
		t.Fatal("successful apply retry did not accept the new configuration")
	}
}

func TestConfigManagerWatchesDropinsSecretsAndAssets(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.yml")
	writeWatchedConfig(t, configPath, "server: {}\n")
	var loads atomic.Int32
	var applies atomic.Int32
	manager, err := NewConfigManager(ConfigManagerOptions{
		ConfigPath: configPath,
		Initial:    DefaultConfig(),
		Load: func() (*Config, error) {
			loads.Add(1)
			return DefaultConfig(), nil
		},
		Apply: func(_ *Config, _ ConfigChanges) error {
			applies.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(directory, "yaml.d", "20-app.yml"):    "server:\n  mode: development\n",
		filepath.Join(directory, "secrets.yml"):             "server:\n  token: secret\n",
		filepath.Join(directory, "assets", "branding.html"): "<p>updated</p>\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		writeWatchedConfig(t, path, body)
	}
	if err := manager.CheckNow(); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 1 || applies.Load() != 1 {
		t.Fatalf("drop-in, secret, and asset changes invoked loader=%d apply=%d times", loads.Load(), applies.Load())
	}
}

func TestConfigManagerIgnoresHiddenAndBackupDropins(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.yml")
	writeWatchedConfig(t, configPath, "server: {}\n")
	var loads atomic.Int32
	manager, err := NewConfigManager(ConfigManagerOptions{
		ConfigPath: configPath,
		Initial:    DefaultConfig(),
		Load: func() (*Config, error) {
			loads.Add(1)
			return DefaultConfig(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path := range map[string]struct{}{
		filepath.Join(directory, "yaml.d", ".hidden.yml"):    {},
		filepath.Join(directory, "yaml.d", "20-app.yml.bak"): {},
		filepath.Join(directory, "yaml.d", "notes.txt"):      {},
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		writeWatchedConfig(t, path, "changed\n")
	}
	if err := manager.CheckNow(); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 0 {
		t.Fatalf("ignored drop-ins triggered %d reloads, want 0", loads.Load())
	}
}

func TestConfigManagerWatchesOnlyInDirectoryDropinSymlinks(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.yml")
	writeWatchedConfig(t, configPath, "server: {}\n")
	dropinDirectory := filepath.Join(directory, "yaml.d")
	if err := os.MkdirAll(dropinDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	insideTarget := filepath.Join(dropinDirectory, "target.yml")
	writeWatchedConfig(t, insideTarget, "server:\n  mode: development\n")
	outsideTarget := filepath.Join(t.TempDir(), "outside.yml")
	writeWatchedConfig(t, outsideTarget, "server:\n  mode: debug\n")
	insideLink := filepath.Join(dropinDirectory, "10-inside.yml")
	outsideLink := filepath.Join(dropinDirectory, "20-outside.yml")
	if err := os.Symlink(insideTarget, insideLink); err != nil {
		t.Skipf("create in-directory symlink: %v", err)
	}
	if err := os.Symlink(outsideTarget, outsideLink); err != nil {
		t.Skipf("create out-of-directory symlink: %v", err)
	}
	manager, err := NewConfigManager(ConfigManagerOptions{
		ConfigPath: configPath,
		Initial:    DefaultConfig(),
		Load:       func() (*Config, error) { return DefaultConfig(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.fileState()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(insideTarget, []byte("server:\n  mode: production\n"), 0600); err != nil {
		t.Fatal(err)
	}
	afterInside, err := manager.fileState()
	if err != nil {
		t.Fatal(err)
	}
	if before == afterInside {
		t.Fatal("in-directory symlink target change was not observed")
	}
	if err := os.WriteFile(outsideTarget, []byte("server:\n  mode: production\n"), 0600); err != nil {
		t.Fatal(err)
	}
	afterOutside, err := manager.fileState()
	if err != nil {
		t.Fatal(err)
	}
	if afterInside != afterOutside {
		t.Fatal("out-of-directory symlink target change was observed")
	}
}

func TestConfigManagerReportsErrorsAndStops(t *testing.T) {
	initial := DefaultConfig()
	manager := newTestConfigManager(t, initial, func() (*Config, error) {
		return nil, errors.New("load failed")
	}, nil)
	writeWatchedConfig(t, manager.configPath, "changed\n")
	var reported atomic.Int32
	manager.onError = func(error) { reported.Add(1) }
	ctx, cancel := context.WithCancel(context.Background())
	manager.interval = time.Millisecond
	manager.Start(ctx)
	deadline := time.Now().Add(time.Second)
	for reported.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	manager.Stop()
	manager.Stop()
	if reported.Load() == 0 {
		t.Fatal("watch loop did not report reload failure")
	}
	if manager.Current() != initial {
		t.Fatal("watch loop replaced last-good configuration after a failure")
	}
}

func newTestConfigManager(t *testing.T, initial *Config, load func() (*Config, error), apply func(*Config, ConfigChanges) error) *ConfigManager {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.yml")
	writeWatchedConfig(t, configPath, "server: {}\n")
	manager, err := NewConfigManager(ConfigManagerOptions{
		ConfigPath: configPath,
		Initial:    initial,
		Load:       load,
		Apply:      apply,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func writeWatchedConfig(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigManagerRestartSettingsContainOnlyPaths(t *testing.T) {
	initial := DefaultConfig()
	next := DefaultConfig()
	next.Server.Port = "64002"
	manager := newTestConfigManager(t, initial, func() (*Config, error) { return next, nil }, nil)
	writeWatchedConfig(t, manager.configPath, "server:\n  port: '64002'\n")
	if err := manager.CheckNow(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(manager.RestartSettings(), ","), "64002") {
		t.Fatal("restart settings exposed a configuration value")
	}
}
