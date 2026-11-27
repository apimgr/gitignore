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
	cfg := &config.ServerConfig{}
	cfg.Server.Metrics.Auth.Tokens.Prometheus = token
	return &Server{
		config:  &ServerOptions{Cfg: cfg},
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
	s.metricsServiceHandler("prometheus").ServeHTTP(rec, req)
	return rec
}

// TestMetricsNoTokenIsClosed verifies that missing credentials never disable auth.
func TestMetricsNoTokenIsClosed(t *testing.T) {
	s := newTestServerWithToken("")
	rec := doMetrics(s, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no token configured, got %d", rec.Code)
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

func TestMetricsServicesUseIndependentBearerTokens(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Metrics.Auth.Tokens = config.MetricsTokenConfig{
		Prometheus: "prom-token",
		Grafana:    "grafana-token",
		Loki:       "loki-token",
	}
	s := &Server{config: &ServerOptions{Cfg: cfg}, metrics: metrics.New(metrics.Options{Version: "test"})}
	for _, service := range []string{"prometheus", "grafana", "loki"} {
		t.Run(service, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/server/metrics/"+service, nil)
			request.Header.Set("Authorization", "Bearer wrong-token")
			response := httptest.NewRecorder()
			s.metricsServiceHandler(service).ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("wrong token status = %d, want 401", response.Code)
			}
			token := map[string]string{"prometheus": "prom-token", "grafana": "grafana-token", "loki": "loki-token"}[service]
			request = httptest.NewRequest(http.MethodGet, "/server/metrics/"+service, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response = httptest.NewRecorder()
			s.metricsServiceHandler(service).ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("correct token status = %d, want 200: %s", response.Code, response.Body.String())
			}
			switch service {
			case "prometheus":
				if !strings.Contains(response.Header().Get("Content-Type"), "text/plain") || !strings.Contains(response.Body.String(), "gitignore_app_info") {
					t.Fatal("Prometheus service did not return text exposition")
				}
			case "grafana":
				if !strings.Contains(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), "GitIgnore Metrics") {
					t.Fatal("Grafana service did not return a dashboard JSON document")
				}
			case "loki":
				if !strings.Contains(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), `"streams": []`) {
					t.Fatal("Loki service did not return the streams response")
				}
			}
		})
	}
}

func TestMetricsAllowUnauthenticatedRequiresExplicitConfiguration(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Metrics.Auth.AllowUnauthenticated = true
	s := &Server{config: &ServerOptions{Cfg: cfg}, metrics: metrics.New(metrics.Options{Version: "test"})}
	response := httptest.NewRecorder()
	s.metricsServiceHandler("prometheus").ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/server/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("explicit unauthenticated status = %d, want 200", response.Code)
	}
}
