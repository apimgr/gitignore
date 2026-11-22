package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestLLMSTxtRoutesAndGeneratedEndpoints(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Title = "GitIgnore API"
	cfg.Web.APIContact = "api@example.com"
	cfg.Web.Security.Contact = "security@example.com"
	s := New(&Config{Version: "test", Cfg: cfg})

	for _, path := range []string{"/.well-known/llms.txt", "/llms.txt"} {
		recorder := httptest.NewRecorder()
		s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		body := recorder.Body.String()
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, recorder.Code)
		}
		if recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("GET %s content type = %q", path, recorder.Header().Get("Content-Type"))
		}
		for _, want := range []string{"# GitIgnore API", "Base URL: http://example.com/api/v1", "GET /api/v1/list", "POST http://example.com/api/v1/server/preferences/tokens/primary", "create one linked token per MCP client", "MCP: http://example.com/mcp", "api@example.com", "security@example.com"} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s body missing %q", path, want)
			}
		}
		if strings.Contains(body, "/server/metrics") || strings.Contains(body, "/server/maintenance") {
			t.Errorf("GET %s advertised an internal endpoint", path)
		}
	}

	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/.well-known/llms.txt", nil))
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Errorf("HEAD llms.txt status/body = %d/%q, want 200/empty", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/.well-known/llms.txt", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST llms.txt status = %d, want 405", recorder.Code)
	}
}

func TestLLMSTxtOptionalSections(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Web.LLMS.IncludeEndpoints = false
	cfg.Web.LLMS.IncludeSchemas = true
	cfg.Web.LLMS.CustomSections = []string{"## Project Notes\nUse the catalog API."}
	s := New(&Config{Version: "test", Cfg: cfg})
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	body := recorder.Body.String()
	if strings.Contains(body, "## Endpoints") {
		t.Error("endpoints were included while disabled")
	}
	if !strings.Contains(body, "## OpenAPI request and response schemas") || !strings.Contains(body, "\"openapi\"") {
		t.Error("configured OpenAPI schema was not included")
	}
	if !strings.Contains(body, "## Project Notes") {
		t.Error("custom section was omitted")
	}
}
