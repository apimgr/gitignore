package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

// newTestSchedulerServer builds a Server backed by a real in-memory-on-disk
// database so handleAPISchedulerStatus can query db.LoadSchedulerStates.
func newTestSchedulerServer(t *testing.T) *Server {
	t.Helper()
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Server{config: &Config{}}
}

func TestHandleAPISchedulerStatusEmpty(t *testing.T) {
	s := newTestSchedulerServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/scheduler", nil)
	rec := httptest.NewRecorder()
	s.handleAPISchedulerStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Errorf("expected zero tasks with empty scheduler state: %s", rec.Body.String())
	}
}

func TestHandleAPISchedulerStatusWithSavedState(t *testing.T) {
	s := newTestSchedulerServer(t)
	if err := db.UpsertSchedulerState(db.SchedulerState{
		TaskID:     "cleanup",
		TaskName:   "Cleanup expired tokens",
		Schedule:   "@hourly",
		LastStatus: "ok",
		RunCount:   3,
		Enabled:    true,
		LastRun:    time.Now(),
		NextRun:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSchedulerState: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/scheduler", nil)
	rec := httptest.NewRecorder()
	s.handleAPISchedulerStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cleanup") {
		t.Errorf("expected saved task in response: %s", rec.Body.String())
	}
}

func TestOpenAPISpecShape(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	spec := s.openAPISpec(req)

	if spec["openapi"] == nil {
		t.Error("expected an 'openapi' version key")
	}
	if spec["paths"] == nil {
		t.Error("expected a 'paths' key")
	}
}
