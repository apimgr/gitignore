package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// okMark/warnMark: NO_COLOR convention toggles emoji vs plain-text markers.
func TestOkMarkHonorsNoColor(t *testing.T) {
	t.Run("empty NO_COLOR keeps emoji", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		if got := okMark(); got != "✅" {
			t.Errorf("okMark() = %q, want emoji", got)
		}
	})
	t.Run("NO_COLOR set returns plain text", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if got := okMark(); got != "[OK]" {
			t.Errorf("okMark() = %q, want [OK]", got)
		}
	})

	t.Run("NO_COLOR unset returns emoji", func(t *testing.T) {
		if err := os.Unsetenv("NO_COLOR"); err != nil {
			t.Fatalf("Unsetenv(NO_COLOR): %v", err)
		}
		if got := okMark(); got != "✅" {
			t.Errorf("okMark() = %q, want emoji", got)
		}
	})
}

func TestWarnMarkHonorsNoColor(t *testing.T) {
	t.Run("empty NO_COLOR keeps emoji", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		if got := warnMark(); got != "⚠️ " {
			t.Errorf("warnMark() = %q, want emoji", got)
		}
	})
	t.Run("NO_COLOR set returns plain text", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if got := warnMark(); got != "[WARN]" {
			t.Errorf("warnMark() = %q, want [WARN]", got)
		}
	})

	t.Run("NO_COLOR unset returns emoji", func(t *testing.T) {
		if err := os.Unsetenv("NO_COLOR"); err != nil {
			t.Fatalf("Unsetenv(NO_COLOR): %v", err)
		}
		if got := warnMark(); got != "⚠️ " {
			t.Errorf("warnMark() = %q, want emoji", got)
		}
	})
}

// hasExecutable must find something on PATH that definitely exists, and
// must not find something that definitely doesn't.
func TestHasExecutable(t *testing.T) {
	if !hasExecutable("sh") {
		t.Error(`hasExecutable("sh") = false, want true (sh must be on PATH)`)
	}
	if hasExecutable("definitely-not-a-real-binary-xyz-123") {
		t.Error("hasExecutable() = true for a nonexistent binary, want false")
	}
}

// DetectServiceManager must always return one of the known ServiceType
// constants for the current GOOS, never panic, never a bogus value.
func TestDetectServiceManagerReturnsKnownType(t *testing.T) {
	got := DetectServiceManager()
	switch got {
	case ServiceUnknown, ServiceSystemd, ServiceRunit, ServiceOpenRC,
		ServiceS6, ServiceSysVinit, ServiceLaunchd, ServiceWindows, ServiceBSDRC:
		// ok
	default:
		t.Errorf("DetectServiceManager() = %v, not a known ServiceType", got)
	}
}

func TestIsS6ManagerRequiresExecutableAndRunningScanner(t *testing.T) {
	for _, tc := range []struct {
		name       string
		executable bool
		running    bool
		want       bool
	}{
		{name: "both available", executable: true, running: true, want: true},
		{name: "missing executable", running: true},
		{name: "scanner not running", executable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isS6Manager(tc.executable, tc.running); got != tc.want {
				t.Errorf("isS6Manager(%v, %v) = %v, want %v", tc.executable, tc.running, got, tc.want)
			}
		})
	}
}

func TestS6ScanDirectoryFromCmdline(t *testing.T) {
	if got, want := s6ScanDirectoryFromCmdline([]byte("s6-svscan\x00/run/s6/services\x00")), "/run/s6/services"; got != want {
		t.Errorf("s6 scan directory = %q, want %q", got, want)
	}
	if got := s6ScanDirectoryFromCmdline([]byte("s6-svscan\x00")); got != "" {
		t.Errorf("s6 scan directory without an argument = %q, want empty for fallback", got)
	}
}

