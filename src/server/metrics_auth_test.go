package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/server/metrics"
)

// newTestServerWithToken builds a minimal Server wired with a metrics instance
// and the given bearer token for exercising the /metrics auth gate.
func newTestServerWithToken(token string) *Server {
	cfg := &config.Config{}
	cfg.Server.Metrics.Token = token
	return &Server{
		config:  &Config{Cfg: cfg},
		metrics: metrics.New(metrics.Options{Version: "test", IncludeRuntime: true}),
	}
}

// doMetrics issues a GET against the metrics handler with an optional auth header.
func doMetrics(s *Server, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	s.metricsHandler().ServeHTTP(rec, req)
	return rec
}

// TestMetricsNoTokenIsOpen verifies that an empty token leaves /metrics open.
func TestMetricsNoTokenIsOpen(t *testing.T) {
	s := newTestServerWithToken("")
	rec := doMetrics(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no token configured, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gitignore_app_info") {
		t.Errorf("expected Prometheus exposition output, got:\n%s", rec.Body.String())
	}
}

// TestMetricsTokenRejectsMissingHeader verifies a configured token rejects
// requests with no Authorization header.
func TestMetricsTokenRejectsMissingHeader(t *testing.T) {
	s := newTestServerWithToken("s3cr3t")
	rec := doMetrics(s, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without header, got %d", rec.Code)
	}
}

// TestMetricsTokenRejectsWrongToken verifies a wrong bearer token is rejected.
func TestMetricsTokenRejectsWrongToken(t *testing.T) {
	s := newTestServerWithToken("s3cr3t")
	rec := doMetrics(s, "Bearer wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
}

// TestMetricsTokenAcceptsCorrectToken verifies the correct bearer token passes.
func TestMetricsTokenAcceptsCorrectToken(t *testing.T) {
	s := newTestServerWithToken("s3cr3t")
	rec := doMetrics(s, "Bearer s3cr3t")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct token, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gitignore_app_info") {
		t.Errorf("expected exposition output, got:\n%s", rec.Body.String())
	}
}
