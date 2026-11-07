package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/scheduler"
)

// resolveColor: --color=yes/no always win; otherwise NO_COLOR env wins over
// TTY autodetection.
func TestResolveColor(t *testing.T) {
	t.Run("yes forces color regardless of NO_COLOR", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if !resolveColor("yes") {
			t.Error("resolveColor(\"yes\") = false, want true")
		}
	})

	t.Run("no disables color regardless of NO_COLOR", func(t *testing.T) {
		os.Unsetenv("NO_COLOR")
		if resolveColor("no") {
			t.Error("resolveColor(\"no\") = true, want false")
		}
	})

	t.Run("auto honors NO_COLOR", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if resolveColor("auto") {
			t.Error("resolveColor(\"auto\") = true with NO_COLOR set, want false")
		}
	})

	t.Run("case and whitespace insensitive", func(t *testing.T) {
		os.Unsetenv("NO_COLOR")
		if !resolveColor("  YES ") {
			t.Error("resolveColor(\"  YES \") = false, want true")
		}
	})
}

func TestEnsureRuntimeDir(t *testing.T) {
	t.Run("empty path is a no-op", func(t *testing.T) {
		if err := ensureRuntimeDir(""); err != nil {
			t.Errorf("ensureRuntimeDir(\"\") = %v, want nil", err)
		}
	})

	t.Run("creates the directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "runtime")
		if err := ensureRuntimeDir(dir); err != nil {
			t.Fatalf("ensureRuntimeDir: %v", err)
		}
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !fi.IsDir() {
			t.Error("expected a directory to be created")
		}
	})
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
		{"/", "/"},
		{"api", "/api"},
		{"/api", "/api"},
		{"/api/", "/api"},
		{"  /api  ", "/api"},
		{"//", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeBaseURL(tc.in); got != tc.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"production", "production"},
		{"prod", "production"},
		{"PROD", "production"},
		{"development", "development"},
		{"dev", "development"},
		{"  dev  ", "development"},
		{"bogus", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := resolveMode(tc.in); got != tc.want {
				t.Errorf("resolveMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// shellcomplete.go pure helpers.

func TestDetectShell(t *testing.T) {
	t.Run("defaults to bash when unset", func(t *testing.T) {
		os.Unsetenv("SHELL")
		if got := detectShell(); got != "bash" {
			t.Errorf("detectShell() = %q, want bash", got)
		}
	})

	t.Run("extracts basename from SHELL", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/bin/zsh")
		if got := detectShell(); got != "zsh" {
			t.Errorf("detectShell() = %q, want zsh", got)
		}
	})
}

func TestJoinServerWords(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a b c"},
	}
	for _, tc := range cases {
		if got := joinServerWords(tc.in); got != tc.want {
			t.Errorf("joinServerWords(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGenerateServerCompletionsContainFlags(t *testing.T) {
	const bin = "gitignore-server"

	t.Run("bash", func(t *testing.T) {
		out := generateServerBashCompletions(bin)
		if !strings.Contains(out, "_gitignore-server_completions") || !strings.Contains(out, "--port") {
			t.Errorf("unexpected bash completions: %s", out)
		}
	})

	t.Run("zsh", func(t *testing.T) {
		out := generateServerZshCompletions(bin)
		if !strings.Contains(out, "#compdef "+bin) || !strings.Contains(out, "--port") {
			t.Errorf("unexpected zsh completions: %s", out)
		}
	})

	t.Run("fish", func(t *testing.T) {
		out := generateServerFishCompletions(bin)
		if !strings.Contains(out, "complete -c "+bin+" -l port") {
			t.Errorf("unexpected fish completions: %s", out)
		}
	})

	t.Run("posix", func(t *testing.T) {
		out := generateServerPosixCompletions(bin)
		if !strings.Contains(out, "--port") {
			t.Errorf("unexpected posix completions: %s", out)
		}
	})

	t.Run("powershell", func(t *testing.T) {
		out := generateServerPowershellCompletions(bin)
		if !strings.Contains(out, "Register-ArgumentCompleter") || !strings.Contains(out, "--port") {
			t.Errorf("unexpected powershell completions: %s", out)
		}
	})
}

func TestPrintShellCompletionsUnsupportedShell(t *testing.T) {
	if code := printShellCompletions("cobol", "gitignore"); code != 64 {
		t.Errorf("printShellCompletions unsupported shell = %d, want 64", code)
	}
}

func TestPrintShellInitUnsupportedShell(t *testing.T) {
	if code := printShellInit("cobol", "gitignore"); code != 64 {
		t.Errorf("printShellInit unsupported shell = %d, want 64", code)
	}
}

func TestHandleShellNoArgs(t *testing.T) {
	if code := handleShell(nil); code != 64 {
		t.Errorf("handleShell(nil) = %d, want 64", code)
	}
}

func TestHandleShellHelp(t *testing.T) {
	if code := handleShell([]string{"help"}); code != 0 {
		t.Errorf("handleShell([\"help\"]) = %d, want 0", code)
	}
}

func TestHandleShellUnknownSubcommand(t *testing.T) {
	if code := handleShell([]string{"bogus"}); code != 64 {
		t.Errorf("handleShell([\"bogus\"]) = %d, want 64", code)
	}
}

// scheduler_wire.go pure helpers.

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
	}
	for _, tc := range cases {
		if got := humanizeBytes(tc.in); got != tc.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBackupFileSize(t *testing.T) {
	t.Run("missing file returns unknown", func(t *testing.T) {
		if got := backupFileSize(filepath.Join(t.TempDir(), "missing")); got != "unknown" {
			t.Errorf("backupFileSize(missing) = %q, want unknown", got)
		}
	})

	t.Run("existing file returns humanized size", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "file.bin")
		if err := os.WriteFile(p, make([]byte, 2048), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if got := backupFileSize(p); got != "2.0 KiB" {
			t.Errorf("backupFileSize = %q, want 2.0 KiB", got)
		}
	})
}

func TestFmtTime(t *testing.T) {
	if got := fmtTime(time.Time{}); got != "-" {
		t.Errorf("fmtTime(zero) = %q, want -", got)
	}
	tm := time.Date(2026, 1, 2, 3, 4, 0, 0, time.Local)
	if got := fmtTime(tm); got != "2026-01-02 03:04" {
		t.Errorf("fmtTime = %q, want 2026-01-02 03:04", got)
	}
}

func TestNonEmpty(t *testing.T) {
	if got := nonEmpty("", "fallback"); got != "fallback" {
		t.Errorf("nonEmpty(\"\", fallback) = %q, want fallback", got)
	}
	if got := nonEmpty("value", "fallback"); got != "value" {
		t.Errorf("nonEmpty(value, fallback) = %q, want value", got)
	}
}

func TestSchedulerStatusGlyph(t *testing.T) {
	cases := []struct {
		name string
		info scheduler.TaskInfo
		want string
	}{
		{"running wins over status", scheduler.TaskInfo{Running: true, LastStatus: scheduler.StatusFailed}, "●"},
		{"success", scheduler.TaskInfo{LastStatus: scheduler.StatusSuccess}, "✓"},
		{"failed", scheduler.TaskInfo{LastStatus: scheduler.StatusFailed}, "✗"},
		{"skipped", scheduler.TaskInfo{LastStatus: scheduler.StatusSkipped}, "◐"},
		{"pending/unknown", scheduler.TaskInfo{}, "○"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := schedulerStatusGlyph(tc.info); got != tc.want {
				t.Errorf("schedulerStatusGlyph() = %q, want %q", got, tc.want)
			}
		})
	}
}

// pidfile.go / pidfile_unix.go

func TestWritePIDFileEmptyPathIsNoop(t *testing.T) {
	if err := writePIDFile(""); err != nil {
		t.Errorf("writePIDFile(\"\") = %v, want nil", err)
	}
}

func TestRemovePIDFileEmptyPathIsNoop(t *testing.T) {
	removePIDFile("")
}

func TestRemovePIDFileOwnedByAnotherProcessIsKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.pid")
	otherPID := os.Getpid() + 1
	if err := os.WriteFile(p, []byte(strconv.Itoa(otherPID)), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	removePIDFile(p)
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected PID file owned by another process to survive removePIDFile, got: %v", err)
	}
}

func TestRemovePIDFileOwnedBySelfIsRemoved(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.pid")
	if err := os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	removePIDFile(p)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("expected PID file owned by self to be removed, stat err = %v", err)
	}
}

func TestRemovePIDFileGarbageContentsIsRemoved(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.pid")
	if err := os.WriteFile(p, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	removePIDFile(p)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("expected PID file with unparsable contents to be removed, stat err = %v", err)
	}
}

func TestPidFilePerm(t *testing.T) {
	perm := pidFilePerm()
	if os.Geteuid() == 0 {
		if perm != 0644 {
			t.Errorf("pidFilePerm() as root = %v, want 0644", perm)
		}
	} else if perm != 0600 {
		t.Errorf("pidFilePerm() as non-root = %v, want 0600", perm)
	}
}

func TestPidProcessAliveForSelf(t *testing.T) {
	if !pidProcessAlive(os.Getpid()) {
		t.Error("pidProcessAlive(self) = false, want true")
	}
}

func TestPidProcessAliveForBogusPID(t *testing.T) {
	// An extremely large PID is exceedingly unlikely to be assigned.
	if pidProcessAlive(1 << 30) {
		t.Error("pidProcessAlive(huge bogus pid) = true, want false")
	}
}
