package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

// newTestPageServer builds a minimal Server sufficient for rendering static
// standard pages and error pages (no Templates manager needed).
func newTestPageServer() *Server {
	return &Server{config: &Config{Version: "test", Cfg: &config.Config{}}}
}

// doPage issues a GET against a page handler with an optional theme cookie.
func doPage(h http.HandlerFunc, path, themeCookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if themeCookie != "" {
		req.AddCookie(&http.Cookie{Name: "theme", Value: themeCookie})
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// TestStandardPagesRender verifies every mandated standard page returns 200,
// HTML, and content that identifies the page (AI.md PART 16 "Standard pages").
func TestStandardPagesRender(t *testing.T) {
	s := newTestPageServer()
	cases := []struct {
		path    string
		handler http.HandlerFunc
		needle  string
	}{
		{"/server", s.handleServerPage, "Server"},
		{"/server/about", s.handleAboutPage, "About GitIgnore"},
		{"/server/privacy", s.handlePrivacyPage, "Privacy"},
		{"/server/contact", s.handleContactPage, "Contact"},
		{"/server/help", s.handleHelpPage, "Help"},
		{"/server/terms", s.handleTermsPage, "Terms of Use"},
	}
	for _, c := range cases {
		rec := doPage(c.handler, c.path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", c.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: expected text/html, got %q", c.path, ct)
		}
		if !strings.Contains(rec.Body.String(), c.needle) {
			t.Errorf("%s: body missing %q", c.path, c.needle)
		}
	}
}

// TestThemeClassDefaultsDark verifies the server renders theme-dark with no
// theme cookie present (AI.md PART 16 "Theme preference source", default dark).
func TestThemeClassDefaultsDark(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleAboutPage, "/server/about", "")
	if !strings.Contains(rec.Body.String(), `class="theme-dark"`) {
		t.Errorf("expected theme-dark default on <html>, got:\n%s", rec.Body.String()[:200])
	}
}

// TestThemeClassFromCookie verifies a valid theme cookie is rendered on <html>.
func TestThemeClassFromCookie(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleAboutPage, "/server/about", "light")
	if !strings.Contains(rec.Body.String(), `class="theme-light"`) {
		t.Errorf("expected theme-light from cookie, body head:\n%s", rec.Body.String()[:200])
	}
}

// TestThemeClassInvalidCookieFallsBack verifies an invalid theme cookie falls
// back to the dark default rather than emitting an arbitrary class.
func TestThemeClassInvalidCookieFallsBack(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleAboutPage, "/server/about", "neon")
	if !strings.Contains(rec.Body.String(), `class="theme-dark"`) {
		t.Errorf("expected fallback to theme-dark for invalid cookie")
	}
}

// TestThemeSetPersistsCookie verifies the no-JS theme endpoint sets the cookie
// and redirects (AI.md PART 16 <noscript> fallback).
func TestThemeSetPersistsCookie(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodPost, "/server/theme", strings.NewReader("theme=light&return=/"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleThemeSet(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "theme" && c.Value == "light" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected theme=light cookie to be set")
	}
}

// TestThemeSetRejectsOpenRedirect verifies an external return target is ignored.
func TestThemeSetRejectsOpenRedirect(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodPost, "/server/theme", strings.NewReader("theme=dark&return=//evil.example"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleThemeSet(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}
}

// TestNotFoundRendersThemedPage verifies the 404 handler returns a themed HTML
// error page (AI.md PART 16 "Error Pages MUST Match Theme").
func TestNotFoundRendersThemedPage(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleNotFound, "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "theme-dark") {
		t.Errorf("404 page not themed")
	}
	if !strings.Contains(body, "404") {
		t.Errorf("404 page missing status code")
	}
}

// TestManifestIconsExist verifies the manifest references an icon that is
// actually embedded, closing the broken-icon gap from the audit.
func TestManifestIconsExist(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rec := httptest.NewRecorder()
	s.handleManifest(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "/static/images/icon-192.png") {
		t.Errorf("manifest still references non-existent PNG icon")
	}
	if !strings.Contains(body, "/static/images/icon.svg") {
		t.Fatalf("manifest missing icon.svg reference")
	}
	if _, err := staticHTTPFS.Open("images/icon.svg"); err != nil {
		t.Errorf("referenced icon.svg is not embedded: %v", err)
	}
}
