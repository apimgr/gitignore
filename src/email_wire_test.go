package main

import (
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/server"
)

func TestEmailNotifierGlobalsAndDisabledEmission(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Title = "GitIgnore Test"
	cfg.Server.FQDN = "gitignore.example"
	cfg.Server.SSL.Enabled = true
	cfg.Server.Notifications.Email.ReplyTo = "ops@example.test"
	n := &emailNotifier{
		cfg:     cfg,
		dataDir: t.TempDir(),
		i2pStatus: func() server.I2PInfo {
			return server.I2PInfo{Enabled: true, Running: true, Hostname: "site.b32.i2p"}
		},
	}
	values := n.globals()
	for key, want := range map[string]string{
		"app_name": "GitIgnore Test", "app_url": "https://gitignore.example",
		"fqdn": "gitignore.example", "i2p_url": "http://site.b32.i2p",
		"i2p_address": "site.b32.i2p", "notification_reply_to": "ops@example.test",
	} {
		if values[key] != want {
			t.Errorf("global %s = %q, want %q", key, values[key], want)
		}
	}
	if values["year"] == "" || values["timestamp"] == "" || values["onion_url"] != "" {
		t.Fatalf("unexpected optional globals: %+v", values)
	}
	n.emit("backup_complete", false, nil)
	var nilNotifier *emailNotifier
	nilNotifier.emit("backup_complete", true, nil)
}

func TestEmailCommandStatusAndTemplateValidation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Notifications.Email.SMTP.Host = ""
	handleEmailCommand([]string{"status"}, cfg, t.TempDir())
}

func TestNotificationHelpersAreSafeWithoutConfiguredEmail(t *testing.T) {
	old := notifier
	notifier = nil
	t.Cleanup(func() { notifier = old })
	emitBackupComplete("backup.tar.gz", "1 KiB")
	emitBackupFailed("backup.tar.gz", "disk full")
	emitDiskLow("/data", 90, 1024, 85)
	emitUpdateAvailable("1.0.0", "1.1.0")
	emitUpdateInstalled("1.0.0", "1.1.0")
	emitSchedulerError("cleanup", "Cleanup", "failed", "tomorrow")
	emitSchedulerError("backup_daily", "Backup", "failed", "tomorrow")
	if !suppressSchedulerError["backup_daily"] || suppressSchedulerError["cleanup"] {
		t.Fatal("scheduler notification suppression does not match dedicated-event policy")
	}
}
