package server

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/blocklist"
)

func TestRemoteHost(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"1.2.3.4:5678", "1.2.3.4"},
		{"[::1]:9999", "::1"},
		{"no-port-here", "no-port-here"},
	}
	for _, tc := range cases {
		if got := remoteHost(tc.addr); got != tc.want {
			t.Errorf("remoteHost(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestAllowlistBypassesRateLimitAndBlockedIP(t *testing.T) {
	s := &Server{
		allowlist:  []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		blockedIPs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		limiter:    newRateLimiter(1, 60),
	}
	handler := s.allowlistMiddleware(s.blocklistMiddleware(s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.1.2.3:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("allowlisted request %d status = %d", i, rec.Code)
		}
	}
}

func TestConfiguredIPBlockAndDomainCandidate(t *testing.T) {
	s := &Server{blockedIPs: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}}
	blocked := s.blocklistMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.4:1234"
	rec := httptest.NewRecorder()
	blocked.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("blocked IP status = %d", rec.Code)
	}
	if got := requestDomainCandidate("security@blocked.example"); got != "blocked.example" {
		t.Fatalf("email domain = %q", got)
	}
}

func TestBlocklistMatchesJSONEmailAndFallsBackToReferer(t *testing.T) {
	securityDir := t.TempDir()
	dir := filepath.Join(securityDir, "blocklists")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	updated := []byte(time.Now().UTC().Format(time.RFC3339))
	for id, contents := range map[string][]byte{blocklist.FireHOLID: []byte("203.0.113.0/24\n"), "domains": []byte("evil.example\n")} {
		if err := os.WriteFile(filepath.Join(dir, id+".txt"), contents, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".last_updated"), updated, 0600); err != nil {
			t.Fatal(err)
		}
	}
	enabled := true
	matcher := blocklist.NewMatcher(securityDir, []blocklist.Source{{ID: "domains", Type: "domain", URL: "https://lists.example/domains.txt", Enabled: &enabled}})
	if err := matcher.Reload(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	s := &Server{blocklist: matcher}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
		if r.Method == http.MethodPost && string(body) != `{"email":"user@good.example"}` {
			t.Errorf("body changed by middleware: %s", body)
		}
	})
	handler := s.blocklistMiddleware(next)
	blocked := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(`{"email":"user@evil.example"}`))
	blocked.RemoteAddr = "192.0.2.1:1234"
	blocked.Header.Set("Content-Type", "application/json")
	blockedRec := httptest.NewRecorder()
	handler.ServeHTTP(blockedRec, blocked)
	if blockedRec.Code != http.StatusForbidden {
		t.Fatalf("JSON email block status = %d", blockedRec.Code)
	}
	allowed := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(`{"email":"user@good.example"}`))
	allowed.RemoteAddr = "192.0.2.1:1234"
	allowed.Header.Set("Content-Type", "application/json")
	allowed.Header.Set("Referer", "https://good.example/form")
	allowedRec := httptest.NewRecorder()
	handler.ServeHTTP(allowedRec, allowed)
	if allowedRec.Code != http.StatusNoContent {
		t.Fatalf("allowed JSON request status = %d", allowedRec.Code)
	}
	refererBlocked := httptest.NewRequest(http.MethodGet, "/", nil)
	refererBlocked.RemoteAddr = "192.0.2.1:1234"
	refererBlocked.Header.Set("Referer", "https://evil.example/form")
	refererRec := httptest.NewRecorder()
	handler.ServeHTTP(refererRec, refererBlocked)
	if refererRec.Code != http.StatusForbidden {
		t.Fatalf("Referer block status = %d", refererRec.Code)
	}
	originWins := httptest.NewRequest(http.MethodGet, "/", nil)
	originWins.RemoteAddr = "192.0.2.1:1234"
	originWins.Header.Set("Origin", "https://good.example")
	originWins.Header.Set("Referer", "https://evil.example/form")
	originRec := httptest.NewRecorder()
	handler.ServeHTTP(originRec, originWins)
	if originRec.Code != http.StatusNoContent {
		t.Fatalf("Origin did not take precedence over Referer: status %d", originRec.Code)
	}
}

