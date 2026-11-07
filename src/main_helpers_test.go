package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestUtilityHelpers(t *testing.T) {
	if !resolveColor(" YES ") || resolveColor("no") {
		t.Fatal("explicit color setting was ignored")
	}
	t.Setenv("NO_COLOR", "")
	if resolveColor("auto") {
		t.Fatal("NO_COLOR was ignored")
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
		if r.URL.Path != "/healthz" {
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
