package logging

import (
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestResolveUsesPerKindDefaults(t *testing.T) {
	settings, err := Resolve(config.DefaultConfig().Server.Logging, "production", false)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Level != "info" {
		t.Fatalf("level = %q", settings.Level)
	}
	if settings.Kinds["access"].Rotate != "monthly" || settings.Kinds["audit"].Rotate != "daily" {
		t.Fatalf("special rotation defaults = access:%q audit:%q", settings.Kinds["access"].Rotate, settings.Kinds["audit"].Rotate)
	}
	if settings.Kinds["server"].Rotate != "weekly,50MB" || settings.Kinds["debug"].Enabled {
		t.Fatalf("inherited defaults = server:%q debug-enabled:%t", settings.Kinds["server"].Rotate, settings.Kinds["debug"].Enabled)
	}
}

func TestResolveRejectsUnsafeFilenameAndUnsupportedFormat(t *testing.T) {
	cfg := config.DefaultConfig().Server.Logging
	cfg.Error.Filename = "../outside.log"
	if _, err := Resolve(cfg, "production", false); err == nil || !strings.Contains(err.Error(), "filename") {
		t.Fatalf("unsafe filename error = %v", err)
	}
	cfg = config.DefaultConfig().Server.Logging
	cfg.Audit.Format = "text"
	if _, err := Resolve(cfg, "production", false); err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("unsupported audit format error = %v", err)
	}
}

func TestResolveValidatesEffectiveSettings(t *testing.T) {
	cases := []struct {
		name string
		edit func(*config.LoggingConfig)
		want string
	}{
		{"level", func(cfg *config.LoggingConfig) { cfg.Level = "trace" }, "log level"},
		{"rotation", func(cfg *config.LoggingConfig) { cfg.Defaults.Rotate = "everyday" }, "rotation"},
		{"retention", func(cfg *config.LoggingConfig) { cfg.Defaults.Keep = "forever:invalid" }, "retention"},
		{"custom", func(cfg *config.LoggingConfig) { cfg.Access.Format = "custom"; cfg.Access.Custom = "{secret}" }, "custom format"},
		{"separator in filename", func(cfg *config.LoggingConfig) { cfg.Server.Filename = "nested/server.log" }, "filename"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig().Server.Logging
			tc.edit(&cfg)
			if _, err := Resolve(cfg, "production", false); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Resolve error = %v, want %q", err, tc.want)
			}
		})
	}
	if settings, err := Resolve(config.LoggingConfig{}, "development", false); err != nil || settings.Level != "debug" {
		t.Fatalf("development defaults = (%+v, %v)", settings, err)
	}
	if settings, err := Resolve(config.LoggingConfig{}, "production", true); err != nil || settings.Level != "debug" {
		t.Fatalf("debug-mode defaults = (%+v, %v)", settings, err)
	}
}

func TestLoggingFormatAndRetentionValidators(t *testing.T) {
	for _, value := range []string{"{remote_ip} {method} {path}", "plain text", "{status}:{bytes}"} {
		if !validCustom(value) {
			t.Errorf("validCustom(%q) = false", value)
		}
	}
	for _, value := range []string{"", "{unknown}", "{method", "method}"} {
		if validCustom(value) {
			t.Errorf("validCustom(%q) = true", value)
		}
	}
	for _, value := range []string{"daily", "weekly,50MB", "never", "1GiB"} {
		if !validRotation(value) {
			t.Errorf("validRotation(%q) = false", value)
		}
	}
	for _, value := range []string{"", "weekly,,daily", "nope"} {
		if validRotation(value) {
			t.Errorf("validRotation(%q) = true", value)
		}
	}
	for _, value := range []string{"none", "forever", "7d:4w:2m", "daily:weekly:10"} {
		if !validRetention(value) {
			t.Errorf("validRetention(%q) = false", value)
		}
	}
	for _, value := range []string{"", "3x", "daily::weekly"} {
		if validRetention(value) {
			t.Errorf("validRetention(%q) = true", value)
		}
	}
	for kind, formats := range map[string][]string{
		"access":   {"apache", "nginx", "json", "custom"},
		"server":   {"text", "json"},
		"audit":    {"json"},
		"security": {"fail2ban", "syslog", "cef", "json", "text"},
	} {
		for _, format := range formats {
			if !validFormat(kind, format) {
				t.Errorf("validFormat(%q, %q) = false", kind, format)
			}
		}
	}
	if validFormat("unknown", "text") || validFormat("audit", "text") {
		t.Fatal("unsupported format was accepted")
	}
}
