package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMaintenanceMiddlewarePreservesReadOnlyPOSTProtocols(t *testing.T) {
	state := &MaintenanceState{}
	state.Enter("database_connection", "Database connection failed", time.Now().UTC())
	s := &Server{config: &Config{MaintenanceState: state}}
	handler := s.maintenanceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/api/graphql", "/api/v1/server/graphql", "/api/mcp", "/mcp", "/api/v1/server/mcp"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Errorf("read-only POST %s returned %d, want 204", path, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/templates", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("write POST returned %d, want 503", recorder.Code)
	}
}
