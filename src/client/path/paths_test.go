package path

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDirDataDirCacheDirLogDirUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}

	cases := []struct {
		name string
		fn   func() string
	}{
		{"ConfigDir", ConfigDir},
		{"DataDir", DataDir},
		{"CacheDir", CacheDir},
		{"LogDir", LogDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.fn()
			if !strings.HasPrefix(dir, home) {
				t.Errorf("%s() = %q, want prefix %q", tc.name, dir, home)
			}
			if !strings.Contains(dir, orgName) || !strings.Contains(dir, projectName) {
				t.Errorf("%s() = %q, want it to contain %q and %q", tc.name, dir, orgName, projectName)
			}
		})
	}
}

func TestLogFile(t *testing.T) {
	got := LogFile()
	want := filepath.Join(LogDir(), "cli.log")
	if got != want {
		t.Errorf("LogFile() = %q, want %q", got, want)
	}
}

func TestConfigFile(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to cli", "", filepath.Join(ConfigDir(), "cli.yml")},
		{"bare name gets yml extension", "myprofile", filepath.Join(ConfigDir(), "myprofile.yml")},
		{"name with extension kept as-is", "profile.yaml", filepath.Join(ConfigDir(), "profile.yaml")},
		{"absolute path returned unchanged", "/etc/gitignore/cli.yml", "/etc/gitignore/cli.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfigFile(tc.in); got != tc.want {
				t.Errorf("ConfigFile(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnsureDirsCreatesAllDirsWithUserOnlyPerms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	for _, dir := range []string{ConfigDir(), DataDir(), CacheDir(), LogDir()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s perm = %o, want 0700", dir, perm)
		}
	}
}
