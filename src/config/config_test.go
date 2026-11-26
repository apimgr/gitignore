package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadKeepsLastGoodConfigurationAfterParseFailure(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  mode: development\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("initial Load() failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("server: [invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() rejected malformed config instead of using the last good copy: %v", err)
	}
	if cfg.Server.Mode != "development" {
		t.Fatalf("mode = %q, want last-good value development", cfg.Server.Mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "server: [invalid\n" {
		t.Fatalf("malformed config was overwritten: %q", data)
	}
}

func TestLoadRemovesDeprecatedServiceIdentitySettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	content := "server:\n  port: '64211'\n  user: custom-user\n  group: custom-group\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "user:") || strings.Contains(string(data), "group:") {
		t.Fatalf("deprecated service identity keys were retained: %s", data)
	}
}

func TestLoadIgnoresMalformedDropinWithoutDiscardingOtherFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  mode: development\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dropin := filepath.Join(directory, "yaml.d", "20-app.yml")
	if err := os.MkdirAll(filepath.Dir(dropin), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, []byte("server: [invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() rejected malformed drop-in: %v", err)
	}
	if cfg.Server.Mode != "development" {
		t.Fatalf("mode = %q, want value from server.yml", cfg.Server.Mode)
	}
}

func TestLoadInvalidUpdateBranchUsesDefaultAndTracksSourceLine(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  update:\n    branch: nightly\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() rejected invalid update branch: %v", err)
	}
	if cfg.Server.Update.Branch != DefaultConfig().Server.Update.Branch {
		t.Fatalf("update branch = %q, want default %q", cfg.Server.Update.Branch, DefaultConfig().Server.Update.Branch)
	}
	if location := configValueLocations["server.update.branch"]; location.file != "maintenance.yml" || location.line != 4 {
		t.Fatalf("invalid branch location = %#v, want maintenance.yml:4", location)
	}
}

func TestApplyYAMLConfigFileKeepsValidFieldsAndDefaultsInvalidFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20-server.yml")
	cfg := DefaultConfig()
	data := []byte("server:\n  mode: production\n  daemonize: not-a-bool\n  fqdn: null\n")
	if err := applyYAMLConfigFile(path, data, cfg); err != nil {
		t.Fatalf("applyYAMLConfigFile() error = %v", err)
	}
	if cfg.Server.Mode != "production" {
		t.Errorf("mode = %q, want valid sibling value production", cfg.Server.Mode)
	}
	if cfg.Server.Daemonize != DefaultConfig().Server.Daemonize {
		t.Errorf("daemonize = %v, want built-in default %v", cfg.Server.Daemonize, DefaultConfig().Server.Daemonize)
	}
	if cfg.Server.FQDN != DefaultConfig().Server.FQDN {
		t.Errorf("fqdn = %q, want built-in default %q", cfg.Server.FQDN, DefaultConfig().Server.FQDN)
	}
}

func TestApplyYAMLConfigFileDuplicateKeyDefaultsOnlyDuplicateField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20-server.yml")
	cfg := DefaultConfig()
	data := []byte("server:\n  mode: development\n  mode: production\n  fqdn: example.test\n")
	if err := applyYAMLConfigFile(path, data, cfg); err != nil {
		t.Fatalf("applyYAMLConfigFile() error = %v", err)
	}
	if cfg.Server.Mode != DefaultConfig().Server.Mode {
		t.Errorf("mode = %q, want built-in default %q", cfg.Server.Mode, DefaultConfig().Server.Mode)
	}
	if cfg.Server.FQDN != "example.test" {
		t.Errorf("fqdn = %q, want valid sibling value example.test", cfg.Server.FQDN)
	}
}

