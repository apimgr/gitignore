package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Output != "text" {
		t.Errorf("Output = %q, want text", cfg.Output)
	}
	if cfg.Color != "auto" {
		t.Errorf("Color = %q, want auto", cfg.Color)
	}
	if cfg.Update.Auto != "no" || cfg.Update.Channel != "stable" {
		t.Errorf("Update = %+v, want {no stable}", cfg.Update)
	}
	if cfg.Display.Mode != "auto" {
		t.Errorf("Display.Mode = %q, want auto", cfg.Display.Mode)
	}
}

func TestDisplayModeValidationAndPersistence(t *testing.T) {
	for _, mode := range []string{"auto", "gui", "tui"} {
		if !IsValidDisplayMode(mode) {
			t.Errorf("IsValidDisplayMode(%q) = false", mode)
		}
	}
	if IsValidDisplayMode("cli") {
		t.Error("IsValidDisplayMode(cli) = true, want false")
	}

	path := filepath.Join(t.TempDir(), "cli.yml")
	cfg := Default()
	cfg.Display.Mode = "tui"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Display.Mode != "tui" {
		t.Errorf("Display.Mode = %q, want tui", loaded.Display.Mode)
	}
}

func TestLoadRejectsInvalidDisplayMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli.yml")
	if err := os.WriteFile(path, []byte("display:\n  mode: cli\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := Load(path, "es"); err == nil {
		t.Fatal("Load accepted an invalid display.mode")
	} else if err.Error() != "El valor de display.mode no es válido; usa auto, gui o tui" {
		t.Errorf("Load error = %q, want localized display mode message", err)
	}
}

func TestLoadParseErrorUsesRequestedLanguage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli.yml")
	if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := Load(path, "es"); err == nil {
		t.Fatal("Load accepted malformed configuration")
	} else if !strings.Contains(err.Error(), "No se pudo interpretar la configuración "+path) {
		t.Errorf("Load error = %q, want translated Spanish prefix", err)
	}
}

func TestIsValidServerURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"https://example.com", true},
		{"http://example.com", true},
		{"ftp://example.com", false},
		{"not-a-url", false},
		{"https://", false},
	}
	for _, tc := range cases {
		if got := IsValidServerURL(tc.in); got != tc.want {
			t.Errorf("IsValidServerURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSaveIfEmptyOrInvalid(t *testing.T) {
	alwaysValid := func(string) bool { return true }
	neverValid := func(string) bool { return false }

	t.Run("empty flag never saves", func(t *testing.T) {
		result, save := SaveIfEmptyOrInvalid("current", "", alwaysValid)
		if result != "current" || save {
			t.Errorf("got (%q, %v), want (\"current\", false)", result, save)
		}
	})

	t.Run("invalid flag never saves", func(t *testing.T) {
		result, save := SaveIfEmptyOrInvalid("current", "flag", neverValid)
		if result != "current" || save {
			t.Errorf("got (%q, %v), want (\"current\", false)", result, save)
		}
	})

	t.Run("valid flag with empty current saves", func(t *testing.T) {
		result, save := SaveIfEmptyOrInvalid("", "flag", alwaysValid)
		if result != "flag" || !save {
			t.Errorf("got (%q, %v), want (\"flag\", true)", result, save)
		}
	})

	t.Run("valid flag with invalid current saves", func(t *testing.T) {
		result, save := SaveIfEmptyOrInvalid("bad", "flag", func(s string) bool { return s == "flag" })
		if result != "flag" || !save {
			t.Errorf("got (%q, %v), want (\"flag\", true)", result, save)
		}
	})

	t.Run("valid flag with valid current does not save", func(t *testing.T) {
		result, save := SaveIfEmptyOrInvalid("existing", "flag", alwaysValid)
		if result != "flag" || save {
			t.Errorf("got (%q, %v), want (\"flag\", false)", result, save)
		}
	})
}

func TestResolveBoolIsTruthyIsFalsy(t *testing.T) {
	got, err := ResolveBool("yes", false)
	if err != nil || !got {
		t.Errorf("ResolveBool(yes) = (%v, %v), want (true, nil)", got, err)
	}
	if !IsTruthy("true") {
		t.Error("IsTruthy(true) = false, want true")
	}
	if !IsFalsy("no") {
		t.Error("IsFalsy(no) = false, want true")
	}
}

func TestResolveServer(t *testing.T) {
	t.Run("flag takes priority and strips trailing slash", func(t *testing.T) {
		got, err := ResolveServer("https://flag.example/", &Config{Server: ServerConfig{Primary: "https://cfg.example"}})
		if err != nil || got != "https://flag.example" {
			t.Errorf("got (%q, %v), want (\"https://flag.example\", nil)", got, err)
		}
	})

	t.Run("env var used when flag empty", func(t *testing.T) {
		t.Setenv("GITIGNORE_SERVER_PRIMARY", "https://env.example/")
		got, err := ResolveServer("", &Config{Server: ServerConfig{Primary: "https://cfg.example"}})
		if err != nil || got != "https://env.example" {
			t.Errorf("got (%q, %v), want (\"https://env.example\", nil)", got, err)
		}
	})

	t.Run("config used when flag and env empty", func(t *testing.T) {
		got, err := ResolveServer("", &Config{Server: ServerConfig{Primary: "https://cfg.example/"}})
		if err != nil || got != "https://cfg.example" {
			t.Errorf("got (%q, %v), want (\"https://cfg.example\", nil)", got, err)
		}
	})

	t.Run("nothing configured returns error", func(t *testing.T) {
		_, err := ResolveServer("", nil)
		if err == nil {
			t.Error("expected error when no server is configured")
		}
	})
}

func TestLoadCreatesDefaultOnFirstRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, path, err := Load("test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Output != "text" {
		t.Errorf("expected default config, got %+v", cfg)
	}
	if filepath.Base(path) != "test.yml" {
		t.Errorf("path = %q, want basename test.yml", path)
	}

	cfg2, _, err := Load("test")
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if cfg2.Output != cfg.Output {
		t.Errorf("reload mismatch: %+v vs %+v", cfg2, cfg)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := Default()
	cfg.Server.Primary = "https://example.com"
	cfg.Color = "no"

	path := filepath.Join(home, "custom.yml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Server.Primary != "https://example.com" || loaded.Color != "no" {
		t.Errorf("loaded = %+v, want Server.Primary=https://example.com Color=no", loaded)
	}
}
