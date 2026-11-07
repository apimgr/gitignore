package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	apppath "github.com/apimgr/gitignore/src/path"
)

// newTestDebugHandlersServer builds a Server with a mounted router (so
// handleDebugRoutes has routes to walk) and a real template manager and
// path manager (so handleDebugConfig and handleDebugTemplates resolve).
func newTestDebugHandlersServer(t *testing.T) *Server {
	t.Helper()
	s := newTestCompatServer(t)
	s.router = newTestDebugServer().router
	s.router.Get("/", s.handleHome)
	pm := apppath.New()
	pm.SetConfigDir(t.TempDir())
	pm.SetDataDir(t.TempDir())
	pm.SetLogsDir(t.TempDir())
	s.config.Paths = pm
	s.config.Version = "test"
	s.config.Commit = "abc123"
	s.config.BuildDate = "2026-01-01"
	s.config.Cfg = &config.Config{}
	return s
}

// TestHandleDebugRoutes verifies the registered routes are listed as JSON.
func TestHandleDebugRoutes(t *testing.T) {
	s := newTestDebugHandlersServer(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/routes", nil)
	rec := httptest.NewRecorder()
	s.handleDebugRoutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["success"] != true {
		t.Errorf("expected success=true, got %v", payload["success"])
	}
	if count, ok := payload["count"].(float64); !ok || count < 1 {
		t.Errorf("expected at least one route, got %v", payload["count"])
	}
}

// TestHandleDebugConfig verifies the configuration snapshot is returned as
// JSON with the expected fields.
func TestHandleDebugConfig(t *testing.T) {
	s := newTestDebugHandlersServer(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/config", nil)
	rec := httptest.NewRecorder()
	s.handleDebugConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %v", payload["data"])
	}
	if data["version"] != "test" {
		t.Errorf("version = %v, want test", data["version"])
	}
	if data["commit"] != "abc123" {
		t.Errorf("commit = %v, want abc123", data["commit"])
	}
}

// TestHandleDebugTemplates verifies the template statistics are returned as
// JSON.
func TestHandleDebugTemplates(t *testing.T) {
	s := newTestDebugHandlersServer(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/templates", nil)
	rec := httptest.NewRecorder()
	s.handleDebugTemplates(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["success"] != true {
		t.Errorf("expected success=true, got %v", payload["success"])
	}
	if payload["data"] == nil {
		t.Errorf("expected non-nil stats data")
	}
}