func TestWebRobotsAndSecurityUseCanonicalNestedConfigWithLegacyReadSupport(t *testing.T) {
	cfg := DefaultConfig()
	canonical := []byte("web:\n  robots:\n    allow: [/public]\n  security:\n    contact: security@example.test\n")
	if err := applyYAMLConfigFile(filepath.Join(t.TempDir(), "web.yml"), canonical, cfg); err != nil {
		t.Fatalf("apply canonical web config: %v", err)
	}
	if len(cfg.Web.Robots.Allow) != 1 || cfg.Web.Robots.Allow[0] != "/public" {
		t.Fatalf("web.robots.allow = %#v", cfg.Web.Robots.Allow)
	}
	if cfg.Web.Security.Contact != "security@example.test" {
		t.Fatalf("web.security.contact = %q", cfg.Web.Security.Contact)
	}

	legacy := []byte("web_robots:\n  allow: [/legacy]\nweb_security:\n  contact: legacy@example.test\n")
	if err := applyYAMLConfigFile(filepath.Join(t.TempDir(), "legacy.yml"), legacy, cfg); err != nil {
		t.Fatalf("apply legacy web config: %v", err)
	}
	if len(cfg.Web.Robots.Allow) != 1 || cfg.Web.Robots.Allow[0] != "/legacy" {
		t.Fatalf("legacy web_robots did not migrate: %#v", cfg.Web.Robots.Allow)
	}
	if cfg.Web.Security.Contact != "legacy@example.test" {
		t.Fatalf("legacy web_security did not migrate: %q", cfg.Web.Security.Contact)
	}
}

func TestAllowlistDescriptionIsOptional(t *testing.T) {
	cfg := DefaultConfig()
	data := []byte("server:\n  security:\n    allowlist:\n      - cidr: 192.0.2.1\n")
	if err := applyYAMLConfigFile(filepath.Join(t.TempDir(), "security.yml"), data, cfg); err != nil {
		t.Fatalf("applyYAMLConfigFile() error = %v", err)
	}
	if len(cfg.Server.Security.Allowlist) != 1 || cfg.Server.Security.Allowlist[0].Description != "" {
		t.Fatalf("allowlist = %#v, want one entry with an omitted description", cfg.Server.Security.Allowlist)
	}
}

func TestUnknownConfigurationKeyIsReportedWithLocation(t *testing.T) {
	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	cfg := DefaultConfig()
	applyErr := applyYAMLConfigFile(filepath.Join(t.TempDir(), "web.yml"), []byte("server:\n  unknown_setting: true\n"), cfg)
	os.Stderr = previousStderr
	_ = writer.Close()
	if applyErr != nil {
		t.Fatalf("apply config: %v", applyErr)
	}
	message, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if !strings.Contains(string(message), "unknown configuration key server.unknown_setting") || !strings.Contains(string(message), "web.yml:2") {
		t.Fatalf("unknown-key diagnostic = %q", message)
	}
}

func TestApplyYAMLConfigFileDecodesInlineAuditLogFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20-logs.yml")
	cfg := DefaultConfig()
	data := []byte("server:\n  logs:\n    audit:\n      enabled: false\n      format: json\n      events:\n        security: false\n")
	if err := applyYAMLConfigFile(path, data, cfg); err != nil {
		t.Fatalf("applyYAMLConfigFile() error = %v", err)
	}
	if cfg.Server.Logging.Audit.Enabled == nil || *cfg.Server.Logging.Audit.Enabled {
		t.Errorf("audit enabled = %v, want false", cfg.Server.Logging.Audit.Enabled)
	}
	if cfg.Server.Logging.Audit.Format != "json" {
		t.Errorf("audit format = %q, want json", cfg.Server.Logging.Audit.Format)
	}
	if cfg.Server.Logging.Audit.Events.Security {
		t.Error("audit security event = true, want false")
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
	if cfg.Web.Reports.RateLimitPerMinute != 60 || cfg.Web.Reports.RateLimitPerIPBurst != 10 {
		t.Errorf("web report limits = %d/minute with burst %d, want 60/minute and 10 burst", cfg.Web.Reports.RateLimitPerMinute, cfg.Web.Reports.RateLimitPerIPBurst)
	}
	if pool := cfg.Server.Database.Pool; pool.MaxOpen != 25 || pool.MaxIdle != 5 || pool.MaxLifetime != "5m" || pool.MaxIdleTime != "1m" {
		t.Errorf("database pool defaults = %+v, want 25 open, 5 idle, 5m lifetime, 1m idle time", pool)
	}
	if cache := cfg.Server.Database.Cache; cache.Enabled || cache.SyncInterval != "30s" || !cache.OfflineMode || cache.MaxAge != "1h" {
		t.Errorf("database cache defaults = %+v", cache)
	}
	if retention := cfg.Server.Backup.Retention; retention.MaxBackups != 1 || retention.MaxTotalSize != "10%" {
		t.Errorf("backup retention defaults = %+v", retention)
	}
	if restore := cfg.Server.Restore; restore.MaxArchiveSize != "1G" || restore.MaxExpandedSize != "4G" || restore.MaxFiles != 10000 || restore.MaxFileSize != "512M" || restore.MaxRatio != 100 {
		t.Errorf("restore defaults = %+v", restore)
	}
}

