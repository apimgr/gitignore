package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNormalizePath verifies UUID and numeric ID path segments are collapsed
// to ":id" for bounded metric label cardinality (AI.md PART 20).
func TestNormalizePath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"no dynamic segment", "/api/v1/templates", "/api/v1/templates"},
		{"numeric id", "/api/v1/users/42", "/api/v1/users/:id/"},
		{"numeric id trailing slash", "/api/v1/users/42/", "/api/v1/users/:id/"},
		{"uuid", "/api/v1/tokens/550e8400-e29b-41d4-a716-446655440000", "/api/v1/tokens/:id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizePath(c.path); got != c.want {
				t.Errorf("normalizePath(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

// TestMetricsResponseWriterWriteHeader verifies the status code is recorded
// and forwarded to the underlying ResponseWriter.
func TestMetricsResponseWriterWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &metricsResponseWriter{ResponseWriter: rec}
	rw.WriteHeader(http.StatusTeapot)

	if rw.status != http.StatusTeapot {
		t.Errorf("rw.status = %d, want %d", rw.status, http.StatusTeapot)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("underlying recorder code = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

// TestMetricsResponseWriterWriteDefaultsStatus verifies a Write with no prior
// WriteHeader call defaults the recorded status to 200.
func TestMetricsResponseWriterWriteDefaultsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &metricsResponseWriter{ResponseWriter: rec}
	n, err := rw.Write([]byte("hello"))

	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned n = %d, want 5", n)
	}
	if rw.status != http.StatusOK {
		t.Errorf("rw.status = %d, want 200", rw.status)
	}
	if rw.size != 5 {
		t.Errorf("rw.size = %d, want 5", rw.size)
	}
}

// TestMetricsResponseWriterWriteAfterWriteHeaderKeepsStatus verifies a
// subsequent Write does not overwrite a status already set by WriteHeader.
func TestMetricsResponseWriterWriteAfterWriteHeaderKeepsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &metricsResponseWriter{ResponseWriter: rec}
	rw.WriteHeader(http.StatusCreated)
	if _, err := rw.Write([]byte("ok")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if rw.status != http.StatusCreated {
		t.Errorf("rw.status = %d, want %d", rw.status, http.StatusCreated)
	}
}

// TestMetricsMiddlewareNoMetricsStillRecordsStats verifies the middleware is a
// no-op passthrough for Prometheus metrics when s.metrics is nil, while still
// recording the public-safe stats counter.
func TestMetricsMiddlewareNoMetricsStillRecordsStats(t *testing.T) {
	s := &Server{stats: newStatsCollector()}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/list", nil)
	rec := httptest.NewRecorder()
	s.metricsMiddleware(next).ServeHTTP(rec, req)

	if !called {
		t.Error("expected wrapped handler to be called")
	}
	total, _, _ := s.stats.snapshot(time.Now())
	if total != 1 {
		t.Errorf("stats total = %d, want 1", total)
	}
}
