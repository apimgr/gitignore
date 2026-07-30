package server

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/mode"
)

// clientIP resolves the client's IP address for logging, rate limiting, and
// blocklists (AI.md PART 12 "Client IP Detection"). Forwarding headers are
// honored only when the immediate TCP peer is in the trusted-proxy set;
// otherwise resolution falls straight through to r.RemoteAddr so a direct
// client cannot spoof a fresh IP per request and bypass the rate limiter.
//
// Priority when the peer is trusted: CF-Connecting-IP, True-Client-IP,
// X-Real-IP, X-Forwarded-For (leftmost), X-Client-IP, then r.RemoteAddr.
func (s *Server) clientIP(r *http.Request) string {
	remote := remoteHost(r.RemoteAddr)

	if s.isTrustedPeer(r.RemoteAddr) {
		if ip := firstIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if ip := firstIP(r.Header.Get("True-Client-IP")); ip != "" {
			return ip
		}
		if ip := firstIP(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip := firstIP(strings.Split(xff, ",")[0]); ip != "" {
				return ip
			}
		}
		if ip := firstIP(r.Header.Get("X-Client-IP")); ip != "" {
			return ip
		}
	}

	return remote
}

// remoteHost strips the ephemeral source port from a "host:port" address so
// repeat requests from the same client share a rate-limit bucket.
func remoteHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// firstIP validates and normalizes a single forwarded IP value, returning ""
// when the value is empty or not a parseable IP address.
func firstIP(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	if net.ParseIP(v) == nil {
		return ""
	}
	return v
}

// permissionsPolicyDefault is the spec default Permissions-Policy header
// (AI.md PART 11 → "Permissions-Policy Configuration"). Sensor/capture
// features are locked to (), advertising-tracking proposals to (), and the
// features the app itself uses are scoped to (self). Per-feature config
// override is not yet wired (that config subtree is a separate subsystem);
// the locked-down default is emitted verbatim.
const permissionsPolicyDefault = "accelerometer=(), ambient-light-sensor=(), battery=(), " +
	"camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), " +
	"idle-detection=(), magnetometer=(), microphone=(), midi=(), " +
	"screen-wake-lock=(), serial=(), usb=(), xr-spatial-tracking=(), " +
	"attribution-reporting=(), browsing-topics=(), interest-cohort=(), " +
	"autoplay=(self), encrypted-media=(self), fullscreen=(self), " +
	"payment=(self), picture-in-picture=(self), " +
	"publickey-credentials-get=(self), storage-access=(self), web-share=(self)"

// cspDirectives builds the Content-Security-Policy value from the PART 11
// per-directive defaults. upgrade-insecure-requests is emitted only when TLS
// is active: over plaintext it would upgrade same-origin subresource fetches
// to https and break the page (documented deviation, matches HSTS gating).
// {learned_origins} is omitted — the domain-learning subsystem is absent.
func cspDirectives(tlsActive bool, api string) string {
	directives := []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self' https:",
		"connect-src 'self'",
		"media-src 'self' blob:",
		"worker-src 'self' blob:",
		"manifest-src 'self'",
		"frame-src 'self'",
		"frame-ancestors 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"object-src 'none'",
	}
	if tlsActive {
		directives = append(directives, "upgrade-insecure-requests")
	}
	directives = append(directives,
		"report-to default",
		"report-uri "+api+"/server/reports/csp",
	)
	return strings.Join(directives, "; ")
}

// securityHeaders sets the always-on security response headers mandated by
// AI.md PART 11. HSTS and upgrade-insecure-requests are gated on TLS being
// active, per RFC 6797 (never send HSTS over plaintext). In development mode
// CSP is emitted in Report-Only form so violations are logged, not blocked.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("Origin-Agent-Cluster", "?1")
		h.Set("Cross-Origin-Opener-Policy", "unsafe-none")
		h.Set("Cross-Origin-Embedder-Policy", "unsafe-none")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Permissions-Policy", permissionsPolicyDefault)

		sslEnabled := s.config.Cfg != nil && s.config.Cfg.Server.SSL.Enabled
		tlsActive := r.TLS != nil || sslEnabled

		csp := cspDirectives(tlsActive, apiBasePath())
		if mode.IsAppModeDev() {
			h.Set("Content-Security-Policy-Report-Only", csp)
		} else {
			h.Set("Content-Security-Policy", csp)
		}

		base := s.detectServerURL(r)
		reportURL := base + apiBasePath() + "/server/reports/default"
		h.Set("Reporting-Endpoints", `default="`+reportURL+`"`)
		h.Set("Report-To", `{"group":"default","max_age":10886400,"endpoints":[{"url":"`+reportURL+`"}]}`)
		h.Set("NEL", `{"report_to":"default","max_age":2592000,"include_subdomains":true}`)

		if tlsActive {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}

// rateLimiter is a fixed-window per-client-IP limiter. It is enabled only when
// the operator turns it on in config (AI.md PART 11).
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string]*rateBucket
	limit  int
	window time.Duration
	lastGC time.Time
}

// rateBucket tracks a single client's request count within the current window.
type rateBucket struct {
	count int
	reset time.Time
}

// newRateLimiter builds a limiter allowing limit requests per window seconds.
func newRateLimiter(limit, windowSeconds int) *rateLimiter {
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	return &rateLimiter{
		hits:   make(map[string]*rateBucket),
		limit:  limit,
		window: time.Duration(windowSeconds) * time.Second,
		lastGC: time.Now(),
	}
}

// allow reports whether the request from ip is within the limit and records it.
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Sub(rl.lastGC) > rl.window {
		for k, b := range rl.hits {
			if now.After(b.reset) {
				delete(rl.hits, k)
			}
		}
		rl.lastGC = now
	}

	b, ok := rl.hits[ip]
	if !ok || now.After(b.reset) {
		rl.hits[ip] = &rateBucket{count: 1, reset: now.Add(rl.window)}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// rateLimitMiddleware rejects requests exceeding the configured per-IP rate
// with the spec's RATE_LIMITED envelope.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter != nil && !s.limiter.allow(s.clientIP(r)) {
			sendAPIResponseError(w, "RATE_LIMITED", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// geoipMiddleware enforces the country-based access policy as a risk signal
// only (AI.md PART 19). It runs after rate limiting and before authentication:
// a blocked-country request has already consumed rate-limit budget. It is
// fail-open — GeoIP disabled, no country database loaded, an unresolved
// country, or a private/loopback IP all pass through. Block events are logged
// with the IP redacted (country data is PII under GDPR).
func (s *Server) geoipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.geoip != nil && s.geoip.Enabled() {
			if ip := net.ParseIP(s.clientIP(r)); ip != nil {
				if allowed, country := s.geoip.CountryAllowed(ip); !allowed {
					log.Printf("geoip_block: ip=[redacted] country=%s", country)
					sendAPIResponseError(w, "FORBIDDEN", "access denied")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
