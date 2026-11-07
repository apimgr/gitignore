package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

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
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
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
	l.Close()

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

	cfg := &config.Config{}
	cfg.Server.Update.Branch = "stable"
	handleUpdateCommand("branch", nil, cfg)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	if got := buf.String(); !strings.Contains(got, "stable") {
		t.Errorf("handleUpdateCommand(branch, nil) output = %q, want it to mention the current branch", got)
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
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
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
