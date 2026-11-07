package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBool(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		defaultVal bool
		want       bool
		wantErr    bool
	}{
		{"empty uses default true", "", true, true, false},
		{"empty uses default false", "", false, false, false},
		{"true", "true", false, true, false},
		{"yes", "yes", false, true, false},
		{"1", "1", false, true, false},
		{"on", "on", false, true, false},
		{"enabled uppercase", "ENABLED", false, true, false},
		{"whitespace trimmed", "  yes  ", false, true, false},
		{"false", "false", true, false, false},
		{"no", "no", true, false, false},
		{"0", "0", true, false, false},
		{"off", "off", true, false, false},
		{"invalid", "maybe", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBool(tc.in, tc.defaultVal)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseBool(%q) expected error, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBool(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseBool(%q, %v) = %v, want %v", tc.in, tc.defaultVal, got, tc.want)
			}
		})
	}
}

func TestMustParseBool(t *testing.T) {
	if got := MustParseBool("true", false); got != true {
		t.Errorf("MustParseBool(true) = %v", got)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseBool(invalid) should panic")
		}
	}()
	MustParseBool("not-a-bool", false)
}

func TestIsTruthy(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"yes", true},
		{"YES", true},
		{" true ", true},
		{"no", false},
		{"", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		if got := IsTruthy(tc.in); got != tc.want {
			t.Errorf("IsTruthy(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsFalsy(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"no", true},
		{"NO", true},
		{" false ", true},
		{"yes", false},
		{"", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		if got := IsFalsy(tc.in); got != tc.want {
			t.Errorf("IsFalsy(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}
	if cfg.Server.Branding.Title == "" {
		t.Error("expected non-empty default branding title")
	}
	if cfg.Web.UI.Theme == "" {
		t.Error("expected non-empty default theme")
	}
}

func TestDefaultTorConfig(t *testing.T) {
	tc := DefaultTorConfig()
	if tc.MaxCircuits != 32 {
		t.Errorf("MaxCircuits = %d, want 32", tc.MaxCircuits)
	}
	if !tc.SafeLogging {
		t.Error("expected SafeLogging true by default")
	}
	if tc.UseNetwork {
		t.Error("expected UseNetwork false by default")
	}
}

func TestDefaultGeoIPConfig(t *testing.T) {
	gc := DefaultGeoIPConfig()
	if !gc.Enabled {
		t.Error("expected GeoIP enabled by default")
	}
	if len(gc.DenyCountries) != 0 || len(gc.AllowCountries) != 0 {
		t.Error("expected empty country lists by default (no blocking)")
	}
	if !gc.Databases.ASN || !gc.Databases.Country || !gc.Databases.City || !gc.Databases.Whois {
		t.Errorf("expected all databases enabled by default, got %+v", gc.Databases)
	}
}

func TestDefaultNotificationsConfigStructure(t *testing.T) {
	n := DefaultNotificationsConfig()
	if n.Email.SMTP.Port == 0 {
		t.Error("expected non-zero default SMTP port")
	}
}

func TestGetTheme(t *testing.T) {
	mu.Lock()
	current = DefaultConfig()
	current.Web.UI.Theme = "midnight"
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = nil
		mu.Unlock()
	})

	if got := GetTheme(); got != "midnight" {
		t.Errorf("GetTheme() = %q, want midnight", got)
	}
}

func TestGetCORS(t *testing.T) {
	t.Run("explicit value", func(t *testing.T) {
		mu.Lock()
		current = DefaultConfig()
		current.Web.CORS = "https://example.com"
		mu.Unlock()
		t.Cleanup(func() {
			mu.Lock()
			current = nil
			mu.Unlock()
		})

		if got := GetCORS(); got != "https://example.com" {
			t.Errorf("GetCORS() = %q, want https://example.com", got)
		}
	})

	t.Run("empty defaults to wildcard", func(t *testing.T) {
		mu.Lock()
		current = DefaultConfig()
		current.Web.CORS = ""
		mu.Unlock()
		t.Cleanup(func() {
			mu.Lock()
			current = nil
			mu.Unlock()
		})

		if got := GetCORS(); got != "*" {
			t.Errorf("GetCORS() = %q, want *", got)
		}
	})
}

func TestGetWithNoCurrentReturnsDefault(t *testing.T) {
	mu.Lock()
	current = nil
	mu.Unlock()

	cfg := Get()
	if cfg == nil {
		t.Fatal("Get() returned nil when current is unset")
	}
	if cfg.Web.UI.Theme == "" {
		t.Error("Get() fallback should have default theme populated")
	}
}

func TestLoadCreatesDefaultConfigWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
	if cfg.Server.Port == "" {
		t.Error("expected Load to fill in a random port for a fresh config")
	}
	if cfg.Server.FQDN == "" {
		t.Error("expected Load to fill in a detected FQDN")
	}
	if cfg.Server.Admin.Email == "" {
		t.Error("expected Load to derive an admin email from FQDN")
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("expected config file to be written at %s: %v", path, statErr)
	}
}

func TestLoadParsesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")

	if _, err := Load(path); err != nil {
		t.Fatalf("first Load (create): %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("second Load (parse existing): %v", err)
	}
	if cfg.Server.Port == "" {
		t.Error("expected port to be preserved across reload")
	}
}

func TestSaveWithoutLoadedConfigReturnsError(t *testing.T) {
	mu.Lock()
	current = nil
	configPath = ""
	mu.Unlock()

	if err := Save(); err == nil {
		t.Error("expected Save() to error when no config is loaded")
	}
}

func TestUpdateWithoutLoadedConfigReturnsError(t *testing.T) {
	mu.Lock()
	current = nil
	mu.Unlock()

	if err := Update(func(c *Config) {}); err == nil {
		t.Error("expected Update() to error when no config is loaded")
	}
}

func TestUpdateAppliesAndSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")

	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() {
		mu.Lock()
		current = nil
		configPath = ""
		mu.Unlock()
	})

	if err := Update(func(c *Config) {
		c.Server.Branding.Title = "Updated Title"
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got := Get().Server.Branding.Title; got != "Updated Title" {
		t.Errorf("Get().Server.Branding.Title = %q, want Updated Title", got)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Server.Branding.Title != "Updated Title" {
		t.Errorf("reloaded title = %q, want persisted Updated Title", reloaded.Server.Branding.Title)
	}
}