func TestGeneratedConfigUsesCanonicalSchedulerAndLogs(t *testing.T) {
	generated := generateConfigYAML(DefaultConfig())
	for _, required := range []string{
		"# This node only:", "server:", "  address:", "  api_version:", "  database:", "    driver:", "    pool:", "      max_open: 25", "      max_idle: 5",
	} {
		if !strings.Contains(generated, required) {
			t.Errorf("generated config missing %q", required)
		}
	}
	for _, retired := range []string{"  scheduler:", "  logs:", "  branding:", "  metrics:", "  maintenance:"} {
		if strings.Contains(generated, retired) {
			t.Errorf("generated node config contains area setting %q", retired)
		}
	}
}

func TestMigrateAreaConfigSplitsSectionsAndPreservesDropinPrecedence(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  port: 64001\n  token: adm_legacy\n  branding:\n    title: Moved\n    tagline: From server\n  security:\n    encryption_key: a2V5\n  metrics:\n    auth:\n      tokens:\n        prometheus: mtr_prometheus\n  scheduler:\n    timezone: UTC\n  logs:\n    level: debug\nweb:\n  ui:\n    theme: light\ndata:\n  cve:\n    enabled: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	areaDir := filepath.Join(directory, "yaml.d")
	if err := os.Mkdir(areaDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(areaDir, "branding.yml"), []byte("server:\n  branding:\n    title: Existing wins\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateAreaConfig(path); err != nil {
		t.Fatal(err)
	}
	mainData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainData), "port: 64001") || strings.Contains(string(mainData), "branding:") || strings.Contains(string(mainData), "scheduler:") {
		t.Fatalf("server.yml was not reduced to node settings: %s", mainData)
	}
	for _, filename := range []string{"branding.yml", "maintenance.yml", "logs.yml", "security.yml"} {
		if _, err := os.Stat(filepath.Join(areaDir, filename)); err != nil {
			t.Errorf("expected migrated area file %s: %v", filename, err)
		}
	}
	for _, filename := range []string{"server.yml", "secrets.yml"} {
		if _, err := os.Stat(filepath.Join(directory, filename)); err != nil {
			t.Errorf("expected migration file %s: %v", filename, err)
		}
	}
	secretData, err := os.ReadFile(filepath.Join(directory, "secrets.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"adm_legacy", "a2V5", "mtr_prometheus"} {
		if !strings.Contains(string(secretData), secret) {
			t.Errorf("secrets.yml missing migrated credential %q: %s", secret, secretData)
		}
	}
	securityData, err := os.ReadFile(filepath.Join(areaDir, "security.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(securityData), "a2V5") {
		t.Fatalf("encryption key was copied to an area file: %s", securityData)
	}
	cfg := DefaultConfig()
	if err := applyYAMLConfigFile(path, mainData, cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadYAMLDropins(path, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Branding.Title != "Existing wins" || cfg.Server.Branding.Tagline != "From server" {
		t.Fatalf("branding after split = %#v", cfg.Server.Branding)
	}
	backup, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatalf("migration backup missing: %v", err)
	}
	if backup.Mode().Perm() != 0600 {
		t.Fatalf("backup mode = %o, want 600", backup.Mode().Perm())
	}
}

func TestDefaultTorConfig(t *testing.T) {
	tc := DefaultTorConfig()
	if tc.Expose || tc.ContactEmail != "" {
		t.Errorf("Tor exposure/contact defaults = %t/%q, want false/empty", tc.Expose, tc.ContactEmail)
	}
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

func TestPeekStartupSettingsMergesOnlyStartupFields(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: '64123'\n  address: 127.0.0.1\n  daemonize: true\n  pidfile: false\nsecrets:\n  token: never-read\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropins := filepath.Join(dir, "yaml.d")
	if err := os.Mkdir(dropins, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropins, "20-startup.yml"), []byte("server:\n  port: '64234'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := PeekStartupSettings(configPath)
	if settings.Port != "64234" || settings.Address != "127.0.0.1" || !settings.Daemonize || settings.PIDFile {
		t.Fatalf("startup settings = %+v", settings)
	}
}

func TestPeekStartupSettingsInvalidValuesUseDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: '64123'\n  address: 127.0.0.1\n  daemonize: true\n  pidfile: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropins := filepath.Join(dir, "yaml.d")
	if err := os.Mkdir(dropins, 0o700); err != nil {
		t.Fatal(err)
	}
	invalid := "server:\n  port: null\n  address: 0.0.0.0\n  daemonize: true\n  daemonize: false\n  pidfile: perhaps\n"
	if err := os.WriteFile(filepath.Join(dropins, "20-invalid.yml"), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := PeekStartupSettings(configPath)
	defaults := DefaultConfig().Server
	if settings.Port != defaults.Port || settings.Address != "0.0.0.0" || settings.Daemonize != defaults.Daemonize || settings.PIDFile != defaults.PIDFile {
		t.Fatalf("invalid startup values = %+v, want invalid keys reset to defaults", settings)
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
	if len(gc.Presets) != 0 {
		t.Errorf("expected no country presets by default, got %+v", gc.Presets)
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

func TestLoadMergesYAMLDropinsInLexicalOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  branding:\n    title: Main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dropinDir := filepath.Join(dir, "yaml.d")
	if err := os.Mkdir(dropinDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"10-branding.yml":  "server:\n  branding:\n    title: First\n    tagline: Drop-in tagline\n",
		"20-branding.yaml": "server:\n  branding:\n    title: Last\n",
	} {
		if err := os.WriteFile(filepath.Join(dropinDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Branding.Title != "Last" || cfg.Server.Branding.Tagline != "Drop-in tagline" {
		t.Fatalf("merged branding = %#v, want last title and preserved tagline", cfg.Server.Branding)
	}
}

func TestLoadSeedsDatabaseAndCacheConnectionsOnce(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	t.Setenv("DATABASE_URL", "libsql://db.example.test?authToken=first-token")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("CACHE_URL", "valkey://cache.example.test:6379/0")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if cfg.Server.Database.Driver != "libsql" || cfg.Server.Database.URL != "libsql://db.example.test?authToken=first-token" {
		t.Errorf("first-run database = (%q, %q), want URL-selected libsql connection", cfg.Server.Database.Driver, cfg.Server.Database.URL)
	}
	if cfg.Server.Cache.Type != "valkey" || cfg.Server.Cache.URL != "valkey://cache.example.test:6379/0" {
		t.Errorf("first-run cache = (%q, %q), want URL-selected valkey connection", cfg.Server.Cache.Type, cfg.Server.Cache.URL)
	}

	serverFile, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read server.yml: %v", err)
	}
	if strings.Contains(string(serverFile), "first-token") || strings.Contains(string(serverFile), "authToken") {
		t.Fatal("credential-bearing database URL was written to server.yml")
	}
	secretsFile, err := os.ReadFile(filepath.Join(directory, "secrets.yml"))
	if err != nil {
		t.Fatalf("read secrets.yml: %v", err)
	}
	if !strings.Contains(string(secretsFile), "authToken=first-token") {
		t.Fatal("credential-bearing database URL was not persisted to secrets.yml")
	}

	t.Setenv("DATABASE_URL", "libsql://changed.example.test?authToken=second-token")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("CACHE_URL", "redis://changed.example.test:6379/1")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if cfg.Server.Database.URL != "libsql://db.example.test?authToken=first-token" {
		t.Errorf("DATABASE_URL overrode the saved connection: %q", cfg.Server.Database.URL)
	}
	if cfg.Server.Cache.URL != "valkey://cache.example.test:6379/0" || cfg.Server.Cache.Type != "valkey" {
		t.Errorf("CACHE_URL overrode the saved connection: (%q, %q)", cfg.Server.Cache.Type, cfg.Server.Cache.URL)
	}
}

func TestLoadYAMLDropinsFiltersNamesAndSymlinkTargets(t *testing.T) {
	dir := t.TempDir()
	dropinDir := filepath.Join(dir, "yaml.d")
	if err := os.Mkdir(dropinDir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"10-visible.yml":         "server:\n  branding:\n    title: Visible\n",
		".hidden.yml":            "server:\n  branding:\n    title: Hidden\n",
		"20-backup.yml.orig":     "server:\n  branding:\n    title: Backup\n",
		"30-external-source.yml": "server:\n  branding:\n    title: External\n",
		"40-internal-source.yml": "server:\n  branding:\n    title: Internal\n",
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if name != "30-external-source.yml" {
			path = filepath.Join(dropinDir, name)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "30-external-source.yml"), filepath.Join(dropinDir, "30-external.yml")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(dropinDir, "40-internal-source.yml"), filepath.Join(dropinDir, "40-internal.yml")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Server.Branding.Title = "Main"
	if err := loadYAMLDropins(filepath.Join(dir, "server.yml"), cfg); err != nil {
		t.Fatalf("loadYAMLDropins: %v", err)
	}
	if cfg.Server.Branding.Title != "Internal" {
		t.Fatalf("branding title = %q, want internal symlink value", cfg.Server.Branding.Title)
	}
}

func TestLoadUsesCanonicalSchedulerAndLogPaths(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	data := []byte("server:\n  scheduler:\n    timezone: UTC\n    catch_up_window: 2h\n  logs:\n    level: debug\n    access:\n      format: json\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Schedule.Timezone != "UTC" || cfg.Server.Schedule.CatchUpWindow != "2h" {
		t.Fatalf("scheduler config = %+v", cfg.Server.Schedule)
	}
	if cfg.Server.Logging.Level != "debug" || cfg.Server.Logging.Access.Format != "json" {
		t.Fatalf("logs config = %+v", cfg.Server.Logging)
	}
}

func TestLoadPreservesLegacySchedulerAndLogSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	data := []byte("server:\n  schedule:\n    timezone: UTC\n    catch_up_window: 3h\n  logging:\n    level: warn\n    access_format: json\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Schedule.Timezone != "UTC" || cfg.Server.Schedule.CatchUpWindow != "3h" {
		t.Fatalf("legacy scheduler config = %+v", cfg.Server.Schedule)
	}
	if cfg.Server.Logging.Level != "warn" || cfg.Server.Logging.Access.Format != "json" {
		t.Fatalf("legacy logs config = %+v", cfg.Server.Logging)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{"\n  schedule:\n", "\n  logging:\n", "access_format:"} {
		if strings.Contains(string(migrated), retired) {
			t.Errorf("migrated server.yml still contains legacy key %q:\n%s", retired, migrated)
		}
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("legacy config backup missing: %v", err)
	}
	if !strings.Contains(string(backup), "schedule:") || !strings.Contains(string(backup), "logging:") {
		t.Errorf("legacy config backup did not preserve the original file:\n%s", backup)
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
