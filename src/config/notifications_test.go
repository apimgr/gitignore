package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplySMTPEnvOverrides(t *testing.T) {
	t.Setenv("SMTP_HOST", "mail.example.com")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_TLS", "TLS")
	t.Setenv("SMTP_FROM_NAME", "Ops")
	t.Setenv("SMTP_FROM_EMAIL", "ops@example.com")

	cfg := DefaultConfig()
	ApplySMTPEnv(cfg)
	e := cfg.Server.Notifications.Email
	if e.SMTP.Host != "mail.example.com" {
		t.Errorf("host = %q", e.SMTP.Host)
	}
	if e.SMTP.Port != 465 {
		t.Errorf("port = %d", e.SMTP.Port)
	}
	if e.SMTP.Username != "user" || e.SMTP.Password != "secret" {
		t.Errorf("auth = %q/%q", e.SMTP.Username, e.SMTP.Password)
	}
	if e.SMTP.TLS != "tls" {
		t.Errorf("tls = %q, want lowercased tls", e.SMTP.TLS)
	}
	if e.From.Name != "Ops" || e.From.Email != "ops@example.com" {
		t.Errorf("from = %q/%q", e.From.Name, e.From.Email)
	}
}

func TestApplySMTPEnvInvalidPortIgnored(t *testing.T) {
	t.Setenv("SMTP_PORT", "not-a-number")
	cfg := DefaultConfig()
	ApplySMTPEnv(cfg)
	if cfg.Server.Notifications.Email.SMTP.Port != 587 {
		t.Errorf("port = %d, want default 587 preserved", cfg.Server.Notifications.Email.SMTP.Port)
	}
}

func TestDefaultNotificationsDefaults(t *testing.T) {
	n := DefaultNotificationsConfig()
	if n.WebUI.Position != "top-right" {
		t.Errorf("position = %q", n.WebUI.Position)
	}
	if n.Email.SMTP.Port != 587 || n.Email.SMTP.TLS != "auto" {
		t.Errorf("smtp defaults wrong: %d/%q", n.Email.SMTP.Port, n.Email.SMTP.TLS)
	}
	ev := n.Email.Events
	if !ev.BackupFailed || !ev.SSLExpiring || !ev.SSLRenewalFailed || !ev.SecurityAlert || !ev.SchedulerError || !ev.UpdateInstalled {
		t.Error("expected critical events enabled by default")
	}
	if ev.Startup || ev.Shutdown || ev.BackupComplete || ev.SSLRenewed || ev.UpdateAvailable {
		t.Error("expected routine events disabled by default")
	}
}

func TestNotificationsYAMLRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Notifications.Email.SMTP.Host = "smtp.example.com"
	cfg.Server.Notifications.Email.ReplyTo = "ops@example.com"
	block := generateNotificationsYAML(cfg)
	if !strings.Contains(block, "notifications:") {
		t.Fatal("block missing notifications key")
	}

	var parsed struct {
		Notifications NotificationsConfig `yaml:"notifications"`
	}
	// Dedent the two-space server-child indentation to top level for parsing.
	if err := yaml.Unmarshal([]byte(dedent(block)), &parsed); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if parsed.Notifications.Email.SMTP.Host != "smtp.example.com" {
		t.Errorf("host round-trip = %q", parsed.Notifications.Email.SMTP.Host)
	}
	if parsed.Notifications.Email.ReplyTo != "ops@example.com" {
		t.Errorf("reply_to round-trip = %q", parsed.Notifications.Email.ReplyTo)
	}
	if !parsed.Notifications.Email.Events.BackupFailed {
		t.Error("backup_failed should be true after round-trip")
	}
}

// dedent removes exactly two leading spaces from each line so the server-child
// notifications block parses as a top-level document in tests.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "  ")
	}
	return strings.Join(lines, "\n")
}
