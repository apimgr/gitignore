package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestFirstIP(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"1.2.3.4", "1.2.3.4"},
		{"1.2.3.4:80", "1.2.3.4"},
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

	t.Run("X-Forwarded-For leftmost used when higher priority headers absent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		req.Header.Set("X-Forwarded-For", "5.5.5.5, 6.6.6.6")
		if got := s.clientIP(req); got != "5.5.5.5" {
			t.Errorf("clientIP() = %q, want 5.5.5.5", got)
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
