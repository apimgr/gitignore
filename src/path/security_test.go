package path

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
		err   error
	}{
		{input: "/my//admin/", want: "my/admin"},
		{input: "", want: ""},
		{input: "/server/../secret", err: ErrPathTraversal},
		{input: "/Admin", err: ErrInvalidPath},
		{input: "/" + string(make([]byte, 65)), err: ErrPathTooLong},
	}
	for _, test := range tests {
		got, err := SafePath(test.input)
		if test.err != nil {
			if !errors.Is(err, test.err) {
				t.Errorf("SafePath(%q) error = %v, want %v", test.input, err, test.err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("SafePath(%q) = %q, %v; want %q", test.input, got, err, test.want)
		}
	}
}

func TestSafeResourcePath(t *testing.T) {
	if got, err := SafeResourcePath("/static/css/Main_1.css"); err != nil || got != "static/css/Main_1.css" {
		t.Fatalf("SafeResourcePath() = %q, %v", got, err)
	}
	for _, input := range []string{"../secret", "a\\b", ".hidden", ""} {
		if _, err := SafeResourcePath(input); err == nil {
			t.Errorf("SafeResourcePath(%q) accepted invalid path", input)
		}
	}
}

func TestSafeDirPath(t *testing.T) {
	got, err := SafeDirPath("relative/path")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("SafeDirPath() = %q, want absolute path", got)
	}
	for _, input := range []string{"", "../outside", "bad\npath"} {
		if _, err := SafeDirPath(input); err == nil {
			t.Errorf("SafeDirPath(%q) accepted invalid path", input)
		}
	}
}

func TestSafeFilePath(t *testing.T) {
	base := t.TempDir()
	got, err := SafeFilePath(base, "assets/theme.css")
	if err != nil || got != filepath.Join(base, "assets", "theme.css") {
		t.Errorf("SafeFilePath() = %q, %v", got, err)
	}
	if _, err := SafeFilePath(base, "../outside"); !errors.Is(err, ErrPathTraversal) {
		t.Errorf("SafeFilePath traversal error = %v", err)
	}
}

func TestEnforcePrivateFixesPrivateTreeModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses inherited ACLs instead of chmod modes")
	}
	configDir := filepath.Join(t.TempDir(), "config")
	dataDir := filepath.Join(t.TempDir(), "data")
	files := []string{
		filepath.Join(configDir, "yaml.d", "nested", "site.yml"), filepath.Join(configDir, "assets", "secret.txt"),
		filepath.Join(configDir, "ssl", "key.pem"), filepath.Join(configDir, "tor", "site", "hostname"),
		filepath.Join(configDir, "i2p", "key.dat"), filepath.Join(dataDir, "security", "geoip", "database.mmdb"),
		filepath.Join(dataDir, "tor", "site", "hostname"), filepath.Join(configDir, "server.yml"),
		filepath.Join(configDir, "secrets.yml"),
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("private"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnforcePrivate(configDir, dataDir); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Errorf("%s mode = %04o, want 0600", file, got)
		}
	}
	dirs := []string{
		filepath.Join(configDir, "yaml.d"), filepath.Join(configDir, "yaml.d", "nested"),
		filepath.Join(configDir, "assets"), filepath.Join(configDir, "ssl"), filepath.Join(configDir, "tor"),
		filepath.Join(configDir, "tor", "site"), filepath.Join(configDir, "i2p"), filepath.Join(dataDir, "security"),
		filepath.Join(dataDir, "security", "geoip"), filepath.Join(dataDir, "tor"), filepath.Join(dataDir, "tor", "site"),
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0700 {
			t.Errorf("%s mode = %04o, want 0700", dir, got)
		}
	}
}
