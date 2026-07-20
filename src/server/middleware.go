package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// clientIP extracts the client's IP address without the ephemeral source
// port, so repeat requests from the same client hit the same rate-limit
// bucket (r.RemoteAddr includes a distinct port per connection).
//
// X-Forwarded-For/X-Real-IP are deliberately NOT trusted here: the project
// has no trusted-proxy CIDR config, so honoring client-supplied headers
// would let any client spoof a fresh IP per request and bypass the rate
// limiter entirely. If a trusted-proxy allowlist is added later (see
// TODO.AI.md), this should switch to trusting those headers only when
// r.RemoteAddr matches an allowlisted proxy, taking the rightmost entry.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// securityHeaders sets the always-on security response headers mandated by
// AI.md PART 11. HSTS is added only when TLS is active on the request, per
// RFC 6797 (never send HSTS over plaintext).
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("Origin-Agent-Cluster", "?1")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
				"img-src 'self' data:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'self'; "+
				"base-uri 'self'")

		sslEnabled := s.config.Cfg != nil && s.config.Cfg.Server.SSL.Enabled
		if r.TLS != nil || sslEnabled {
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
		if s.limiter != nil && !s.limiter.allow(clientIP(r)) {
			sendAPIResponseError(w, "RATE_LIMITED", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}
