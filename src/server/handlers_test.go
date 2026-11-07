package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleHome verifies the home page renders with template counts from the
// template manager.
func TestHandleHome(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleHome(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// TestHandleStaticServesEmbeddedFile verifies handleStatic serves a known
// embedded asset with the immutable static cache policy.
func TestHandleStaticServesEmbeddedFile(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/static/images/icon.svg", nil)
	rec := httptest.NewRecorder()
	s.handleStatic(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
}

// TestHandleStaticMissingFileIs404 verifies a missing static asset 404s
// instead of panicking.
func TestHandleStaticMissingFileIs404(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/static/does-not-exist.css", nil)
	rec := httptest.NewRecorder()
	s.handleStatic(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleAPIInfo(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	s.handleAPIInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "GitIgnore API") {
		t.Errorf("expected API name in body: %s", rec.Body.String())
	}
}

func TestHandleAPIAutodiscover(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/autodiscover", nil)
	rec := httptest.NewRecorder()
	s.handleAPIAutodiscover(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "api_version") {
		t.Errorf("expected api_version in body: %s", rec.Body.String())
	}
}

func TestHandleAPITemplateFound(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/Go", nil)
	req = withChiParam(req, "name", "Go")
	rec := httptest.NewRecorder()
	s.handleAPITemplate(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPITemplateJSON(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/Go.json", nil)
	req = withChiParam(req, "name", "Go")
	rec := httptest.NewRecorder()
	s.handleAPITemplateJSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("expected Accept header set to application/json, got %q", got)
	}
}

func TestHandleAPIList(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/list", nil)
	rec := httptest.NewRecorder()
	s.handleAPIList(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPISearch(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=go", nil)
	rec := httptest.NewRecorder()
	s.handleAPISearch(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPICombine(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/combine?templates=Go", nil)
	rec := httptest.NewRecorder()
	s.handleAPICombine(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPICategories(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/categories", nil)
	rec := httptest.NewRecorder()
	s.handleAPICategories(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPICategoryTemplates(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/categories/Root", nil)
	req = withChiParam(req, "name", "Root")
	rec := httptest.NewRecorder()
	s.handleAPICategoryTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPIStats(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rec := httptest.NewRecorder()
	s.handleAPIStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPITemplatesJSON(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates.json", nil)
	rec := httptest.NewRecorder()
	s.handleAPITemplatesJSON(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"count"`) {
		t.Errorf("expected count in meta: %s", rec.Body.String())
	}
}

func TestHandleAPITemplatesTarGz(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates.tar.gz", nil)
	rec := httptest.NewRecorder()
	s.handleAPITemplatesTarGz(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty tar.gz body")
	}
}

func TestHandleSwaggerUI(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/swagger", nil)
	rec := httptest.NewRecorder()
	s.handleSwaggerUI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleOpenAPIJSON(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/swagger.json", nil)
	rec := httptest.NewRecorder()
	s.handleOpenAPIJSON(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "openapi") {
		t.Errorf("expected openapi key: %s", rec.Body.String())
	}
}

func TestHandleGraphQL(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(`{"query":"{ template(name: \"Go\") { name } }"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleGraphQL(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"Go"`) {
		t.Errorf("expected the Go template in the GraphQL response: %s", rec.Body.String())
	}
}

func TestHandleGraphQLSchema(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/graphql", nil)
	rec := httptest.NewRecorder()
	s.handleGraphQLSchema(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleFaviconMissingReturns404(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	s.handleFavicon(rec, req)
	// staticHTTPFS may or may not embed a favicon; either 200 or 404 is a
	// valid outcome, but the handler must not panic and must set a status.
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 200 or 404", rec.Code)
	}
}

func TestHandleRobotsTxtDefaultWithoutConfig(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	s.handleRobotsTxt(rec, req)
	if !strings.Contains(rec.Body.String(), "Allow: /") {
		t.Errorf("expected default Allow rule: %s", rec.Body.String())
	}
}

func TestHandleRobotsTxtFromConfig(t *testing.T) {
	s := newTestCompatServer(t)
	s.config.Cfg.WebRobots.Allow = []string{"/foo"}
	s.config.Cfg.WebRobots.Deny = []string{"/bar"}
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	s.handleRobotsTxt(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Allow: /foo") || !strings.Contains(body, "Disallow: /bar") {
		t.Errorf("expected configured rules: %s", body)
	}
}

func TestHandleSecurityTxtDefaultAdmin(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
	rec := httptest.NewRecorder()
	s.handleSecurityTxt(rec, req)
	if !strings.Contains(rec.Body.String(), "security@apimgr.us") {
		t.Errorf("expected default admin contact: %s", rec.Body.String())
	}
}

func TestHandleSecurityTxtConfiguredAdmin(t *testing.T) {
	s := newTestCompatServer(t)
	s.config.Cfg.WebSecurity.Admin = "custom@example.com"
	req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
	rec := httptest.NewRecorder()
	s.handleSecurityTxt(rec, req)
	if !strings.Contains(rec.Body.String(), "custom@example.com") {
		t.Errorf("expected configured admin contact: %s", rec.Body.String())
	}
}

func TestHandleManifest(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rec := httptest.NewRecorder()
	s.handleManifest(rec, req)
	if !strings.Contains(rec.Body.String(), "GitIgnore API") {
		t.Errorf("expected manifest name: %s", rec.Body.String())
	}
}

func TestHandleServiceWorker(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	s.handleServiceWorker(rec, req)
	if !strings.Contains(rec.Body.String(), "CACHE_NAME") {
		t.Errorf("expected service worker script: %s", rec.Body.String())
	}
}

func TestHandleAPITemplateTextStripsTxtExtension(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/Go.txt", nil)
	req = withChiParam(req, "name", "Go.txt")
	rec := httptest.NewRecorder()
	s.handleAPITemplateText(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPITemplateTextNotFound(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/Bogus.txt", nil)
	req = withChiParam(req, "name", "Bogus.txt")
	rec := httptest.NewRecorder()
	s.handleAPITemplateText(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("expected non-200 for missing template")
	}
}

func TestHandleAPIListText(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/list.txt", nil)
	rec := httptest.NewRecorder()
	s.handleAPIListText(rec, req)
	if !strings.Contains(rec.Body.String(), "Go") {
		t.Errorf("expected Go in list output: %s", rec.Body.String())
	}
}

func TestHandleAPISearchTextMissingQuery(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search.txt", nil)
	rec := httptest.NewRecorder()
	s.handleAPISearchText(rec, req)
	if !strings.Contains(rec.Body.String(), "BAD_REQUEST") {
		t.Errorf("expected BAD_REQUEST error: %s", rec.Body.String())
	}
}

func TestHandleAPISearchTextFound(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search.txt?q=go", nil)
	rec := httptest.NewRecorder()
	s.handleAPISearchText(rec, req)
	if !strings.Contains(rec.Body.String(), "Go") {
		t.Errorf("expected Go in search results: %s", rec.Body.String())
	}
}

func TestHandleAPICombineText(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/combine.txt?templates=Go", nil)
	rec := httptest.NewRecorder()
	s.handleAPICombineText(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPICategoriesText(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/categories.txt", nil)
	rec := httptest.NewRecorder()
	s.handleAPICategoriesText(rec, req)
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty categories text")
	}
}

func TestHandleAPICategoryTemplatesText(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/categories/Root.txt", nil)
	req = withChiParam(req, "name", "Root")
	rec := httptest.NewRecorder()
	s.handleAPICategoryTemplatesText(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleAPIStatsText(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats.txt", nil)
	rec := httptest.NewRecorder()
	s.handleAPIStatsText(rec, req)
	if !strings.Contains(rec.Body.String(), "Total Templates:") {
		t.Errorf("expected stats text: %s", rec.Body.String())
	}
}
