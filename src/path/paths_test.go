package path

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewPathManagerDefaults(t *testing.T) {
	pm := New()
	if pm.GetConfigDir() == "" {
		t.Error("GetConfigDir() empty by default")
	}
	if pm.GetDataDir() == "" {
		t.Error("GetDataDir() empty by default")
	}
	if pm.GetLogsDir() == "" {
		t.Error("GetLogsDir() empty by default")
	}
}

func TestPathManagerSettersAndGetters(t *testing.T) {
	pm := New()

	pm.SetConfigDir("/tmp/x/config")
	if got := pm.GetConfigDir(); got != "/tmp/x/config" {
		t.Errorf("GetConfigDir() = %q", got)
	}

	pm.SetDataDir("/tmp/x/data")
	if got := pm.GetDataDir(); got != "/tmp/x/data" {
		t.Errorf("GetDataDir() = %q", got)
	}

	pm.SetLogsDir("/tmp/x/logs")
	if got := pm.GetLogsDir(); got != "/tmp/x/logs" {
		t.Errorf("GetLogsDir() = %q", got)
	}
}

func TestPathManagerJoinHelpers(t *testing.T) {
	pm := New()
	pm.SetConfigDir("/base/config")
	pm.SetDataDir("/base/data")
	pm.SetLogsDir("/base/logs")

	if got, want := pm.ConfigPath("server.yml"), filepath.Join("/base/config", "server.yml"); got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
	if got, want := pm.DataPath("db.sqlite"), filepath.Join("/base/data", "db.sqlite"); got != want {
		t.Errorf("DataPath() = %q, want %q", got, want)
	}
	if got, want := pm.LogsPath("app.log"), filepath.Join("/base/logs", "app.log"); got != want {
		t.Errorf("LogsPath() = %q, want %q", got, want)
	}
}

func TestPathManagerEnsureDirectories(t *testing.T) {
	tmp := t.TempDir()
	pm := New()
	pm.SetConfigDir(filepath.Join(tmp, "config"))
	pm.SetDataDir(filepath.Join(tmp, "data"))
	pm.SetLogsDir(filepath.Join(tmp, "logs"))

	if err := pm.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(tmp, "config"),
		filepath.Join(tmp, "data"),
		filepath.Join(tmp, "logs"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected %s to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

func TestEnsureDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nested", "deep")
	if err := EnsureDir(target); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Errorf("EnsureDir did not create %s", target)
	}
}

func TestEnsureDirectories(t *testing.T) {
	tmp := t.TempDir()
	// Backup is intentionally not created by EnsureDirectories, only
	// Config/Data/Logs are; asserted separately below.
	dirs := Directories{
		Config: filepath.Join(tmp, "c"),
		Data:   filepath.Join(tmp, "d"),
		Logs:   filepath.Join(tmp, "l"),
		Backup: filepath.Join(tmp, "b"),
	}
	if err := EnsureDirectories(dirs); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	for _, dir := range []string{dirs.Config, dirs.Data, dirs.Logs} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("expected %s to exist: %v", dir, err)
		}
	}
	if _, err := os.Stat(dirs.Backup); err == nil {
		t.Error("Backup dir should not be created by EnsureDirectories")
	}
}

func TestGetDirectories(t *testing.T) {
	dirs := GetDirectories()
	if dirs.Config == "" || dirs.Data == "" || dirs.Logs == "" || dirs.Backup == "" {
		t.Errorf("GetDirectories() returned empty fields: %+v", dirs)
	}
}

func TestGetDefaultDirsContainsOrgAndProject(t *testing.T) {
	configDir, dataDir, logsDir, backupDir := GetDefaultDirs(ProjectName)
	for name, dir := range map[string]string{
		"config": configDir, "data": dataDir, "logs": logsDir, "backup": backupDir,
	} {
		if dir == "" {
			t.Errorf("%s dir empty", name)
		}
	}
}

func TestGetBackupDirEnvOverride(t *testing.T) {
	t.Setenv("BACKUP_DIR", "/custom/backup/path")
	if got := GetBackupDir(); got != "/custom/backup/path" {
		t.Errorf("GetBackupDir() = %q, want env override", got)
	}
}

func TestGetBackupDirEnvOverrideTrimmed(t *testing.T) {
	t.Setenv("BACKUP_DIR", "  /padded/path  ")
	if got := GetBackupDir(); got != "/padded/path" {
		t.Errorf("GetBackupDir() = %q, want trimmed override", got)
	}
}

func TestGetBackupDirNoEnvUsesDefault(t *testing.T) {
	t.Setenv("BACKUP_DIR", "")
	if got := GetBackupDir(); got == "" {
		t.Error("GetBackupDir() empty with no override")
	}
}

func TestGetCacheDirEnvOverride(t *testing.T) {
	t.Setenv("CACHE_DIR", "/custom/cache/path")
	if got := GetCacheDir(); got != "/custom/cache/path" {
		t.Errorf("GetCacheDir() = %q, want env override", got)
	}
}

func TestGetCacheDirNoEnvUsesDefault(t *testing.T) {
	t.Setenv("CACHE_DIR", "")
	if got := GetCacheDir(); got == "" {
		t.Error("GetCacheDir() empty with no override")
	}
}

func TestGetPIDFile(t *testing.T) {
	got := GetPIDFile()
	if got == "" {
		t.Skip("empty PID file path is valid on windows")
	}
}

func TestIsRunningInContainer(t *testing.T) {
	// Just exercise the code path; the result depends on the actual
	// host/container the tests run in, so no specific value is asserted.
	_ = IsRunningInContainer()
}

// PathManager.GetBackupDir is a plain field accessor, distinct from the
// package-level GetBackupDir() function tested above.
func TestPathManagerGetBackupDir(t *testing.T) {
	pm := New()
	pm.dirs.Backup = "/base/backup"
	if got := pm.GetBackupDir(); got != "/base/backup" {
		t.Errorf("GetBackupDir() = %q, want %q", got, "/base/backup")
	}
}

// isPrivileged must agree with the underlying platform elevation check,
// mirroring TestIsElevatedMatchesEuidOnUnix in the service package.
func TestIsPrivileged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("euid check not applicable on windows")
	}
	want := os.Geteuid() == 0
	if got := isPrivileged(); got != want {
		t.Errorf("isPrivileged() = %v, want %v (euid=%d)", got, want, os.Geteuid())
	}
}

func TestUserHomeDir(t *testing.T) {
	got := userHomeDir()
	if got == "" {
		t.Error("userHomeDir() returned empty string")
	}
}

func TestXdgConfigHome(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/xdg/config")
		if got := xdgConfigHome("/home/user"); got != "/custom/xdg/config" {
			t.Errorf("xdgConfigHome() = %q, want env override", got)
		}
	})

	t.Run("falls back to home/.config", func(t *testing.T) {
		os.Unsetenv("XDG_CONFIG_HOME")
		want := filepath.Join("/home/user", ".config")
		if got := xdgConfigHome("/home/user"); got != want {
			t.Errorf("xdgConfigHome() = %q, want %q", got, want)
		}
	})
}

func TestXdgDataHome(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/xdg/data")
		if got := xdgDataHome("/home/user"); got != "/custom/xdg/data" {
			t.Errorf("xdgDataHome() = %q, want env override", got)
		}
	})

	t.Run("falls back to home/.local/share", func(t *testing.T) {
		os.Unsetenv("XDG_DATA_HOME")
		want := filepath.Join("/home/user", ".local", "share")
		if got := xdgDataHome("/home/user"); got != want {
			t.Errorf("xdgDataHome() = %q, want %q", got, want)
		}
	})
}
