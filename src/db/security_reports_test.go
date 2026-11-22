package db

import (
	"testing"
	"time"
)

func TestSecurityReportMaintenanceTransitionsAndAcknowledgment(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = Close() }()
	if _, err := conn.Exec(`INSERT INTO security_reports (tracking_id, token_hash, body_enc, body_alg, severity, component, credit_pref, credit_name)
VALUES ('sec_0123456789abcdef', 'hash', X'01', 'aes-256-gcm', 'high', 'API', 'handle', 'researcher')`); err != nil {
		t.Fatalf("insert report: %v", err)
	}
	old, report, err := UpdateSecurityReport("sec_0123456789abcdef", "disclosed", "Fixed in release 1.2", true, "2026-11-22", "critical")
	if err != nil {
		t.Fatalf("disclose report: %v", err)
	}
	if old != "received" || report.Status != "disclosed" || report.StatusComment != "Fixed in release 1.2" || report.Severity != "critical" {
		t.Fatalf("UpdateSecurityReport returned old=%q report=%+v", old, report)
	}
	status, comment, date, err := ClaimSecurityReportStatus("sec_0123456789abcdef", "hash", "2026-11-22")
	if err != nil || status != "disclosed" || comment != "Fixed in release 1.2" || date != "2026-11-22" {
		t.Fatalf("ClaimSecurityReportStatus() = %q, %q, %q, %v", status, comment, date, err)
	}
	if _, _, _, err := ClaimSecurityReportStatus("sec_0123456789abcdef", "hash", "2026-11-22"); err == nil {
		t.Fatal("status token was accepted twice on the same UTC day")
	}
	entries, err := ListSecurityThanks()
	if err != nil || len(entries) != 1 || entries[0].Credit != "researcher" || entries[0].Severity != "critical" || entries[0].Date != "2026-11-22" {
		t.Fatalf("ListSecurityThanks() = %+v, %v", entries, err)
	}
	if _, _, err := UpdateSecurityReport("sec_0123456789abcdef", "triaged", "", false, "", ""); err == nil {
		t.Fatal("backward transition from closed report unexpectedly succeeded")
	}
}

func TestSecurityReportMaintenanceValidation(t *testing.T) {
	if _, _, err := UpdateSecurityReport("sec_missing", "bogus", "", false, "", ""); err == nil {
		t.Fatal("invalid status unexpectedly succeeded")
	}
	if _, _, err := UpdateSecurityReport("sec_missing", "", "", false, "2026-02-30", ""); err == nil {
		t.Fatal("invalid disclosure date unexpectedly succeeded")
	}
}

func TestCleanupClosedSecurityReportsRetainsOpenReportsAndAcknowledgments(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = Close() }()
	if _, err := conn.Exec(`INSERT INTO security_reports (tracking_id, token_hash, body_enc, severity, component, closed_at)
VALUES ('sec_expired00000001', 'hash', X'01', 'high', 'API', '2024-01-01 00:00:00')`); err != nil {
		t.Fatalf("insert expired report: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO security_reports (tracking_id, token_hash, body_enc, severity, component)
VALUES ('sec_open0000000001', 'hash', X'01', 'high', 'API')`); err != nil {
		t.Fatalf("insert open report: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO security_thanks (tracking_id, credit, severity, disclosed_on)
VALUES ('sec_expired00000001', 'Researcher', 'high', '2024-01-01')`); err != nil {
		t.Fatalf("insert acknowledgment: %v", err)
	}
	removed, err := CleanupClosedSecurityReports(time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || removed != 1 {
		t.Fatalf("CleanupClosedSecurityReports() = %d, %v; want 1, nil", removed, err)
	}
	if _, err := GetSecurityReport("sec_open0000000001"); err != nil {
		t.Fatalf("open report was deleted: %v", err)
	}
	entries, err := ListSecurityThanks()
	if err != nil || len(entries) != 1 {
		t.Fatalf("cleanup changed retained thanks entry: %+v, %v", entries, err)
	}
}
