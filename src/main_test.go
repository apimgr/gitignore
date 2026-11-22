package main

import (
	"context"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/i2p"
	"github.com/apimgr/gitignore/src/scheduler"
)

func TestBuildMetadataAndModeResolution(t *testing.T) {
	if got := buildDate(0); got != "1970-01-01T00:00:00Z" {
		t.Fatalf("buildDate(0) = %q", got)
	}
	oldEpoch := BuildEpoch
	BuildEpoch = "1735689600"
	if got := buildEpoch(); got != 1735689600 {
		t.Fatalf("buildEpoch() = %d", got)
	}
	BuildEpoch = "invalid"
	if got := buildEpoch(); got != 0 {
		t.Fatalf("invalid buildEpoch() = %d, want 0", got)
	}
	BuildEpoch = oldEpoch
	for input, want := range map[string]string{"production": "production", "PROD": "production", "dev": "development", "Development": "development", "unknown": ""} {
		if got := resolveMode(input); got != want {
			t.Errorf("resolveMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestI2PRuntimeHealthAndUnavailableProvider(t *testing.T) {
	runtime := newI2PRuntime(false)
	if info := runtime.healthInfo(); info.Enabled || info.Status != "disabled" || info.Provider != string(i2p.ProviderNone) {
		t.Fatalf("disabled I2P info = %+v", info)
	}
	runtime.setStatus(i2p.Status{State: i2p.StateStarting, Provider: i2p.ProviderSAM})
	if info := runtime.healthInfo(); !info.Enabled || info.Running || info.Status != "starting" {
		t.Fatalf("starting I2P info = %+v", info)
	}
	runtime.setStatus(i2p.Status{State: i2p.StateRunning, Provider: i2p.ProviderI2PD, Address: "site.b32.i2p"})
	if info := runtime.healthInfo(); !info.Enabled || !info.Running || info.Status != "healthy" || info.Hostname != "site.b32.i2p" {
		t.Fatalf("running I2P info = %+v", info)
	}
	runtime.setStatus(i2p.Status{State: i2p.StateError, Provider: i2p.ProviderSAM})
	if info := runtime.healthInfo(); !info.Enabled || info.Status != "unavailable" {
		t.Fatalf("error I2P info = %+v", info)
	}
	if err := runtime.healthCheck(context.Background()); err == nil {
		t.Fatal("inactive runtime health check unexpectedly succeeded")
	}
	runtime.ctx = context.Background()
	runtime.config = i2p.Config{Enabled: true, SAMAddress: "127.0.0.1:1", VirtualPort: 80, InboundLength: 3, OutboundLength: 3, InboundQuantity: 5, OutboundQuantity: 5, SignatureType: 7, BootstrapTimeout: 30 * time.Second}
	runtime.paths = i2p.Paths{ConfigDir: t.TempDir(), DataDir: t.TempDir(), LogDir: t.TempDir()}
	if err := runtime.healthCheck(context.Background()); err == nil {
		t.Fatal("health check with unavailable SAM unexpectedly succeeded")
	}
	if info := runtime.healthInfo(); info.Status != "unavailable" {
		t.Fatalf("failed restart health = %+v", info)
	}
}

func TestSecurityDataHelpers(t *testing.T) {
	for value, want := range map[string]string{
		"":                     filepath.Join("/data", "security"),
		"relative":             filepath.Join("/data", "relative"),
		"${DATA_DIR}/security": filepath.Join("/data", "security"),
		"$DATA_DIR/security":   filepath.Join("/data", "security"),
		"{data_dir}/security":  filepath.Join("/data", "security"),
	} {
		if got := resolveSecurityDataDir(value, "/data"); got != want {
			t.Errorf("resolveSecurityDataDir(%q) = %q, want %q", value, got, want)
		}
	}
	if got := parseDurationOr(" 2h ", time.Minute); got != 2*time.Hour {
		t.Fatalf("valid duration = %s", got)
	}
	for _, value := range []string{"", "invalid", "0s", "-1s"} {
		if got := parseDurationOr(value, time.Minute); got != time.Minute {
			t.Errorf("parseDurationOr(%q) = %s", value, got)
		}
	}
	cfg := config.DefaultConfig()
	enabled := false
	cfg.Server.Security.Blocklists.ExtraSources = []config.BlocklistSourceConfig{{ID: "domains", Type: "domain", URL: "https://lists.example/domains.txt", Enabled: &enabled}}
	cfg.Server.TrustedProxies.Additional = []string{"192.0.2.0/24", "2001:db8::1", "invalid"}
	result := makeBlocklistConfig(cfg, "/data/security")
	if !result.Enabled || result.SecurityDir != "/data/security" || len(result.ExtraSources) != 1 || len(result.TrustedPrefixes) != 2 {
		t.Fatalf("blocklist config = %+v", result)
	}
	if result.TrustedPrefixes[1] != netip.MustParsePrefix("2001:db8::1/128") {
		t.Fatalf("single trusted address prefix = %v", result.TrustedPrefixes[1])
	}
}

func TestSchedulerWiringAndFormatHelpers(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	handler := func(context.Context) error { return nil }
	sched, err := buildScheduler(cfg, "/config", "/data", "/logs", nil, scheduler.Deps{
		BlocklistUpdate: handler, CVEUpdate: handler, DiskCheck: handler,
		DiskCheckInterval: "3m", TorHealth: handler, I2PHealth: handler,
		PurgeIdleOwners: handler, OwnerIdlePurgeDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{"blocklist_update", "cve_update", "disk_check", "token_cleanup", "security_cleanup"} {
		if _, ok := sched.Task(taskID); !ok {
			t.Errorf("required task %q was not registered", taskID)
		}
	}
	for input, want := range map[int64]string{0: "0 bytes", 1024: "1 kilobyte", 1024 * 1024: "1 megabyte"} {
		if got := humanizeBytes(input); got != want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", input, got, want)
		}
	}
	if fmtTime(time.Time{}) != "-" || fmtTime(time.Date(2026, 1, 2, 3, 4, 0, 0, time.Local)) == "-" {
		t.Fatal("fmtTime did not handle zero and non-zero timestamps")
	}
	for status, want := range map[string]string{scheduler.StatusSuccess: "✓", scheduler.StatusFailed: "✗", scheduler.StatusSkipped: "◐", "unknown": "○"} {
		if got := schedulerStatusGlyph(scheduler.TaskInfo{LastStatus: status}); got != want {
			t.Errorf("schedulerStatusGlyph(%q) = %q, want %q", status, got, want)
		}
	}
	if got := schedulerStatusGlyph(scheduler.TaskInfo{Running: true}); got != "●" {
		t.Errorf("running status glyph = %q", got)
	}
	if got := backupFileSize(filepath.Join(t.TempDir(), "missing"), "en"); got != "unknown" {
		t.Errorf("missing backup size = %q", got)
	}
}

func TestSchedulerCLIReadAndToggleCommands(t *testing.T) {
	cfg := config.DefaultConfig()
	for _, args := range [][]string{
		{"list"},
		{"show", "healthcheck_self"},
		{"history", "healthcheck_self"},
		{"disable", "healthcheck_self"},
		{"enable", "healthcheck_self"},
		{"run", "healthcheck_self"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			handleSchedulerCommand(args, cfg, t.TempDir(), t.TempDir(), t.TempDir(), false)
		})
	}
}

func TestMainHelpAndVersionFlags(t *testing.T) {
	for _, args := range [][]string{{"gitignore-test", "--help"}, {"gitignore-test", "--version"}} {
		oldArgs, oldFlags := os.Args, flag.CommandLine
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = args
		flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
		flag.CommandLine.SetOutput(io.Discard)
		oldStdout := os.Stdout
		os.Stdout = writer
		main()
		_ = writer.Close()
		os.Stdout = oldStdout
		os.Args, flag.CommandLine = oldArgs, oldFlags
		output, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if args[1] == "--help" && !strings.Contains(string(output), "Usage:") {
			t.Fatalf("help output missing usage: %s", output)
		}
		if args[1] == "--version" && !strings.Contains(string(output), "OS/Arch:") {
			t.Fatalf("version output missing platform: %s", output)
		}
	}
}

func TestBaseURLAndColorResolution(t *testing.T) {
	for input, want := range map[string]string{"": "/", "/": "/", " app/ ": "/app", "app///": "/app"} {
		if got := normalizeBaseURL(input); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if resolveColor("auto") {
		t.Fatal("auto color ignored NO_COLOR")
	}
	if !resolveColor("yes") {
		t.Fatal("explicit yes did not enable color")
	}
	if resolveColor("no") {
		t.Fatal("explicit no enabled color")
	}
	if err := ensureRuntimeDir(""); err != nil {
		t.Fatalf("empty runtime directory: %v", err)
	}
}

func TestPrintHelpIncludesInvocation(t *testing.T) {
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	printHelp("gitignore-test")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "gitignore-test") || !strings.Contains(string(output), "Usage:") {
		t.Fatalf("help output missing invocation or usage: %s", output)
	}
}

func TestCheckHealthStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/server/healthz" {
			t.Errorf("path = %q, want /server/healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	if strings.Contains(port, "127.0.0.1:") {
		port = strings.TrimPrefix(server.URL, "http://")
	}
	if err := checkHealth(port); err != nil {
		t.Fatalf("healthy endpoint: %v", err)
	}
	prefixed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gitignore/server/healthz" {
			t.Errorf("prefixed path = %q, want /gitignore/server/healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer prefixed.Close()
	prefixedPort := strings.TrimPrefix(prefixed.URL, "http://127.0.0.1:")
	if strings.Contains(prefixedPort, "127.0.0.1:") {
		prefixedPort = strings.TrimPrefix(prefixed.URL, "http://")
	}
	if err := checkHealth(prefixedPort, "/gitignore/"); err != nil {
		t.Fatalf("healthy prefixed endpoint: %v", err)
	}
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failed.Close()
	if err := checkHealth(strings.TrimPrefix(failed.URL, "http://")); err == nil {
		t.Fatal("unhealthy endpoint returned no error")
	}
	if err := checkHealth("0"); err == nil {
		t.Fatal("unreachable health endpoint returned no error")
	}
}