func TestSystemdServiceContentUsesRequiredProtectSystem(t *testing.T) {
	content := systemdServiceContent("/usr/local/bin/gitignore")
	if !strings.Contains(content, "ProtectSystem=true") {
		t.Fatal("systemd service content omits ProtectSystem=true")
	}
	if strings.Contains(content, "ProtectSystem=strict") {
		t.Error("systemd service content uses forbidden ProtectSystem=strict")
	}
	for _, required := range []string{
		"ExecStartPre=+/usr/bin/install -d -m 0755",
		"ReadWritePaths=-/etc/apimgr/gitignore",
		"ReadWritePaths=-/var/lib/apimgr/gitignore",
		"ReadWritePaths=-/var/cache/apimgr/gitignore",
		"ReadWritePaths=-/var/log/apimgr/gitignore",
		"ReadWritePaths=-/var/run/apimgr/gitignore",
		"ReadWritePaths=-/mnt/Backups/apimgr/gitignore",
		"ReadWritePaths=-/usr/local/bin",
		"StandardOutput=journal",
		"StandardError=journal",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("systemd service content missing %q", required)
		}
	}
	if strings.Contains(content, "\nUser=") || strings.Contains(content, "\nGroup=") {
		t.Error("systemd service must start elevated so the server can bind before dropping privileges")
	}
}

func TestGetBinaryPath(t *testing.T) {
	got := GetBinaryPath()
	if got == "" {
		t.Fatal("GetBinaryPath() returned empty string")
	}
	if runtime.GOOS == "windows" {
		if got != `C:\Program Files\apimgr\gitignore\gitignore.exe` {
			t.Errorf("GetBinaryPath() = %q on windows", got)
		}
	} else {
		if got != "/usr/local/bin/gitignore" {
			t.Errorf("GetBinaryPath() = %q on %s", got, runtime.GOOS)
		}
	}
}

// IsElevated must agree with the underlying platform check: on Unix that's
// simply euid == 0.
func TestIsElevatedMatchesEuidOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("euid check not applicable on windows")
	}
	want := os.Geteuid() == 0
	if got := IsElevated(); got != want {
		t.Errorf("IsElevated() = %v, want %v (euid=%d)", got, want, os.Geteuid())
	}
}

// DetectEscalation must never panic and, when already elevated, must
// report EscalateNone regardless of what escalation tools are installed.
func TestDetectEscalationWhenElevated(t *testing.T) {
	if !IsElevated() {
		t.Skip("test process is not elevated")
	}
	if got := DetectEscalation(); got != EscalateNone {
		t.Errorf("DetectEscalation() = %q, want EscalateNone when already elevated", got)
	}
}

func TestDetectEscalationReturnsKnownMethod(t *testing.T) {
	got := DetectEscalation()
	switch got {
	case EscalateNone, EscalateSudo, EscalateSu, EscalatePkexec, EscalateDoas, EscalateRunas:
		// ok
	default:
		t.Errorf("DetectEscalation() = %q, not a known EscalationMethod", got)
	}
}

// canSudoInteractively must never panic regardless of group membership
// and must return a plain bool (call is enough to exercise both the
// user.Current and GroupIds/LookupGroupId error paths on whatever host
// runs the tests).
func TestCanSudoInteractivelyDoesNotPanic(t *testing.T) {
	_ = canSudoInteractively()
}

func TestExecElevatedNoArgsReturnsError(t *testing.T) {
	if err := ExecElevated(EscalateSudo, nil); err == nil {
		t.Error("ExecElevated(_, nil) = nil error, want error for empty args")
	}
	if err := ExecElevated(EscalateSudo, []string{}); err == nil {
		t.Error("ExecElevated(_, []) = nil error, want error for empty args")
	}
}

func TestExecElevatedUnknownMethodReturnsError(t *testing.T) {
	err := ExecElevated(EscalationMethod("bogus"), []string{"echo", "hi"})
	if err == nil {
		t.Error("ExecElevated with unknown method = nil error, want error")
	}
}

