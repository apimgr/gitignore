package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	apppath "github.com/apimgr/gitignore/src/path"
	tmpl "github.com/apimgr/gitignore/src/template"
)

// TestBaseURLHandlerRedirectsBarePrefix verifies a request for the bare
// prefix is redirected to its trailing-slash form (AI.md PART 8 "--baseurl").
func TestBaseURLHandlerRedirectsBarePrefix(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next should not be called for the bare prefix, got path %q", r.URL.Path)
	})
	h := baseURLHandler("/app", next)

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/" {
		t.Errorf("Location = %q, want /app/", loc)
	}
}

// TestBaseURLHandlerStripsPrefix verifies non-bare requests are dispatched to
// next with the prefix stripped from the path.
func TestBaseURLHandlerStripsPrefix(t *testing.T) {
	var gotPath string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	h := baseURLHandler("/app", next)

	req := httptest.NewRequest(http.MethodGet, "/app/list", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotPath != "/list" {
		t.Errorf("stripped path = %q, want /list", gotPath)
	}
}

// TestParseCookieMaxAge verifies duration suffixes are parsed into seconds and
// invalid input falls back to the one-year default (AI.md PART 30).
func TestParseCookieMaxAge(t *testing.T) {
	const oneYear = 365 * 24 * 60 * 60
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty falls back", "", oneYear},
		{"days", "30d", 30 * 24 * 60 * 60},
		{"hours", "12h", 12 * 60 * 60},
		{"minutes", "5m", 5 * 60},
		{"seconds suffix", "90s", 90},
		{"bare seconds", "3600", 3600},
		{"unknown unit falls back", "10x", oneYear},
		{"negative falls back", "-5d", oneYear},
		{"zero falls back", "0d", oneYear},
		{"non-numeric falls back", "abc", oneYear},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseCookieMaxAge(c.in); got != c.want {
				t.Errorf("parseCookieMaxAge(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestHandleLocaleJSON verifies the default locale catalog is served as JSON.
func TestHandleLocaleJSON(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/locales/en.json", nil)
	req = withChiParam(req, "lang", "en")
	rec := httptest.NewRecorder()
	s.handleLocaleJSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty locale catalog")
	}
}

// TestHandleLocaleJSONUnsupportedFallsBack verifies an unsupported language
// falls back to the default locale rather than 404ing.
func TestHandleLocaleJSONUnsupportedFallsBack(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/locales/xx-not-real.json", nil)
	req = withChiParam(req, "lang", "xx-not-real")
	rec := httptest.NewRecorder()
	s.handleLocaleJSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	want, ok := i18n.LocaleJSON(i18n.DefaultLang)
	if !ok {
		t.Fatalf("default locale %q should exist", i18n.DefaultLang)
	}
	if rec.Body.String() != string(want) {
		t.Errorf("expected default locale catalog body")
	}
}

// TestDetectServerURLFromConfiguredFQDN verifies the configured FQDN wins when
// no trusted-proxy headers are present.
func TestDetectServerURLFromConfiguredFQDN(t *testing.T) {
	s := &Server{config: &Config{Cfg: &config.Config{Server: config.ServerConfig{FQDN: "example.com"}}}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"

	if got := s.detectServerURL(req); got != "https://example.com" {
		t.Errorf("detectServerURL() = %q, want https://example.com", got)
	}
}

// TestDetectServerURLDefaultsToRequestHost verifies the fallback to the
// request's Host header and scheme when nothing else is configured.
func TestDetectServerURLDefaultsToRequestHost(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Host = "gitignore.example"

	if got := s.detectServerURL(req); got != "http://gitignore.example" {
		t.Errorf("detectServerURL() = %q, want http://gitignore.example", got)
	}
}

// TestDetectServerURLTrustsForwardedHeadersFromTrustedPeer verifies
// X-Forwarded-* headers are honored only from a trusted proxy peer
// (AI.md PART 12 "X-Forwarded-* trust gate").
func TestDetectServerURLTrustsForwardedHeadersFromTrustedPeer(t *testing.T) {
	s := &Server{config: &Config{}}
	s.trustedProxies = buildTrustedProxies("127.0.0.1", nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "proxied.example")

	if got := s.detectServerURL(req); got != "https://proxied.example" {
		t.Errorf("detectServerURL() = %q, want https://proxied.example", got)
	}
}

// TestDetectServerURLIgnoresForwardedHeadersFromUntrustedPeer verifies a
// non-trusted peer cannot forge the proto/host used for URL construction.
func TestDetectServerURLIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Host = "direct.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example")

	if got := s.detectServerURL(req); got != "http://direct.example" {
		t.Errorf("detectServerURL() = %q, want http://direct.example", got)
	}
}

// TestNewBuildsRoutableServer verifies New wires up a server with middleware
// and routes mounted, so a basic request resolves.
func TestNewBuildsRoutableServer(t *testing.T) {
	mgr, err := tmpl.New()
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	pm := apppath.New()
	pm.SetDataDir(t.TempDir())

	s := New(&Config{
		Address:   "127.0.0.1",
		Port:      0,
		Templates: mgr,
		Paths:     pm,
		Version:   "test",
		Cfg:       &config.Config{},
	})
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.router == nil {
		t.Fatal("expected router to be set")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}
}

// TestNewMountsBaseURLPrefix verifies a non-root baseurl prefix is mounted so
// requests must include the prefix to reach the router.
func TestNewMountsBaseURLPrefix(t *testing.T) {
	mgr, err := tmpl.New()
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	pm := apppath.New()
	pm.SetDataDir(t.TempDir())

	cfg := &config.Config{}
	cfg.Server.BaseURL = "/app"
	s := New(&Config{
		Address:   "127.0.0.1",
		Port:      0,
		Templates: mgr,
		Paths:     pm,
		Version:   "test",
		Cfg:       cfg,
	})

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /app/ = %d, want 200", rec.Code)
	}
}

// TestShutdownOnUnstartedServer verifies Shutdown returns cleanly on a server
// that was never started (net/http.Server.Shutdown is safe to call in that
// case).
func TestShutdownOnUnstartedServer(t *testing.T) {
	s := &Server{server: &http.Server{Addr: "127.0.0.1:0"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}
