package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendRetriesPendingDatabaseEventsBeforeNewEvents(t *testing.T) {
	appendMu.Lock()
	oldSink, oldPending := databaseSink, pendingDatabaseEntries
	databaseSink, pendingDatabaseEntries = nil, nil
	appendMu.Unlock()
	t.Cleanup(func() {
		appendMu.Lock()
		databaseSink, pendingDatabaseEntries = oldSink, oldPending
		appendMu.Unlock()
	})
	dir := t.TempDir()
	if err := Append(dir, Entry{Event: "first", Category: "system", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := SetDatabaseSink(func(Entry) error { return errors.New("database unavailable") }); err == nil {
		t.Fatal("SetDatabaseSink succeeded while draining queued event")
	}
	var stored []string
	if err := SetDatabaseSink(func(entry Entry) error {
		stored = append(stored, entry.Event)
		return nil
	}); err != nil {
		t.Fatalf("SetDatabaseSink after recovery: %v", err)
	}
	if err := Append(dir, Entry{Event: "second", Category: "system", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0] != "first" || stored[1] != "second" {
		t.Fatalf("database events = %#v, want first then second", stored)
	}
}

func TestAppendWritesAIFormatJSONLines(t *testing.T) {
	dir := t.TempDir()
	for _, event := range []string{"config.secret_generated", "token.created"} {
		if err := Append(dir, Entry{
			Event: event, Category: "config", Severity: "info",
			Actor: Actor{Type: "system"}, Result: "success",
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	file, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close audit log: %v", err)
		}
	})
	scanner := bufio.NewScanner(file)
	seen := 0
	ids := map[string]bool{}
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decode JSON line: %v", err)
		}
		if !strings.HasPrefix(entry.ID, "audit_") || len(entry.ID) != len("audit_")+26 {
			t.Errorf("invalid audit ULID %q", entry.ID)
		}
		if ids[entry.ID] {
			t.Errorf("duplicate audit ID %q", entry.ID)
		}
		ids[entry.ID] = true
		stamp, err := time.Parse("2006-01-02T15:04:05.000Z", entry.Time)
		if err != nil || stamp.Location() != time.UTC {
			t.Errorf("invalid millisecond UTC timestamp %q: %v", entry.Time, err)
		}
		if entry.Event == "" || entry.Category == "" || entry.Severity == "" || entry.Actor.Type == "" || entry.Result == "" {
			t.Errorf("required audit field missing: %+v", entry)
		}
		if entry.Actor.ID != "" || entry.Actor.IP != "" {
			t.Errorf("autonomous event has actor ID or IP: %+v", entry.Actor)
		}
		seen++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("found %d JSON lines, want 2", seen)
	}
	if count, err := Verify(dir); err != nil || count != 2 {
		t.Fatalf("Verify = %d, %v; want 2 entries", count, err)
	}
	info, err := os.Stat(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("audit.log mode = %04o, want 0640", info.Mode().Perm())
	}
}

func TestVerifyDetectsTamperedEntryAndReportsOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Entry{Event: "first", Category: "system", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, Entry{Event: "second", Category: "system", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "audit.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"event":"first"`, `"event":"changed"`, 1))
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAndReport(dir); err == nil {
		t.Fatal("tampered log verified")
	}
	if _, err := VerifyAndReport(dir); err == nil {
		t.Fatal("tampered log verified on second check")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close audit log: %v", err)
		}
	})
	scanner := bufio.NewScanner(file)
	brokenEvents := 0
	for scanner.Scan() {
		var item Entry
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil && item.Event == "security.audit_chain_broken" {
			brokenEvents++
		}
	}
	if brokenEvents != 1 {
		t.Fatalf("integrity events = %d, want 1", brokenEvents)
	}
}

func TestRotateAnchorsAndAppliesRetention(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Entry{Event: "before", Category: "system", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(dir, "none", false); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "audit.anchor")); err != nil {
		t.Fatalf("anchor missing: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "audit.log.") {
			t.Fatalf("keep none retained %q", entry.Name())
		}
	}
	if err := Append(dir, Entry{Event: "after", Category: "system", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	if count, err := Verify(dir); err != nil || count != 1 {
		t.Fatalf("Verify after rotation = %d, %v", count, err)
	}
}

func TestCountEventsSinceIncludesCompressedRotatedLogs(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Entry{Event: "compliance.consent_updated", Category: "compliance", Severity: "info", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(dir, "forever", true); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	counts, err := CountEventsSince(dir, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("CountEventsSince: %v", err)
	}
	if counts["compliance.consent_updated"] != 1 {
		t.Fatalf("counts = %#v, want one rotated consent event", counts)
	}
}

func TestAuditRetentionSupportsAllConfiguredForms(t *testing.T) {
	cases := []struct {
		value    string
		duration time.Duration
		files    int
		retain   bool
	}{
		{"none", 0, 0, false},
		{"7", 0, 7, true},
		{"30d", 30 * 24 * time.Hour, 0, true},
		{"2w", 14 * 24 * time.Hour, 0, true},
		{"3m", 90 * 24 * time.Hour, 0, true},
		{"forever", 0, 0, true},
		{"keep:weekly:4", 0, 4, true},
	}
	for _, tc := range cases {
		duration, files, retain := auditRetention(tc.value)
		if duration != tc.duration || files != tc.files || retain != tc.retain {
			t.Errorf("auditRetention(%q) = (%v, %d, %v), want (%v, %d, %v)", tc.value, duration, files, retain, tc.duration, tc.files, tc.retain)
		}
	}
}
