package db

import (
	"testing"

	"github.com/apimgr/gitignore/src/common/audit"
)

func TestAppendAuditEntryStoresStructuredEvent(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	entry := audit.Entry{
		Time:     "2026-06-20T12:30:45.000Z",
		Event:    "security.token_revoked",
		Category: "security",
		Severity: "info",
		Actor:    audit.Actor{Type: "operator", ID: "operator", IP: "192.0.2.1"},
		Target:   &audit.Target{Type: "token", ID: "own_1234"},
		Details:  map[string]any{"reason": "rotation"},
		Result:   "success",
	}
	if err := AppendAuditEntry(entry); err != nil {
		t.Fatal(err)
	}
	var timestamp int64
	var level, category, action, actorIP, targetType, targetID, details string
	var success int
	err := readConn.QueryRow(`SELECT timestamp, level, category, action, actor_ip, target_type, target_id, details, success FROM audit_log WHERE action = ?`, entry.Event).Scan(&timestamp, &level, &category, &action, &actorIP, &targetType, &targetID, &details, &success)
	if err != nil {
		t.Fatal(err)
	}
	if timestamp != 1781958645 || level != "security" || category != "security" || action != entry.Event || actorIP != "192.0.2.1" || targetType != "token" || targetID != "own_1234" || details != `{"reason":"rotation"}` || success != 1 {
		t.Fatalf("stored audit event mismatch: %d %s %s %s %s %s %s %s %d", timestamp, level, category, action, actorIP, targetType, targetID, details, success)
	}
}
