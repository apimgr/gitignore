package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendWritesAIFormatJSONLines(t *testing.T) {
	dir := t.TempDir()
	for _, event := range []string{"config.secret_generated", "token.created"} {
		if err := Append(dir, Entry{
			Event: event, Category: "config", Severity: "info",
			Actor: Actor{Type: "system", IP: "127.0.0.1"}, Result: "success",
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	file, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
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
		seen++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("found %d JSON lines, want 2", seen)
	}
	info, err := os.Stat(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("audit.log mode = %04o, want 0640", info.Mode().Perm())
	}
}
