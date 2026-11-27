package app

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-isatty"
)

func TestUtilityHelpers(t *testing.T) {
	if !resolveColor(" YES ") || resolveColor("no") {
		t.Fatal("explicit color setting was ignored")
	}
	t.Setenv("NO_COLOR", "")
	if got, want := resolveColor("auto"), isatty.IsTerminal(os.Stdout.Fd()); got != want {
		t.Fatalf("empty NO_COLOR resolved color to %v, want TTY detection result %v", got, want)
	}
	if err := ensureRuntimeDir(""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "nested", "runtime")
	if err := ensureRuntimeDir(dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("runtime directory: %v", err)
	}
	for input, want := range map[string]string{"": "/", "/": "/", "///": "/", "api": "/api", "/api/": "/api", " api/v1/// ": "/api/v1"} {
		if got := normalizeBaseURL(input); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{"prod": "production", " production ": "production", "DEV": "development", "unknown": ""} {
		if got := resolveMode(input); got != want {
			t.Errorf("resolveMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCheckHealthSuccessfulServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/server/healthz" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := checkHealth(port); err != nil {
		t.Fatal(err)
	}
}

func TestStartupURLsUseOnlyResolvedDisplayHost(t *testing.T) {
	urls := startupURLs("example.test", "80,443", "/app", "v2", false)
	if len(urls) != 4 || urls[0] != "http://example.test/app" || urls[2] != "https://example.test/app" {
		t.Fatalf("startupURLs() = %#v", urls)
	}
	for _, value := range urls {
		if strings.Contains(value, "localhost") || strings.Contains(value, "127.0.0.1") {
			t.Errorf("startup URL contains a loopback host: %s", value)
		}
	}
	if got := startupURLs("", "64000", "/", "v1", false); len(got) != 0 {
		t.Fatalf("unresolved startup host produced URLs: %#v", got)
	}
}

func TestResolveStartupFQDNUsesDomainEnvironment(t *testing.T) {
	t.Setenv("DOMAIN", " , api.example.test, www.example.test")
	if got := resolveStartupFQDN(); got != "api.example.test" {
		t.Fatalf("resolveStartupFQDN() = %q, want primary DOMAIN entry", got)
	}
	if usableConfiguredHost("localhost") || usableConfiguredHost("127.0.0.1") || usableConfiguredHost("") {
		t.Fatal("loopback or empty display host was accepted")
	}
}
