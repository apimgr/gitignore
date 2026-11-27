package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/mode"
	"github.com/go-chi/chi/v5"
)

func TestDebugBodyLoggingRedactsSensitiveFieldsAndRestoresRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(`{"username":"alice","password":"secret","nested":{"api_key":"key-value"}}`))
	request.Header.Set("Content-Type", "application/json")
	preview := captureDebugRequestBody(request, "1KB")
	if strings.Contains(preview, "secret") || strings.Contains(preview, "key-value") || !strings.Contains(preview, "[REDACTED]") {
		t.Fatalf("preview did not redact sensitive fields: %s", preview)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if !strings.Contains(string(body), `"password":"secret"`) {
		t.Fatalf("request body changed by debug capture: %s", body)
	}
}

func TestDebugBodyLoggingOmitsUnstructuredBodies(t *testing.T) {
	if got := redactDebugBody("text/plain", []byte("token=private")); !strings.Contains(got, "omitted") {
		t.Fatalf("unstructured body preview = %q, want omitted", got)
	}
}

func TestDebugSQLLiteralsAreRedacted(t *testing.T) {
	got := sanitizeDebugSQL("SELECT * FROM owners WHERE token = 'top-secret' AND note = 'it''s private'")
	if strings.Contains(got, "top-secret") || strings.Contains(got, "private") {
		t.Fatalf("SQL literals were not redacted: %s", got)
	}
}

func TestParseDebugBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{{"10KB", 10 * 1024}, {"1MiB", 1 << 20}, {"bad", 0}} {
		if got := parseDebugBodyLimit(tc.value); got != tc.want {
			t.Errorf("parseDebugBodyLimit(%q) = %d, want %d", tc.value, got, tc.want)
		}
	}
}

// newTestDebugServer builds a minimal Server with a fresh router, mounting the
// debug routes exactly as setupRoutes does (gated on the debug flag).
func newTestDebugServer() *Server {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_debugtest0123456789012345678901234"
	s := &Server{config: &Config{Cfg: cfg}, router: chi.NewRouter()}
	if mode.ShouldShowDebugEndpoints() {
		s.registerDebugRoutes(s.router)
	}
	return s
}

// TestDebugRoutesEnabledWithDebugFlag verifies pprof and expvar endpoints are
// reachable when the independent debug flag is set (AI.md PART 6).
func TestDebugRoutesEnabledWithDebugFlag(t *testing.T) {
	mode.SetDebug(true)
	defer mode.SetDebug(false)

	s := newTestDebugServer()
	for _, path := range []string{"/debug/vars", "/debug/pprof/"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+s.currentConfig().Server.Token)
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("debug on: GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestDebugRoutesRequireOperatorToken(t *testing.T) {
	mode.SetDebug(true)
	defer mode.SetDebug(false)

	s := newTestDebugServer()
	for _, authorization := range []string{"", "Bearer invalid", "Basic " + s.currentConfig().Server.Token} {
		req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("authorization %q: status = %d, want 401", authorization, rec.Code)
		}
	}
}

func TestDebugArgumentViewsExposeOnlyBinaryName(t *testing.T) {
	mode.SetDebug(true)
	defer mode.SetDebug(false)

	s := newTestDebugServer()
	request := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+s.currentConfig().Server.Token)
		s.router.ServeHTTP(rec, req)
		return rec
	}
	want := filepath.Base(os.Args[0])
	cmdline := request("/debug/pprof/cmdline")
	if got := strings.TrimSuffix(cmdline.Body.String(), "\x00"); cmdline.Code != http.StatusOK || got != want {
		t.Fatalf("cmdline response = status %d, %q; want binary name %q", cmdline.Code, got, want)
	}
	vars := request("/debug/vars")
	var values map[string]json.RawMessage
	if err := json.Unmarshal(vars.Body.Bytes(), &values); err != nil {
		t.Fatalf("debug vars JSON: %v", err)
	}
	if value, ok := values["cmdline"]; ok {
		var got string
		if err := json.Unmarshal(value, &got); err != nil || got != want {
			t.Fatalf("debug vars cmdline = %s, want %q", value, want)
		}
	}
}

func TestDebugDiagnosticRoutesAreMountedBehindAuthentication(t *testing.T) {
	mode.SetDebug(true)
	defer mode.SetDebug(false)

	s := newTestDebugServer()
	for _, path := range []string{"/debug/cache", "/debug/db", "/debug/scheduler", "/debug/memory", "/debug/goroutines", "/debug/error500"} {
		unauthorized := httptest.NewRecorder()
		s.router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, path, nil))
		if unauthorized.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated GET %s = %d, want 401", path, unauthorized.Code)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+s.currentConfig().Server.Token)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Errorf("authenticated GET %s = 404, route must be mounted", path)
		}
	}
}

// TestDebugRoutesDisabledByDefault verifies pprof and expvar endpoints return
// 404 when the debug flag is off, in either mode (AI.md PART 6).
func TestDebugRoutesDisabledByDefault(t *testing.T) {
	mode.SetDebug(false)

	s := newTestDebugServer()
	for _, path := range []string{"/debug/vars", "/debug/pprof/", "/debug/pprof/heap", "/debug/routes"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("debug off: GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestDebugEndpointGroupsRespectConfiguration(t *testing.T) {
	mode.SetDebug(true)
	defer mode.SetDebug(false)
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_debugtest0123456789012345678901234"
	cfg.Server.Debug.Pprof = false
	cfg.Server.Debug.RuntimeEndpoints = false
	s := &Server{config: &Config{Cfg: cfg}, router: chi.NewRouter()}
	s.registerDebugRoutes(s.router)
	for _, path := range []string{"/debug/pprof/", "/debug/vars", "/debug/routes"} {
		recorder := httptest.NewRecorder()
		s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, recorder.Code)
		}
	}
}