func TestFirstIP(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"1.2.3.4", "1.2.3.4"},
		{"1.2.3.4:80", "1.2.3.4"},
		{"[2001:DB8::1]:443", "2001:db8::1"},
		{"::ffff:192.0.2.1", "192.0.2.1"},
		{"fe80::1%eth0", "fe80::1"},
		{"not-an-ip", ""},
		{"  10.0.0.1  ", "10.0.0.1"},
	}
	for _, tc := range cases {
		if got := firstIP(tc.in); got != tc.want {
			t.Errorf("firstIP(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClientIPUntrustedPeerIgnoresHeaders(t *testing.T) {
	s := &Server{trustedProxies: buildTrustedProxies("", nil)}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	if got := s.clientIP(req); got != "203.0.113.9" {
		t.Errorf("clientIP() = %q, want 203.0.113.9 (untrusted peer headers ignored)", got)
	}
}

func TestClientIPTrustedPeerHonorsForwardedHeaderPriority(t *testing.T) {
	s := &Server{trustedProxies: buildTrustedProxies("", []string{"203.0.113.5"})}

	t.Run("CF-Connecting-IP wins", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		req.Header.Set("CF-Connecting-IP", "1.1.1.1")
		req.Header.Set("X-Real-IP", "2.2.2.2")
		if got := s.clientIP(req); got != "1.1.1.1" {
			t.Errorf("clientIP() = %q, want 1.1.1.1", got)
		}
	})

	t.Run("X-Forwarded-For resolves from nearest hop on the right", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		req.Header.Set("X-Forwarded-For", "5.5.5.5, 6.6.6.6")
		if got := s.clientIP(req); got != "6.6.6.6" {
			t.Errorf("clientIP() = %q, want 6.6.6.6", got)
		}
	})

	t.Run("skips trusted proxy hops and stops at first untrusted hop", func(t *testing.T) {
		s.trustedProxies = buildTrustedProxies("", []string{"203.0.113.5", "198.51.100.0/24"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")
		if got := s.clientIP(req); got != "203.0.113.9" {
			t.Errorf("clientIP() = %q, want 203.0.113.9", got)
		}
	})

	t.Run("invalid rightmost hop falls back to the peer", func(t *testing.T) {
		s.trustedProxies = buildTrustedProxies("", []string{"203.0.113.5"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.9, unknown")
		if got := s.clientIP(req); got != "203.0.113.5" {
			t.Errorf("clientIP() = %q, want 203.0.113.5", got)
		}
	})

	t.Run("Forwarded RFC 7239 chain", func(t *testing.T) {
		s.trustedProxies = buildTrustedProxies("", []string{"203.0.113.5"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		req.Header.Set("Forwarded", `for=203.0.113.9;proto=https, for="[2001:DB8::1]:443"`)
		if got := s.clientIP(req); got != "2001:db8::1" {
			t.Errorf("clientIP() = %q, want 2001:db8::1", got)
		}
	})

	t.Run("falls through to remote addr when no headers set", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		if got := s.clientIP(req); got != "203.0.113.5" {
			t.Errorf("clientIP() = %q, want 203.0.113.5", got)
		}
	})
}

func TestCSPDirectives(t *testing.T) {
	t.Run("plaintext omits upgrade-insecure-requests", func(t *testing.T) {
		csp := cspDirectives(false, "/api/v1")
		if strings.Contains(csp, "upgrade-insecure-requests") {
			t.Error("expected no upgrade-insecure-requests over plaintext")
		}
		if !strings.Contains(csp, "report-uri /api/v1/server/reports/csp") {
			t.Errorf("missing report-uri directive: %s", csp)
		}
	})

	t.Run("tls active includes upgrade-insecure-requests", func(t *testing.T) {
		csp := cspDirectives(true, "/api/v1")
		if !strings.Contains(csp, "upgrade-insecure-requests") {
			t.Error("expected upgrade-insecure-requests when TLS active")
		}
	})
}

func TestSecurityHeadersSetsExpectedHeaders(t *testing.T) {
	s := &Server{config: &Config{}}
	handler := s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Permissions-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("expected header %q to be set", h)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" && rec.Header().Get("Content-Security-Policy-Report-Only") == "" {
		t.Error("expected a CSP header (enforced or report-only) to be set")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("expected no HSTS header over plaintext")
	}
}

func TestSecurityHeadersHSTSWhenTLS(t *testing.T) {
	s := &Server{config: &Config{}}
	handler := s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("expected HSTS header when TLS active")
	}
	if rec.Header().Get("Content-Security-Policy") == "" && rec.Header().Get("Content-Security-Policy-Report-Only") == "" {
		t.Error("expected a CSP header (enforced or report-only) to be set")
	}
}

func TestNewRateLimiterDefaultsWindow(t *testing.T) {
	rl := newRateLimiter(5, 0)
	if rl.window.Seconds() != 60 {
		t.Errorf("window = %v, want 60s default", rl.window)
	}
}

func TestRateLimiterAllow(t *testing.T) {
	rl := newRateLimiter(2, 60)
	if !rl.allow("1.2.3.4") {
		t.Error("1st request should be allowed")
	}
	if !rl.allow("1.2.3.4") {
		t.Error("2nd request should be allowed")
	}
	if rl.allow("1.2.3.4") {
		t.Error("3rd request should be rejected (limit=2)")
	}
	if !rl.allow("5.6.7.8") {
		t.Error("different IP should have its own bucket")
	}
}

func TestRateLimitMiddlewareRejectsOverLimit(t *testing.T) {
	s := &Server{limiter: newRateLimiter(1, 60)}
	handler := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "9.9.9.9:1234"

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Errorf("1st request status = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code == http.StatusOK {
		t.Error("2nd request should have been rate limited")
	}
}

func TestRateLimitMiddlewareNilLimiterPassesThrough(t *testing.T) {
	s := &Server{}
	called := false
	handler := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Error("expected next handler to run when limiter is nil")
	}
}

func TestGeoipMiddlewareNilGeoipPassesThrough(t *testing.T) {
	s := &Server{}
	called := false
	handler := s.geoipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Error("expected next handler to run when geoip is nil")
	}
}
