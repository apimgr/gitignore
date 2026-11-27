package app

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

// printHelp must not panic and must mention the binary name and key flags.
func TestPrintHelp(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	printHelp("gitignore-server")
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	for _, want := range []string{"gitignore-server", "--help", "--service", "--maintenance", "scheduler list"} {
		if !strings.Contains(out, want) {
			t.Errorf("printHelp() output missing %q", want)
		}
	}
}

// checkHealth against a port nothing is listening on must return a
// connection error, never panic or block.
func TestCheckHealthConnectionRefused(t *testing.T) {
	// Grab an ephemeral port and close it immediately so nothing is bound.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	_, port, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close()

	if err := checkHealth(port); err == nil {
		t.Error("checkHealth() against a closed port = nil error, want error")
	}
}

// handleUpdateCommand "branch" with no args must print the current branch
// and return without exiting the process.
func TestHandleUpdateCommandBranchNoArgsPrintsCurrent(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	cfg := &config.ServerConfig{}
	cfg.Server.Update.Branch = "stable"
	handleUpdateCommand("branch", nil, cfg)

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if got := buf.String(); !strings.Contains(got, "stable") {
		t.Errorf("handleUpdateCommand(branch, nil) output = %q, want it to mention the current branch", got)
	}
}

func TestHandleUpdateCommandBranchSet(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.yml")
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	handleUpdateCommand("branch", []string{"beta"}, cfg)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	if got := config.Get().Server.Update.Branch; got != "beta" {
		t.Fatalf("configured update branch = %q, want beta", got)
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatal(err)
	}
}

// runCommand must never panic regardless of whether the named binary exists.
func TestRunCommandDoesNotPanic(t *testing.T) {
	t.Run("existing binary", func(t *testing.T) {
		if _, err := exec.LookPath("true"); err != nil {
			t.Skip("true(1) not on PATH")
		}
		runCommand("true")
	})

	t.Run("missing binary", func(t *testing.T) {
		runCommand("definitely-not-a-real-binary-xyz-123")
	})
}

// reopenLogs and dumpStatus are pure logging side effects (no file handles,
// no process state mutation) — they must not panic when invoked directly.
func TestReopenLogsDoesNotPanic(t *testing.T) {
	reopenLogs()
}

func TestDumpStatusDoesNotPanic(t *testing.T) {
	dumpStatus()
}

// serviceStatus is a pure print of service.Status(); it must not panic and
// must report a well-formed status block.
func TestServiceStatusDoesNotPanic(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	serviceStatus()
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	for _, want := range []string{"Service:", "State:", "Auto-start:", "PID:"} {
		if !strings.Contains(out, want) {
			t.Errorf("serviceStatus() output missing %q, got %q", want, out)
		}
	}
}

// serviceDisable issues a real "systemctl disable gitignore" (or platform
// equivalent) via runCommand, but since the gitignore service is not
// installed on the test host it is a harmless no-op — the command simply
// fails and runCommand logs the error rather than exiting.
func TestServiceDisableDoesNotPanic(t *testing.T) {
	serviceDisable()
}

func TestResolveSecurityDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	cases := []struct {
		name, value, want string
	}{
		{"default", "", filepath.Join(dataDir, "security")},
		{"relative", "security/reports", filepath.Join(dataDir, "security", "reports")},
		{"placeholder", "${DATA_DIR}/security", filepath.Join(dataDir, "security")},
		{"legacy placeholder", "$DATA_DIR/security", filepath.Join(dataDir, "security")},
		{"braced placeholder", "{data_dir}/security", filepath.Join(dataDir, "security")},
		{"absolute", filepath.Join(dataDir, "..", "private"), filepath.Join(dataDir, "..", "private")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSecurityDataDir(tc.value, dataDir); got != filepath.Clean(tc.want) {
				t.Errorf("resolveSecurityDataDir(%q) = %q, want %q", tc.value, got, filepath.Clean(tc.want))
			}
		})
	}
}

func TestMakeBlocklistConfig(t *testing.T) {
	enabled := false
	cfg := &config.ServerConfig{}
	cfg.Server.Security.Blocklists.Enabled = true
	cfg.Server.Security.Blocklists.ExtraSources = []config.BlocklistSourceConfig{{ID: "custom", Type: "domain", URL: "https://example.test/list", Enabled: &enabled}}
	cfg.Server.TrustedProxies.Additional = []string{"192.0.2.7/24", "2001:db8::1", "invalid"}

	got := makeBlocklistConfig(cfg, "/data/security")
	if !got.Enabled || got.SecurityDir != "/data/security" || len(got.ExtraSources) != 1 || got.ExtraSources[0].ID != "custom" || got.ExtraSources[0].Enabled == nil || *got.ExtraSources[0].Enabled {
		t.Fatalf("makeBlocklistConfig() = %+v", got)
	}
	if len(got.TrustedPrefixes) != 2 || got.TrustedPrefixes[0].String() != "192.0.2.0/24" || got.TrustedPrefixes[1].String() != "2001:db8::1/128" {
		t.Fatalf("trusted prefixes = %v, want masked IPv4 and /128 IPv6", got.TrustedPrefixes)
	}
}

func TestParseDurationOr(t *testing.T) {
	fallback := 7 * time.Second
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{{" 2m ", 2 * time.Minute}, {"invalid", fallback}, {"0s", fallback}, {"-1s", fallback}} {
		if got := parseDurationOr(tc.value, fallback); got != tc.want {
			t.Errorf("parseDurationOr(%q) = %s, want %s", tc.value, got, tc.want)
		}
	}
}

func TestLatestBackupFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := latestBackupFile(dir); err == nil {
		t.Fatal("latestBackupFile on an empty directory succeeded")
	}
	old := filepath.Join(dir, "old.tar.gz")
	newer := filepath.Join(dir, "new.tar.gz.enc")
	ignored := filepath.Join(dir, "ignored.zip")
	for _, path := range []string{old, newer, ignored} {
		if err := os.WriteFile(path, []byte("backup"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ignored, base.Add(2*time.Minute), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, err := latestBackupFile(dir); err != nil || got != newer {
		t.Fatalf("latestBackupFile() = %q, %v, want %q", got, err, newer)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.tar.gz"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := latestBackupFile(dir); err == nil {
		t.Fatal("latestBackupFile accepted a directory without archives")
	}
	if _, err := latestBackupFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("latestBackupFile accepted a missing directory")
	}
}
