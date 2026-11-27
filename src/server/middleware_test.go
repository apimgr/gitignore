package server

import (
	"crypto/tls"
	"encoding/json"
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
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	apppath "github.com/apimgr/gitignore/src/path"
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

func TestOneShotSecurityReportPageDoesNotLeakReferrerOrCache(t *testing.T) {
	s := &Server{config: &Config{Cfg: config.DefaultConfig()}}
	req := httptest.NewRequest(http.MethodGet, "/server/security/report/track?token=one-shot", nil)
	rec := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
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

func TestSecurityHeadersHonorConfiguredWebSecurity(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Web.HSTS.MaxAgeSeconds = 31536000
	cfg.Web.HSTS.IncludeSubdomains = false
	cfg.Web.HSTS.Preload = false
	cfg.Web.Headers.COOP = "same-origin"
	cfg.Web.Headers.COEP = "require-corp"
	cfg.Web.Headers.CORP = "same-site"
	cfg.Web.Headers.OriginAgentCluster = false
	cfg.Web.Headers.CrossDomainPolicies = "master-only"
	cfg.Web.Headers.NEL.Enabled = false
	cfg.Web.CSP.Mode = "report-only"
	cfg.Web.CSP.ScriptSrcExtra = "https://scripts.example.test"
	s := New(&Config{Cfg: cfg})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "https://example.test/", nil)
	req.TLS = &tls.ConnectionState{}
	s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("HSTS = %q", got)
	}
	for header, want := range map[string]string{
		"Cross-Origin-Opener-Policy":        "same-origin",
		"Cross-Origin-Embedder-Policy":      "require-corp",
		"Cross-Origin-Resource-Policy":      "same-site",
		"X-Permitted-Cross-Domain-Policies": "master-only",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("Origin-Agent-Cluster") != "" || rec.Header().Get("NEL") != "" {
		t.Errorf("disabled headers still emitted: %v", rec.Header())
	}
	policy := rec.Header().Get("Content-Security-Policy-Report-Only")
	if !strings.Contains(policy, "script-src 'self' https://scripts.example.test") {
		t.Errorf("configured CSP source missing: %s", policy)
	}
	if !strings.Contains(rec.Header().Get("Reporting-Endpoints"), "nel=") || len(rec.Header().Values("Report-To")) != 2 {
		t.Errorf("reporting groups missing: %v", rec.Header())
	}
}

func TestNewRateLimiterDefaultsWindow(t *testing.T) {
	rl := newRateLimiter(5, 0)
	if rl.window.Seconds() != 60 {
		t.Errorf("window = %v, want 60s default", rl.window)
	}
}

func TestReplaceConfigHotReloadsRateLimitSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.RateLimit.GlobalBurst = 3
	cfg.Server.RateLimit.Read = config.RateLimitRuleConfig{Requests: 3, Window: 60}
	s := New(&Config{Cfg: cfg})
	global := s.limiter
	read := s.rateLimiters["read"]
	global.persistent = false
	read.persistent = false
	if !global.allowKey("client") || !read.allowKey("client") {
		t.Fatal("first request should be recorded before config reload")
	}

	reloaded := config.DefaultConfig()
	reloaded.Server.RateLimit.GlobalBurst = 1
	reloaded.Server.RateLimit.Read = config.RateLimitRuleConfig{Requests: 1, Window: 30}
	reloaded.Server.RateLimit.Write = config.RateLimitRuleConfig{Requests: 7, Window: 45}
	reloaded.Server.RateLimit.Strict = config.RateLimitRuleConfig{Requests: 2, Window: 120}
	s.ReplaceConfig(reloaded)

	if s.limiter != global || s.rateLimiters["read"] != read {
		t.Fatal("rate limiter instances were replaced during hot reload")
	}
	if global.limitValue() != 1 || read.limitValue() != 1 || read.windowValue() != 30*time.Second {
		t.Fatalf("reloaded rate limits not applied: global=%d read=%d/%s", global.limitValue(), read.limitValue(), read.windowValue())
	}
	if global.allowKey("client") || read.allowKey("client") {
		t.Fatal("hot reload discarded active rate limit counters")
	}
	if s.rateLimiters["write"].limitValue() != 7 || s.rateLimiters["write"].windowValue() != 45*time.Second {
		t.Fatal("write rate-limit settings did not reload")
	}
	if s.strictTokenLimiter.limitValue() != 2 || s.strictTokenLimiter.windowValue() != 120*time.Second {
		t.Fatal("strict-token rate-limit settings did not reload")
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

func TestRequestFloodCountsRateLimitedRequestsAndPersistsBlock(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.RateLimit.GlobalBurst = 1000
	cfg.Server.RateLimit.Read.Requests = 1
	cfg.Server.Security.AbuseDetection.RequestFlood.Multiplier = 10
	s := &Server{
		config:  &Config{Cfg: cfg},
		limiter: newRateLimiter(1000, 60),
		rateLimiters: map[string]*rateLimiter{
			"read":          newRateLimiter(1, 60),
			"write":         newRateLimiter(10, 60),
			"health":        newRateLimiter(10, 60),
			"auth_failures": newRateLimiter(5, 900),
		},
		abuseRequests:   map[string][]abuseRequest{},
		abuseAlerted:    map[string]time.Time{},
		temporaryBlocks: map[string]db.IPBlock{},
	}
	alerts := 0
	s.abuseAlertHandler = func(db.IPBlock) { alerts++ }
	handler := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "198.51.100.21:1234"
	for index := 1; index <= 10; index++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusTooManyRequests
		if index == 1 {
			want = http.StatusNoContent
		}
		if response.Code != want {
			t.Fatalf("request %d status = %d, want %d", index, response.Code, want)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("flood-triggering request status = %d, want 403", response.Code)
	}
	if block, ok := s.temporaryBlocks["198.51.100.21"]; !ok || block.Reason != "request_flood" || block.OffenseCount != 11 {
		t.Errorf("temporary block = %+v, %t", block, ok)
	}
	if alerts != 1 {
		t.Errorf("flood alert count = %d, want 1", alerts)
	}
}

func TestCleanupExpiredIPBlocksPreservesRenewedBlock(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Minute)
	newer := expired.Add(time.Hour)
	s := &Server{temporaryBlocks: map[string]db.IPBlock{
		"192.0.2.1": {IP: "192.0.2.1", ExpiresAt: expired},
		"192.0.2.2": {IP: "192.0.2.2", ExpiresAt: newer},
	}}
	s.CleanupExpiredIPBlocks([]db.IPBlock{{IP: "192.0.2.1", ExpiresAt: expired}, {IP: "192.0.2.2", ExpiresAt: expired}})
	if _, ok := s.temporaryBlocks["192.0.2.1"]; ok {
		t.Fatal("expired block remains in memory")
	}
	if got := s.temporaryBlocks["192.0.2.2"].ExpiresAt; !got.Equal(newer) {
		t.Fatalf("renewed block expiry = %v, want %v", got, newer)
	}
}

func TestSecurityRequestAuditIncludesAnonymousNetworkActor(t *testing.T) {
	paths := apppath.New()
	paths.SetLogsDir(t.TempDir())
	s := &Server{config: &Config{Paths: paths}}
	r := httptest.NewRequest(http.MethodGet, "/blocked", nil)
	r.RemoteAddr = "192.0.2.21:1234"
	s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": "192.0.2.21", "reason": "test"})
	data, err := os.ReadFile(filepath.Join(paths.GetLogsDir(), "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Actor map[string]string `json:"actor"`
	}
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Actor["type"] != "anonymous" || entry.Actor["ip"] != "192.0.2.21" {
		t.Fatalf("audit actor = %#v, want anonymous with client IP", entry.Actor)
	}
	if _, exists := entry.Actor["id"]; exists {
		t.Fatalf("anonymous actor has ID: %#v", entry.Actor)
	}
}

func TestFailedAuthenticationLimitBlocksFollowingRequests(t *testing.T) {
	s := &Server{
		limiter: newRateLimiter(100, 60),
		rateLimiters: map[string]*rateLimiter{
			"read":          newRateLimiter(100, 60),
			"auth_failures": newRateLimiter(1, 900),
		},
	}
	handler := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	request.Header.Set("Authorization", "Bearer invalid-token")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, request)
	if rejected.Code != http.StatusTooManyRequests {
		t.Fatalf("failed-auth request status = %d, want 429", rejected.Code)
	}
	if rejected.Header().Get("Retry-After") == "" {
		t.Fatal("failed-auth rejection omitted Retry-After")
	}
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	lockedOut := httptest.NewRecorder()
	handler.ServeHTTP(lockedOut, request)
	if lockedOut.Code != http.StatusTooManyRequests {
		t.Fatalf("request after failed-auth limit status = %d, want 429", lockedOut.Code)
	}
}

func TestStrictOwnerTokenActionsMatchSpecification(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/server/preferences/token/reveal", true},
		{http.MethodPost, "/server/preferences/token/restore", true},
		{http.MethodPost, "/server/preferences/tokens/17/reveal", true},
		{http.MethodPost, "/server/preferences/tokens/17/rotate", true},
		{http.MethodPost, "/api/v1/server/preferences/tokens/17/reveal", true},
		{http.MethodPost, "/api/v1/server/preferences/tokens/17/rotate", true},
		{http.MethodPost, "/api/v1/server/preferences/tokens/primary/rotate", true},
		{http.MethodDelete, "/api/v1/server/preferences/tokens/primary", true},
		{http.MethodPost, "/api/v1/server/preferences/tokens", false},
		{http.MethodDelete, "/api/v1/server/preferences/tokens/17", false},
		{http.MethodPost, "/server/preferences/tokens/17/revoke", false},
		{http.MethodPost, "/server/preferences/tokens/17/extra/reveal", false},
		{http.MethodGet, "/server/preferences/token/reveal", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if got := isStrictOwnerTokenAction(req); got != tc.want {
			t.Errorf("isStrictOwnerTokenAction(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
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
