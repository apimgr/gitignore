package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

// signal_unix.go / signal_linux.go: registering a channel for OS signal
// delivery has no destructive effect and is safe to exercise directly.
func TestNotifyShutdownSignalsDoesNotPanic(t *testing.T) {
	ch := make(chan os.Signal, 1)
	notifyShutdownSignals(ch)
}

// email_wire.go pure helpers.

func TestTemplateDir(t *testing.T) {
	if got, want := templateDir("/cfg"), filepath.Join("/cfg", "template", "email"); got != want {
		t.Errorf("templateDir(/cfg) = %q, want %q", got, want)
	}
}

func TestBuildSender(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Server.Branding.Title = "MyApp"
		s := buildSender(cfg)
		if s.FromName != "MyApp" {
			t.Errorf("FromName = %q, want MyApp", s.FromName)
		}
		if s.FromEmail != "no-reply@localhost" {
			t.Errorf("FromEmail = %q, want no-reply@localhost", s.FromEmail)
		}
	})

	t.Run("uses configured from name/email and fqdn", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Server.FQDN = "example.com"
		cfg.Server.Notifications.Email.From.Name = "Ops"
		cfg.Server.Notifications.Email.From.Email = "ops@example.com"
		s := buildSender(cfg)
		if s.FromName != "Ops" {
			t.Errorf("FromName = %q, want Ops", s.FromName)
		}
		if s.FromEmail != "ops@example.com" {
			t.Errorf("FromEmail = %q, want ops@example.com", s.FromEmail)
		}
	})

	t.Run("derives from address from fqdn when unset", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Server.FQDN = "example.com"
		s := buildSender(cfg)
		if s.FromEmail != "no-reply@example.com" {
			t.Errorf("FromEmail = %q, want no-reply@example.com", s.FromEmail)
		}
	})
}

func TestNotifyRecipient(t *testing.T) {
	t.Run("prefers admin email", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Server.Admin.Email = "admin@example.com"
		cfg.Server.Notifications.Email.ReplyTo = "reply@example.com"
		sender := buildSender(cfg)
		if got := notifyRecipient(cfg, sender); got != "admin@example.com" {
			t.Errorf("notifyRecipient = %q, want admin@example.com", got)
		}
	})

	t.Run("falls back to reply-to", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Server.Notifications.Email.ReplyTo = "reply@example.com"
		sender := buildSender(cfg)
		if got := notifyRecipient(cfg, sender); got != "reply@example.com" {
			t.Errorf("notifyRecipient = %q, want reply@example.com", got)
		}
	})

	t.Run("falls back to sender from address", func(t *testing.T) {
		cfg := config.DefaultConfig()
		sender := buildSender(cfg)
		if got := notifyRecipient(cfg, sender); got != sender.FromEmail {
			t.Errorf("notifyRecipient = %q, want %q", got, sender.FromEmail)
		}
	})
}

func TestDetectGatewayIPAndGlobalIPv4DoNotPanic(t *testing.T) {
	// Host/network-dependent; only assert these never panic and return a string.
	_ = detectGatewayIP()
	_ = detectGlobalIPv4()
}

// update_wire.go pure helpers.

func TestUpdateNotifyStatePath(t *testing.T) {
	if got, want := updateNotifyStatePath("/data"), filepath.Join("/data", "update_last_notified"); got != want {
		t.Errorf("updateNotifyStatePath(/data) = %q, want %q", got, want)
	}
}

func TestLastNotifiedUpdate(t *testing.T) {
	t.Run("missing file returns empty", func(t *testing.T) {
		if got := lastNotifiedUpdate(t.TempDir()); got != "" {
			t.Errorf("lastNotifiedUpdate(missing) = %q, want empty", got)
		}
	})

	t.Run("returns trimmed recorded version", func(t *testing.T) {
		dir := t.TempDir()
		if err := recordNotifiedUpdate(dir, "v1.2.3"); err != nil {
			t.Fatalf("recordNotifiedUpdate: %v", err)
		}
		if got := lastNotifiedUpdate(dir); got != "v1.2.3" {
			t.Errorf("lastNotifiedUpdate = %q, want v1.2.3", got)
		}
	})
}

func TestRecordNotifiedUpdate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	if err := recordNotifiedUpdate(dir, "v9.9.9"); err != nil {
		t.Fatalf("recordNotifiedUpdate: %v", err)
	}
	data, err := os.ReadFile(updateNotifyStatePath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "v9.9.9\n" {
		t.Errorf("recorded contents = %q, want %q", data, "v9.9.9\n")
	}
}

func TestNewUpdater(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Update.Branch = "beta"
	u := newUpdater(cfg)
	if u == nil {
		t.Fatal("newUpdater returned nil")
	}
}

// geoip_wire.go pure helpers.

func TestNewGeoIP(t *testing.T) {
	cfg := config.DefaultConfig()
	gm := newGeoIP(cfg, t.TempDir())
	if gm == nil {
		t.Fatal("newGeoIP returned nil")
	}
}

// backup.go: promptPassword requires an interactive terminal; under `go test`
// stdin is never a tty, so this deterministically exercises the error path.
func TestPromptPasswordNonTTY(t *testing.T) {
	if _, err := promptPassword("password: "); err == nil {
		t.Error("promptPassword() with non-tty stdin = nil error, want error")
	}
}
