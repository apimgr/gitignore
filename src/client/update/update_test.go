package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/client/api"
)

func TestCheckReportsNewerPlatformBuildWithoutInstalling(t *testing.T) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autodiscover" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"data": map[string]interface{}{
				"cli_versions": map[string]interface{}{platform: map[string]string{"version": "9.0.0", "sha256": strings.Repeat("a", 64)}},
			},
		})
	}))
	defer server.Close()

	result, err := Check(context.Background(), api.New(server.URL), "1.0.0", "check", false, false, strings.NewReader(""), &strings.Builder{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Available || result.Version != "9.0.0" || result.Installed {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckEnforcesMinimumVersionWhenNoBuildExists(t *testing.T) {
	previousLogFile := cliLogFile
	cliLogFile = func() string { return filepath.Join(t.TempDir(), "cli.log") }
	t.Cleanup(func() { cliLogFile = previousLogFile })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"data": map[string]interface{}{"cli_min_version": "2.0.0", "cli_versions": map[string]interface{}{}},
		})
	}))
	defer server.Close()

	result, err := Check(context.Background(), api.New(server.URL), "1.0.0", "", false, false, strings.NewReader(""), &strings.Builder{})
	if err == nil || !result.Forced {
		t.Fatalf("Check = (%+v, %v), expected forced minimum error", result, err)
	}
}

func TestCheckValidatesActionAndPublishedChecksum(t *testing.T) {
	if _, err := Check(context.Background(), api.New("https://updates.example.test"), "1.0.0", "install", false, false, strings.NewReader(""), &strings.Builder{}); err == nil {
		t.Fatal("invalid update action was accepted")
	}

	platform := runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"data": map[string]interface{}{
				"cli_versions": map[string]interface{}{platform: map[string]string{"version": "9.0.0", "sha256": "invalid"}},
			},
		})
	}))
	defer server.Close()
	result, err := Check(context.Background(), api.New(server.URL), "1.0.0", "yes", false, false, strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "invalid CLI checksum") || !result.Available {
		t.Fatalf("Check() = (%+v, %v), want checksum rejection", result, err)
	}
}

func TestCheckDeclinesOptionalUpdateAndRejectsInsecureInstallURL(t *testing.T) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/autodiscover" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"data": map[string]interface{}{
					"cli_versions": map[string]interface{}{platform: map[string]string{"version": "9.0.0", "sha256": strings.Repeat("a", 64)}},
				},
			})
			return
		}
		t.Errorf("unexpected download request to insecure server: %s", r.URL.Path)
	}))
	defer server.Close()

	client := api.New(server.URL)
	result, err := Check(context.Background(), client, "1.0.0", "", false, true, strings.NewReader("n\n"), &strings.Builder{})
	if err != nil || !result.Available || result.Installed {
		t.Fatalf("declined update = (%+v, %v)", result, err)
	}
	result, err = Check(context.Background(), client, "1.0.0", "yes", false, false, strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "require an HTTPS") || !result.Available {
		t.Fatalf("insecure install = (%+v, %v), want HTTPS requirement", result, err)
	}
}

func TestValidSHA256(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 64), "  " + strings.Repeat("A", 64) + "  "} {
		if !validSHA256(value) {
			t.Errorf("validSHA256(%q) = false", value)
		}
	}
	for _, value := range []string{"", "abcd", strings.Repeat("z", 64), strings.Repeat("a", 63)} {
		if validSHA256(value) {
			t.Errorf("validSHA256(%q) = true", value)
		}
	}
}

func TestInstallBinaryRejectsHTTPFailuresAndChecksumMismatch(t *testing.T) {
	previousLogFile := cliLogFile
	cliLogFile = func() string { return filepath.Join(t.TempDir(), "cli.log") }
	t.Cleanup(func() { cliLogFile = previousLogFile })

	for _, tc := range []struct {
		name      string
		status    int
		body      string
		length    int64
		wantError string
	}{
		{name: "status", status: http.StatusNotFound, wantError: "HTTP 404"},
		{name: "oversized", status: http.StatusOK, length: maxBinarySize + 1, wantError: "too large"},
		{name: "checksum", status: http.StatusOK, body: "downloaded bytes", wantError: "checksum mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cli/binaries/"+binaryBase+"-"+runtime.GOOS+"-"+runtime.GOARCH {
					t.Errorf("download path = %q", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer update-token" {
					t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
				}
				if tc.length > 0 {
					w.Header().Set("Content-Length", fmt.Sprint(tc.length))
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = io.WriteString(w, tc.body)
				}
			}))
			defer server.Close()
			client := api.New(server.URL)
			client.HTTPClient = server.Client()
			client.Token = "update-token"
			err := installBinary(context.Background(), client, runtime.GOOS+"-"+runtime.GOARCH, "1.0.0", api.BinaryVersion{Version: "2.0.0", SHA256: strings.Repeat("0", 64)})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("installBinary() error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestInstallBinaryRejectsRedirectOutsideConfiguredHost(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/payload", http.StatusFound)
	}))
	defer server.Close()
	client := api.New(server.URL)
	client.HTTPClient = server.Client()
	err := installBinary(context.Background(), client, runtime.GOOS+"-"+runtime.GOARCH, "1.0.0", api.BinaryVersion{Version: "2.0.0", SHA256: strings.Repeat("0", 64)})
	if err == nil || !strings.Contains(err.Error(), "redirect left") {
		t.Fatalf("installBinary() error = %v, want cross-host redirect rejection", err)
	}
}

func TestCheckWithNoPublishedBuildIsNoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": map[string]interface{}{"cli_versions": map[string]interface{}{}}})
	}))
	defer server.Close()
	result, err := Check(context.Background(), api.New(server.URL), "1.0.0", "", false, false, strings.NewReader(""), &strings.Builder{})
	if err != nil || result.Available || result.Forced || result.Installed {
		t.Fatalf("no-build Check() = (%+v, %v)", result, err)
	}
}
