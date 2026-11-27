package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

func TestAccessFormatsAndCustomVariables(t *testing.T) {
	entry := AccessEntry{Time: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC), RemoteIP: "127.0.0.1", Method: "GET", Path: "/items", Status: 200, Bytes: 12}
	for _, format := range []string{"apache", "nginx", "json"} {
		line, err := formatAccess(Kind{Format: format}, entry)
		if err != nil || !strings.HasSuffix(string(line), "\n") {
			t.Fatalf("%s output %q, %v", format, line, err)
		}
	}
	line := formatCustom("{remote_ip} {method} {path} {status}", entry)
	if line != "127.0.0.1 GET /items 200\n" {
		t.Fatalf("custom output = %q", line)
	}
}

func TestManagerRotatesByTimeAndRetainsArchive(t *testing.T) {
	settings, err := Resolve(config.DefaultConfig().Server.Logging, "production", false)
	if err != nil {
		t.Fatal(err)
	}
	serverLog := settings.Kinds["server"]
	serverLog.Rotate = "daily"
	serverLog.Keep = "forever"
	settings.Kinds["server"] = serverLog
	dir := t.TempDir()
	manager, err := Open(dir, settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Write("server", Entry{Level: "info", Message: "first"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "server.log")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write("server", Entry{Level: "info", Message: "second"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "server.log.") {
			return
		}
	}
	t.Fatal("time rotation did not retain an archive")
}

func TestSecurityLogUsesPrivatePermissions(t *testing.T) {
	settings, err := Resolve(config.DefaultConfig().Server.Logging, "production", false)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	manager, err := Open(directory, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()
	info, err := os.Stat(filepath.Join(directory, "security.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("security.log mode = %04o, want 0600", got)
	}
}

func TestManagerWriteAccessRotateAndReopen(t *testing.T) {
	settings, err := Resolve(config.DefaultConfig().Server.Logging, "production", false)
	if err != nil {
		t.Fatal(err)
	}
	serverKind := settings.Kinds["server"]
	serverKind.Format = "json"
	serverKind.Rotate = "daily"
	serverKind.Keep = "forever"
	settings.Kinds["server"] = serverKind
	accessKind := settings.Kinds["access"]
	accessKind.Format = "json"
	settings.Kinds["access"] = accessKind
	dir := t.TempDir()
	manager, err := Open(dir, settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Writer("server").Write([]byte("writer event")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write("server", Entry{Level: "info", Message: "structured", Fields: map[string]string{"request_id": "req-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write("missing", Entry{Level: "info", Message: "ignored"}); err != nil {
		t.Fatalf("disabled log write: %v", err)
	}
	if err := manager.Access(AccessEntry{RemoteIP: "203.0.113.10", Method: "GET", Path: "/healthz", Status: 200, Bytes: 2}); err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(dir, "server.log")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(serverPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := manager.Rotate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reopen(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write("server", Entry{Level: "warn", Message: "after reopen"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	foundArchive := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "server.log.") {
			foundArchive = true
		}
	}
	if !foundArchive {
		t.Fatal("time-based Rotate did not retain the server log archive")
	}
	access, err := os.ReadFile(filepath.Join(dir, "access.log"))
	if err != nil || !strings.Contains(string(access), `"ip":"203.0.113.10"`) {
		t.Fatalf("access log = %q, %v", access, err)
	}
}

func TestFormatEntryVariantsAndRotationHelpers(t *testing.T) {
	event := Entry{Time: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC), Level: "warn\n", Message: "line\nnext", Fields: map[string]string{"msg": "overridden", "path": "/safe"}}
	for _, format := range []string{"text", "json", "syslog", "cef", "fail2ban"} {
		line, err := formatEntry(format, event)
		if err != nil || len(line) == 0 || line[len(line)-1] != '\n' {
			t.Errorf("formatEntry(%q) = %q, %v", format, line, err)
		}
	}
	for _, value := range []string{"1B", "2KB", "3MiB", "4G", "5TB", "0M", "bad"} {
		_ = parseSize(value)
	}
	if got := cleanLogLine([]byte("a\r\nb\x00")); string(got) != "a??b?\n" {
		t.Errorf("cleanLogLine = %q", got)
	}
	if got := cefEscape(`a|b=c\d`); got != `a\|b\=c\\d` {
		t.Errorf("cefEscape = %q", got)
	}
	if shouldRotate("missing", "daily", nil, time.Now()) {
		t.Error("missing log file should not rotate")
	}
}

func TestRotateCompressesAndPrunesArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("archive me"), 0o640); err != nil {
		t.Fatal(err)
	}
	kind := Kind{Name: "server", Compress: true, Keep: "forever"}
	if err := rotateFile(path, kind); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	archive := ""
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".gz") {
			archive = filepath.Join(filepath.Dir(path), entry.Name())
		}
	}
	if archive == "" {
		t.Fatal("compressed archive was not created")
	}
	if err := pruneArchives(path, "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archive remained after zero-retention prune: %v", err)
	}
}

func TestShouldRotatePoliciesAndDiscardWriter(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(path, []byte("a"), 0o640); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"daily", "weekly", "monthly", "yearly", "1B"} {
		if !shouldRotate(path, policy, []byte("b"), now) {
			t.Errorf("shouldRotate(%q) = false", policy)
		}
	}
	if shouldRotate(path, "never", []byte("more"), now) {
		t.Error("never policy requested rotation")
	}
	if err := rotateFile(path, Kind{Name: "server", Keep: "none"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("none retention left archives behind: %#v", entries)
	}
	if n, err := (*Manager)(nil).Writer("server").Write([]byte("discard")); err != nil || n != len("discard") {
		t.Fatalf("nil manager writer = (%d, %v)", n, err)
	}
	if _, err := Open("", Settings{}); err == nil {
		t.Fatal("Open accepted an empty log directory")
	}
}
