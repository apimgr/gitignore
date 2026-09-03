package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/mode"
	"github.com/go-chi/chi/v5"
)

// newTestDebugServer builds a minimal Server with a fresh router, mounting the
// debug routes exactly as setupRoutes does (gated on the debug flag).
func newTestDebugServer() *Server {
	s := &Server{config: &Config{Cfg: &config.Config{}}, router: chi.NewRouter()}
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
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("debug on: GET %s = %d, want 200", path, rec.Code)
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
