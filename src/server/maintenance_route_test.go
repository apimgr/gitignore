package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

func TestMaintenanceStatusUsesAuthenticatedVersionedAPIRoute(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_maintenance0123456789012345678901234567"
	state := &MaintenanceState{}
	state.Enter("database_connection", "Database connection failed", time.Now().UTC())
	s := New(&Config{Version: "test", Cfg: cfg, MaintenanceState: state})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/maintenance", nil)
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/maintenance", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	recorder = httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"active": true`) {
		t.Fatalf("authenticated maintenance status/body = %d/%s, want 503 with active state", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/server/maintenance", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unversioned page path status = %d, want 404", recorder.Code)
	}
}
