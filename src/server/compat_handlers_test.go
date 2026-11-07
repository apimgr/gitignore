package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	tmpl "github.com/apimgr/gitignore/src/template"
	"github.com/go-chi/chi/v5"
)

// newTestCompatServer builds a Server wired to a real template manager
// (loaded from the embedded gitignore template set) for compat-handler tests.
func newTestCompatServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := tmpl.New()
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	return &Server{
		config: &Config{
			Version:   "test",
			Templates: mgr,
			Cfg:       &config.Config{},
		},
	}
}

func TestHandleCompatListDefaultFormat(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/list", nil)
	rec := httptest.NewRecorder()
	s.handleCompatList(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if !strings.Contains(rec.Body.String(), "go") {
		t.Errorf("expected body to contain 'go': %s", rec.Body.String())
	}
}

func TestHandleCompatListJSONFormat(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/list?format=json", nil)
	rec := httptest.NewRecorder()
	s.handleCompatList(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var result map[string]map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty template map")
	}
}

func withChiParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
	)
}

func TestHandleCompatTemplatesResolvedName(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/go", nil)
	req = withChiParam(req, "list", "go")
	rec := httptest.NewRecorder()
	s.handleCompatTemplates(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "### Go ###") {
		t.Errorf("expected resolved template block: %s", rec.Body.String())
	}
}

func TestHandleCompatTemplatesUnresolvedFirstNameIs404(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bogus-does-not-exist", nil)
	req = withChiParam(req, "list", "bogus-does-not-exist")
	rec := httptest.NewRecorder()
	s.handleCompatTemplates(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "is undefined") {
		t.Errorf("expected error block: %s", rec.Body.String())
	}
}

func TestHandleCompatTemplatesMixedNamesUsesFirstForStatus(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/go,bogus", nil)
	req = withChiParam(req, "list", "go,bogus")
	rec := httptest.NewRecorder()
	s.handleCompatTemplates(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (first name resolved)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "### Go ###") || !strings.Contains(body, "is undefined") {
		t.Errorf("expected both resolved and error blocks: %s", body)
	}
}

func TestThemeFromRequest(t *testing.T) {
	t.Run("no cookie defaults to dark", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if got := themeFromRequest(req); got != "dark" {
			t.Errorf("themeFromRequest() = %q, want dark", got)
		}
	})

	t.Run("valid cookie honored", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
		if got := themeFromRequest(req); got != "light" {
			t.Errorf("themeFromRequest() = %q, want light", got)
		}
	})

	t.Run("invalid cookie falls back to dark", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "theme", Value: "not-a-theme"})
		if got := themeFromRequest(req); got != "dark" {
			t.Errorf("themeFromRequest() = %q, want dark", got)
		}
	})
}
