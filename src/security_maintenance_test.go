package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

func TestSecurityMaintenanceValidators(t *testing.T) {
	if !validSecurityTrackingID("sec_0123456789abcdef") {
		t.Fatal("valid tracking ID was rejected")
	}
	for _, value := range []string{"", "sec_short", "sec_0123456789abcdeg", "SEC_0123456789abcdef"} {
		if validSecurityTrackingID(value) {
			t.Errorf("invalid tracking ID %q was accepted", value)
		}
	}
	for _, value := range []string{"received", "triaged", "confirmed", "patching", "disclosed", "wont_fix"} {
		if !validMaintenanceSecurityStatus(value) {
			t.Errorf("valid status %q was rejected", value)
		}
	}
	for _, value := range []string{"critical", "high", "medium", "low", "informational"} {
		if !validMaintenanceSecuritySeverity(value) {
			t.Errorf("valid severity %q was rejected", value)
		}
	}
	if validMaintenanceSecurityStatus("closed") || validMaintenanceSecuritySeverity("urgent") {
		t.Fatal("unsupported status or severity was accepted")
	}
}

func TestSecurityMaintenanceReportListingAndUpdate(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	trackingID := "sec_0123456789abcdef"
	if err := db.SaveSecurityReport(trackingID, "security-id", strings.Repeat("a", 64), []byte("encrypted"), "aes-256-gcm", []byte("contact"), "high", "server", "no", "", false); err != nil {
		t.Fatal(err)
	}
	if err := listSecurityReports(nil); err != nil {
		t.Fatal(err)
	}
	if err := listSecurityReports([]string{"--status", "invalid"}); err == nil {
		t.Fatal("invalid status filter was accepted")
	}
	logsDir := t.TempDir()
	if err := updateSecurityReport(config.DefaultConfig(), logsDir, []string{trackingID, "--status", "triaged", "--comment", "Investigating"}); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(filepath.Join(logsDir, "security.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), `old_status="received"`) || !strings.Contains(string(logData), `new_status="triaged"`) {
		t.Fatalf("unexpected security event log: %s", logData)
	}
	if err := updateSecurityReport(config.DefaultConfig(), logsDir, []string{trackingID, "--unknown", "value"}); err == nil {
		t.Fatal("unknown report update option was accepted")
	}
}
