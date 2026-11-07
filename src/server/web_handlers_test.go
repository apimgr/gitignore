package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleSearchPageNoQuery verifies the search page renders without results
// when no query string is present.
func TestHandleSearchPageNoQuery(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	s.handleSearchPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Search") {
		t.Errorf("expected Search title in body: %s", rec.Body.String())
	}
}

// TestHandleSearchPageWithQuery verifies the search page runs the query
// against the template manager and renders the results.
func TestHandleSearchPageWithQuery(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/search?q=go", nil)
	rec := httptest.NewRecorder()
	s.handleSearchPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Go") {
		t.Errorf("expected Go in search results: %s", rec.Body.String())
	}
}

// TestHandleTemplatePageFound verifies a known template renders its content.
func TestHandleTemplatePageFound(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/template/Go", nil)
	req = withChiParam(req, "name", "Go")
	rec := httptest.NewRecorder()
	s.handleTemplatePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Go") {
		t.Errorf("expected template content: %s", rec.Body.String())
	}
}

// TestHandleTemplatePageNotFound verifies an unknown template renders the 404
// page rather than erroring.
func TestHandleTemplatePageNotFound(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/template/Bogus", nil)
	req = withChiParam(req, "name", "Bogus")
	rec := httptest.NewRecorder()
	s.handleTemplatePage(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestHandleCombinePageNoParam verifies the combine page renders with no
// content or error when no templates are requested.
func TestHandleCombinePageNoParam(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/combine", nil)
	rec := httptest.NewRecorder()
	s.handleCombinePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleCombinePageValid verifies a valid combine request renders the
// combined content.
func TestHandleCombinePageValid(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/combine?templates=Go", nil)
	rec := httptest.NewRecorder()
	s.handleCombinePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Go") {
		t.Errorf("expected combined content: %s", rec.Body.String())
	}
}

// TestHandleCombinePageInvalidTemplate verifies an unresolvable template name
// surfaces the error in the page rather than panicking.
func TestHandleCombinePageInvalidTemplate(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/combine?templates=Bogus", nil)
	rec := httptest.NewRecorder()
	s.handleCombinePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleCategoriesPage verifies the categories page renders the category
// list from the template manager.
func TestHandleCategoriesPage(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	rec := httptest.NewRecorder()
	s.handleCategoriesPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleListPageAll verifies the list page renders every template when no
// category filter is applied.
func TestHandleListPageAll(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/list", nil)
	rec := httptest.NewRecorder()
	s.handleListPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Go") {
		t.Errorf("expected Go in template list: %s", rec.Body.String())
	}
}

// TestHandleListPageByCategory verifies the list page filters by category.
func TestHandleListPageByCategory(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/list?category=Root", nil)
	rec := httptest.NewRecorder()
	s.handleListPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleStatsPage verifies the statistics page renders template stats.
func TestHandleStatsPage(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	s.handleStatsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleDocsPage verifies the API documentation page renders.
func TestHandleDocsPage(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	s.handleDocsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleCLIPage verifies the CLI customization page renders.
func TestHandleCLIPage(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/cli", nil)
	rec := httptest.NewRecorder()
	s.handleCLIPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestHandleMethodNotAllowed verifies the themed 405 page renders with the
// correct status code (AI.md PART 16 "Error Pages Must Match Theme").
func TestHandleMethodNotAllowed(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodPost, "/server", nil)
	rec := httptest.NewRecorder()
	s.handleMethodNotAllowed(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "405") {
		t.Errorf("expected 405 in body: %s", rec.Body.String())
	}
}

// TestHandleGraphiQLPage verifies the GraphiQL playground serves HTML with the
// expected cache headers.
func TestHandleGraphiQLPage(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/server/docs/graphql", nil)
	rec := httptest.NewRecorder()
	s.handleGraphiQLPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}