func TestShellJoin(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"empty", nil, ""},
		{"single simple arg", []string{"echo"}, "'echo'"},
		{"multiple args", []string{"echo", "hi"}, "'echo' 'hi'"},
		{"arg with single quote", []string{"it's"}, `'it'\''s'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shellJoin(tc.args); got != tc.want {
				t.Errorf("shellJoin(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestWindowsCommandLineQuotesArguments(t *testing.T) {
	got := windowsCommandLine([]string{`C:\Program Files\gitignore.exe`, `--value=a b`, `quote"value`, `trail\\`})
	want := `"C:\Program Files\gitignore.exe" "--value=a b" "quote\"value" "trail\\\\"`
	if got != want {
		t.Fatalf("windowsCommandLine() = %q, want %q", got, want)
	}
}

func TestRunAsWindowsServiceIsNoopOutsideWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SCM detection is Windows-only")
	}
	isService, detectErr := IsWindowsService()
	if detectErr != nil || isService {
		t.Fatalf("IsWindowsService() = (%v, %v), want (false, nil)", isService, detectErr)
	}
	called := false
	handled, err := RunAsWindowsService(func(context.Context, func()) error {
		called = true
		return nil
	})
	if err != nil || handled || called {
		t.Fatalf("RunAsWindowsService() = (%v, %v), callback called=%v; want (false, nil), not called", handled, err, called)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(present, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	missing := filepath.Join(dir, "missing.txt")

	if !fileExists(present) {
		t.Errorf("fileExists(%q) = false, want true", present)
	}
	if fileExists(missing) {
		t.Errorf("fileExists(%q) = true, want false", missing)
	}
}

// Status must never panic regardless of what service manager (if any) is
// detected on the host, and must report the "gitignore" service as not
// installed on a fresh build host where it was never actually installed.
func TestStatusOnUninstalledService(t *testing.T) {
	installed, running, enabled, pid := Status()
	if installed {
		t.Errorf("Status() installed = true, want false (gitignore service is not installed on this host)")
	}
	if running {
		t.Errorf("Status() running = true, want false for an uninstalled service")
	}
	_ = enabled
	if pid != 0 {
		t.Errorf("Status() pid = %d, want 0 for a non-running service", pid)
	}
}

// findAvailableSystemID must return an id in the documented safe range,
// skipping every entry in reservedIDs, or an error — never panic.
func TestFindAvailableSystemID(t *testing.T) {
	id, err := findAvailableSystemID()
	if err != nil {
		return
	}
	if id < 200 || id > 899 {
		t.Errorf("findAvailableSystemID() = %d, want in range [200, 899]", id)
	}
	if reservedIDs[id] {
		t.Errorf("findAvailableSystemID() = %d, want a non-reserved id", id)
	}
}

// findAvailableMacOSSystemID must return an id in the documented safe
// range or an error — never panic. On non-darwin hosts the dscl lookups
// it shells out to simply fail, which the function treats as "id free".
func TestFindAvailableMacOSSystemID(t *testing.T) {
	id, err := findAvailableMacOSSystemID()
	if err != nil {
		return
	}
	if id < 200 || id > 399 {
		t.Errorf("findAvailableMacOSSystemID() = %d, want in range [200, 399]", id)
	}
	if reservedIDs[id] {
		t.Errorf("findAvailableMacOSSystemID() = %d, want a non-reserved id", id)
	}
}

func TestCopyBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	if err := os.WriteFile(src, []byte("binary-contents"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("copies contents and creates parent dirs", func(t *testing.T) {
		dst := filepath.Join(dir, "nested", "deeper", "dst-bin")
		if err := copyBinary(src, dst); err != nil {
			t.Fatalf("copyBinary: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("ReadFile(dst): %v", err)
		}
		if string(got) != "binary-contents" {
			t.Errorf("dst contents = %q, want %q", got, "binary-contents")
		}
	})

	t.Run("missing source returns error", func(t *testing.T) {
		dst := filepath.Join(dir, "should-not-exist")
		if err := copyBinary(filepath.Join(dir, "no-such-src"), dst); err == nil {
			t.Error("copyBinary with missing source = nil error, want error")
		}
	})
}

// confirmPrompt reads a line from os.Stdin; substitute a pipe so no real
// terminal input is required.
func TestConfirmPrompt(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase y", "y\n", true},
		{"lowercase yes", "yes\n", true},
		{"uppercase Y", "Y\n", true},
		{"no", "n\n", false},
		{"garbage", "maybe\n", false},
		{"empty line", "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe: %v", err)
			}
			oldStdin := os.Stdin
			os.Stdin = r
			defer func() { os.Stdin = oldStdin }()

			if _, err := w.WriteString(tc.input); err != nil {
				t.Fatalf("WriteString: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("PipeWriter.Close: %v", err)
			}

			if got := confirmPrompt("proceed? "); got != tc.want {
				t.Errorf("confirmPrompt(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEnableAndStartServiceUsesCommandsInOrder(t *testing.T) {
	oldRunner := serviceCommandRunner
	defer func() { serviceCommandRunner = oldRunner }()

	var calls []string
	serviceCommandRunner = func(name string, args ...string) error {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	if err := enableAndStartService("systemd", []string{"systemctl", "enable", appName}, []string{"systemctl", "start", appName}); err != nil {
		t.Fatalf("enableAndStartService() error = %v", err)
	}
	want := []string{"systemctl enable " + appName, "systemctl start " + appName}
	if len(calls) != len(want) {
		t.Fatalf("service command calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("service command call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestEnableAndStartServiceStopsAfterEnableFailure(t *testing.T) {
	oldRunner := serviceCommandRunner
	defer func() { serviceCommandRunner = oldRunner }()

	wantErr := errors.New("enable failed")
	var calls int
	serviceCommandRunner = func(string, ...string) error {
		calls++
		return wantErr
	}
	err := enableAndStartService("systemd", []string{"systemctl", "enable", appName}, []string{"systemctl", "start", appName})
	if !errors.Is(err, wantErr) {
		t.Fatalf("enableAndStartService() error = %v, want wrapped enable error", err)
	}
	if calls != 1 {
		t.Errorf("service command calls = %d, want 1 after enable failure", calls)
	}
}

func TestEnableAndStartServicePropagatesStartFailure(t *testing.T) {
	oldRunner := serviceCommandRunner
	defer func() { serviceCommandRunner = oldRunner }()

	wantErr := errors.New("start failed")
	var calls int
	serviceCommandRunner = func(string, ...string) error {
		calls++
		if calls == 2 {
			return wantErr
		}
		return nil
	}
	err := enableAndStartService("systemd", []string{"systemctl", "enable", appName}, []string{"systemctl", "start", appName})
	if !errors.Is(err, wantErr) {
		t.Fatalf("enableAndStartService() error = %v, want wrapped start error", err)
	}
	if calls != 2 {
		t.Errorf("service command calls = %d, want 2", calls)
	}
}

func TestSetLanguageLocalizesServiceMessages(t *testing.T) {
	oldLanguage := serviceLanguage
	defer SetLanguage(oldLanguage)

	SetLanguage("es")
	if got, want := serviceText("cli.error_unsupported_shell", "shell", "zsh"), "Shell no soportado: zsh"; got != want {
		t.Errorf("serviceText() = %q, want %q", got, want)
	}
}

// Start/Stop/Restart/Reload issue real service-manager control commands
// against the "gitignore" service, but since that service is not installed
// on the test host (see TestStatusOnUninstalledService), these commands
// are no-ops from the OS's perspective: they simply fail with "unit not
// found" or equivalent and return an error. The only thing being verified
// here is that the calls never panic and return a plain error value.
func TestStartStopRestartReloadDoNotPanic(t *testing.T) {
	_ = Start()
	_ = Stop()
	_ = Restart()
	_ = Reload()
}
