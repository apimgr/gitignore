package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestMaintenanceRetryIntervalAcceptsDurationAndLegacySeconds(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  RetryInterval
	}{
		{"30s", 30},
		{"2m", 120},
		{"45", 45},
	} {
		var cfg struct {
			Server struct {
				Maintenance MaintenanceConfig `yaml:"maintenance"`
			} `yaml:"server"`
		}
		if err := yaml.Unmarshal([]byte("server:\n  maintenance:\n    self_healing:\n      retry_interval: "+tc.value+"\n"), &cfg); err != nil {
			t.Fatalf("unmarshal retry_interval %q: %v", tc.value, err)
		}
		if got := cfg.Server.Maintenance.SelfHealing.RetryInterval; got != tc.want {
			t.Errorf("retry_interval %q = %d, want %d seconds", tc.value, got, tc.want)
		}
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

func TestMaintenanceAreaLoadsCLIDistributionSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	if err := os.WriteFile(path, []byte("server: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	areaDir := filepath.Join(directory, "yaml.d")
	if err := os.MkdirAll(areaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	area := "server:\n  update:\n    cli_min_version: 1.2.3\ncli:\n  binary_download:\n    dir: /srv/gitignore/cli\n    require_auth: true\n"
	if err := os.WriteFile(filepath.Join(areaDir, "maintenance.yml"), []byte(area), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Update.CLIMinVersion != "1.2.3" {
		t.Errorf("CLI minimum version = %q", cfg.Server.Update.CLIMinVersion)
	}
	if cfg.CLI.BinaryDownload.Dir != "/srv/gitignore/cli" || !cfg.CLI.BinaryDownload.RequireAuth {
		t.Errorf("CLI binary download config = %+v", cfg.CLI.BinaryDownload)
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

func TestApplyI2PEnvironmentUsesExtendedBooleanValues(t *testing.T) {
	t.Setenv("I2P_ENABLED", "affirmative")
	cfg := DefaultConfig()
	if err := applyI2PEnvironment(cfg); err != nil {
		t.Fatalf("applyI2PEnvironment: %v", err)
	}
	if !cfg.Server.I2P.Enabled {
		t.Fatal("affirmative I2P_ENABLED value was not accepted")
	}
}

func TestI2PExposeDefaultsFalseAndRoundTrips(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.I2P.Expose {
		t.Fatal("I2P expose default must be false")
	}
	encoded, err := yaml.Marshal(cfg.Server.I2P)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "expose: false") {
		t.Fatalf("I2P expose missing from serialized config: %s", encoded)
	}
	var parsed I2PConfig
	if err := yaml.Unmarshal([]byte("expose: true\n"), &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.Expose {
		t.Fatal("I2P expose setting did not deserialize")
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
	if n.WebUI.Position != "top-right" || n.WebUI.Duration != 3 || n.WebUI.WarningDuration != 5 {
		t.Fatalf("WebUI toast defaults = %+v, want top-right, 3 seconds and 5 seconds", n.WebUI)
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

func TestMigrateLegacyContentAndObsoleteKeys(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	data := []byte("server:\n  branding:\n    footer_text: '<unsafe & text>'\n  pages:\n    about:\n      content: '# About'\n  security_headers:\n    hsts: true\n    hsts_max_age: 3600\n    frame_options: DENY\n  security:\n    headers:\n      clear_site_data:\n        enabled: true\n      obsolete: true\n  rate_limit:\n    enabled: false\nweb:\n  footer:\n    custom_html: '<b>new footer</b>'\n  llms:\n    enabled: false\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyServerConfig(path); err != nil {
		t.Fatalf("migrate legacy config: %v", err)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(migrated, &document); err != nil {
		t.Fatalf("parse migrated config: %v", err)
	}
	root := document.Content[0]
	footer := mappingConfigPath(root, []string{"server", "branding", "content", "footer"})
	footerFormat, footerBody := mappingValue(footer, "format"), mappingValue(footer, "body")
	if footer == nil || footerFormat == nil || footerBody == nil || footerFormat.Value != "html" || footerBody.Value != "<b>new footer</b>" {
		t.Fatalf("footer migration = %#v, want existing canonical destination to win", footer)
	}
	about := mappingConfigPath(root, []string{"server", "branding", "content", "about"})
	aboutFormat, aboutBody := mappingValue(about, "format"), mappingValue(about, "body")
	if about == nil || aboutFormat == nil || aboutBody == nil || aboutFormat.Value != "md" || aboutBody.Value != "# About" {
		t.Fatalf("about page migration = %#v", about)
	}
	if mappingConfigPath(root, []string{"web", "llms", "enabled"}) != nil || mappingConfigPath(root, []string{"server", "rate_limit", "enabled"}) != nil {
		t.Fatal("obsolete config keys were retained")
	}
	if mappingConfigPath(root, []string{"web", "hsts", "enabled"}) == nil || mappingConfigPath(root, []string{"web", "hsts", "max_age_seconds"}) == nil {
		t.Fatal("security header values were not moved to canonical paths")
	}
	if mappingConfigPath(root, []string{"web", "headers", "clear_site_data", "enabled"}) == nil {
		t.Fatal("clear_site_data was not preserved at its canonical path")
	}
	if mappingConfigPath(root, []string{"server", "security", "headers", "obsolete"}) != nil || mappingConfigPath(root, []string{"server", "security_headers", "frame_options"}) != nil {
		t.Fatal("obsolete security header keys were retained")
	}
}

func TestMigrateLegacySchedulerTasksRemovesUnknownAndDefaultValues(t *testing.T) {
	var document yaml.Node
	data := []byte("server:\n  scheduler:\n    tasks:\n      geoip_update:\n        enabled: true\n        schedule: '0 3 * * 0'\n        retry_on_fail: true\n        retry_delay: 1h\n        retention: 5\n      backup_hourly:\n        enabled: false\n        schedule: '@hourly'\n      custom_task:\n        enabled: true\n        retry_delay: 5m\n        retention: 3\n")
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if !migrateLegacyKeys(document.Content[0]) {
		t.Fatal("expected scheduler task migration to change the document")
	}
	tasks := mappingConfigPath(document.Content[0], []string{"server", "scheduler", "tasks"})
	if mappingValue(tasks, "geoip_update") != nil || mappingValue(tasks, "backup_hourly") != nil {
		t.Fatal("built-in tasks with default settings should be omitted")
	}
	custom := mappingValue(tasks, "custom_task")
	if custom == nil || mappingValue(custom, "retention") != nil || mappingValue(custom, "retry_delay").Value != "5m" {
		t.Fatalf("custom task keys = %#v", custom)
	}
}

func TestMigrateLegacyDualPortAndSEOEmptyDestinations(t *testing.T) {
	var document yaml.Node
	data := []byte("server:\n  port: 8080\n  https_port: 8443\n  seo:\n    og_image: ''\n    opengraph:\n      image: https://example.test/image.png\n    twitter_handle: ''\n    twitter:\n      site: '@example'\n")
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if !migrateLegacyKeys(document.Content[0]) {
		t.Fatal("expected legacy port and SEO migration")
	}
	root := document.Content[0]
	port := mappingConfigPath(root, []string{"server", "port"})
	if port == nil || port.Value != "8080,8443" || port.Tag != "!!str" || port.Style&yaml.DoubleQuotedStyle == 0 {
		t.Fatalf("migrated port = %#v", port)
	}
	seo := mappingConfigPath(root, []string{"server", "seo"})
	if mappingValue(seo, "og_image").Value != "https://example.test/image.png" || mappingValue(seo, "twitter_handle").Value != "@example" {
		t.Fatalf("SEO values were not filled from legacy empty destinations: %#v", seo)
	}
	if mappingConfigPath(root, []string{"server", "https_port"}) != nil || mappingConfigPath(root, []string{"server", "seo", "opengraph"}) != nil || mappingConfigPath(root, []string{"server", "seo", "twitter"}) != nil {
		t.Fatal("legacy port or SEO paths remain")
	}
}

func TestLegacySecretMigrationStagesValuesAndRemovesOnlyAfterImport(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "server.yml")
	data := []byte("server:\n  security:\n    installation_secret: a2V5\n  token: operator-token\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	securityArea := filepath.Join(directory, "yaml.d", "security.yml")
	if err := os.MkdirAll(filepath.Dir(securityArea), 0o700); err != nil {
		t.Fatal(err)
	}
	areaData := []byte("server:\n  security:\n    installation_secret_previous_expires: 2030-01-01T00:00:00Z\n    encryption_key_previous: cHJldmlvdXM=\n")
	if err := os.WriteFile(securityArea, areaData, 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := extractLegacySecretsFromConfig(path)
	if secrets["installation_secret"] != "a2V5" || secrets["installation_secret_previous_until"] != "2030-01-01T00:00:00Z" || secrets["encryption_key_previous"] != "cHJldmlvdXM=" {
		t.Fatalf("legacy secrets = %#v", secrets)
	}
	if err := CompleteLegacySecretMigration(path); err != nil {
		t.Fatal(err)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migrated), "installation_secret") || strings.Contains(string(migrated), "encryption_key_previous") {
		t.Fatalf("legacy secret keys remain after completion: %s", migrated)
	}
	if !strings.Contains(string(migrated), "operator-token") {
		t.Fatal("unrelated operator token setting was removed")
	}
	areaMigrated, err := os.ReadFile(securityArea)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(areaMigrated), "installation_secret") || strings.Contains(string(areaMigrated), "encryption_key_previous") {
		t.Fatalf("legacy secret keys remain in security area after completion: %s", areaMigrated)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || !strings.Contains(string(backup), "installation_secret") {
		t.Fatalf("legacy secret backup missing or incomplete: %v", err)
	}
	if _, err := os.Stat(securityArea + ".bak"); err != nil {
		t.Fatalf("security area secret backup missing: %v", err)
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

func TestValidateTorConfigUsesSpecifiedRanges(t *testing.T) {
	for _, tc := range []struct {
		name           string
		startupTimeout int
		introPoints    int
		wantTimeout    int
		wantIntro      int
	}{
		{name: "minimums", startupTimeout: 10, introPoints: 3, wantTimeout: 10, wantIntro: 3},
		{name: "maximums", startupTimeout: 300, introPoints: 10, wantTimeout: 300, wantIntro: 10},
		{name: "below minimums", startupTimeout: 9, introPoints: 2, wantTimeout: 60, wantIntro: 3},
		{name: "above maximums", startupTimeout: 301, introPoints: 11, wantTimeout: 60, wantIntro: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Server.Tor.StartupTimeout = tc.startupTimeout
			cfg.Server.Tor.NumIntroPoints = tc.introPoints
			validateTorConfig(cfg)
			if cfg.Server.Tor.StartupTimeout != tc.wantTimeout {
				t.Errorf("startup_timeout = %d, want %d", cfg.Server.Tor.StartupTimeout, tc.wantTimeout)
			}
			if cfg.Server.Tor.NumIntroPoints != tc.wantIntro {
				t.Errorf("num_intro_points = %d, want %d", cfg.Server.Tor.NumIntroPoints, tc.wantIntro)
			}
		})
	}
}

func TestValidateDebugConfigRangesAndByteSize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Debug.MaxBodyLogSize = "invalid"
	cfg.Server.Debug.BlockProfileRate = -1
	cfg.Server.Debug.MutexProfileFraction = -1
	validateDebugConfig(cfg)
	if cfg.Server.Debug.MaxBodyLogSize != "10KB" || cfg.Server.Debug.BlockProfileRate != 1 || cfg.Server.Debug.MutexProfileFraction != 1 {
		t.Fatalf("invalid debug settings were not reset to defaults: %+v", cfg.Server.Debug)
	}
}

func TestValidateLoggingConfigAcceptsLevelsAndResetsInvalid(t *testing.T) {
	for _, level := range []string{"", "debug", "info", "warn", "error"} {
		cfg := DefaultConfig()
		cfg.Server.Logging.Level = level
		validateLoggingConfig(cfg)
		if cfg.Server.Logging.Level != level {
			t.Errorf("level %q changed to %q", level, cfg.Server.Logging.Level)
		}
	}
	cfg := DefaultConfig()
	cfg.Server.Logging.Level = "verbose"
	validateLoggingConfig(cfg)
	if cfg.Server.Logging.Level != "" {
		t.Fatalf("invalid level fallback = %q, want empty mode default", cfg.Server.Logging.Level)
	}
	cfg = DefaultConfig()
	cfg.Server.Logging.Access.Format = "yaml"
	cfg.Server.Logging.Access.Rotate = "daily,invalid"
	cfg.Server.Logging.Audit.Format = "text"
	cfg.Server.Logging.Error.Filename = "../error.log"
	validateLoggingConfig(cfg)
	defaults := DefaultConfig().Server.Logging
	if cfg.Server.Logging.Access.Format != defaults.Access.Format || cfg.Server.Logging.Access.Rotate != "" || cfg.Server.Logging.Audit.Format != "json" || cfg.Server.Logging.Error.Filename != "" {
		t.Fatalf("invalid per-kind log settings not reset: %+v", cfg.Server.Logging)
	}
	cfg = DefaultConfig()
	cfg.Server.Logging.Access.Format = "custom"
	cfg.Server.Logging.Access.Custom = "{remote_ip} {unknown}"
	validateLoggingConfig(cfg)
	if cfg.Server.Logging.Access.Format != defaults.Access.Format {
		t.Fatalf("invalid custom log variable format = %q", cfg.Server.Logging.Access.Format)
	}
}

func TestDatabaseErrorNotificationDefaultsValidationAndSerialization(t *testing.T) {
	cfg := DefaultConfig()
	settings := cfg.Server.Notifications.DatabaseError
	if !settings.Enabled || settings.Failures != 3 || settings.CheckInterval != "1m" || settings.Repeat != "24h" {
		t.Fatalf("database_error defaults = %+v", settings)
	}
	if !cfg.Server.Notifications.Email.Events.DatabaseError {
		t.Fatal("database_error email event should default enabled")
	}
	cfg.Server.Notifications.DatabaseError.Failures = 0
	cfg.Server.Notifications.DatabaseError.CheckInterval = "0s"
	cfg.Server.Notifications.DatabaseError.Repeat = "invalid"
	validateNotificationsConfig(cfg)
	settings = cfg.Server.Notifications.DatabaseError
	if settings.Failures != 3 || settings.CheckInterval != "1m" || settings.Repeat != "24h" {
		t.Fatalf("invalid database_error settings were not reset: %+v", settings)
	}
	serialized := generateNotificationsYAML(cfg)
	for _, expected := range []string{"database_error: true", "enabled: true", "failures: 3", "check_interval: 1m", "repeat: 24h"} {
		if !strings.Contains(serialized, expected) {
			t.Errorf("generated notifications YAML missing %q", expected)
		}
	}
}

func TestValidateWebUINotificationDurations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Notifications.WebUI.Duration = -1
	cfg.Server.Notifications.WebUI.WarningDuration = -1
	validateNotificationsConfig(cfg)
	if cfg.Server.Notifications.WebUI.Duration != 3 || cfg.Server.Notifications.WebUI.WarningDuration != 5 {
		t.Fatalf("invalid WebUI toast durations were not reset: %+v", cfg.Server.Notifications.WebUI)
	}
	cfg.Server.Notifications.WebUI.Duration = 0
	cfg.Server.Notifications.WebUI.WarningDuration = 0
	validateNotificationsConfig(cfg)
	if cfg.Server.Notifications.WebUI.Duration != 0 || cfg.Server.Notifications.WebUI.WarningDuration != 0 {
		t.Fatalf("zero durations should select manual dismissal: %+v", cfg.Server.Notifications.WebUI)
	}
	serialized := generateNotificationsYAML(cfg)
	for _, expected := range []string{"duration: 0", "warning_duration: 0"} {
		if !strings.Contains(serialized, expected) {
			t.Errorf("generated notifications YAML missing %q", expected)
		}
	}
}

func TestIsConfigValueSetDistinguishesExplicitValuesFromDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  mode: development\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if !IsConfigValueSet("server.mode") {
		t.Fatal("explicit server.mode was not reported as set")
	}
	if IsConfigValueSet("server.port") {
		t.Fatal("default server.port was reported as explicitly set")
	}
}
