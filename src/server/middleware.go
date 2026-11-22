package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/mode"
)

type allowlistedContextKey struct{}

func (s *Server) isAllowlisted(ip string) bool {
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, prefix := range s.allowlist {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (s *Server) allowlistMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isTorRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if s.isAllowlisted(s.clientIP(r)) {
			r = r.WithContext(context.WithValue(r.Context(), allowlistedContextKey{}, true))
		}
		next.ServeHTTP(w, r)
	})
}

func requestIsAllowlisted(r *http.Request) bool {
	allowed, _ := r.Context().Value(allowlistedContextKey{}).(bool)
	return allowed
}

func (s *Server) blocklistMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isTorRequest(r) || requestIsAllowlisted(r) {
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now().UTC()
		ip := s.clientIP(r)
		if address, err := netip.ParseAddr(ip); err == nil {
			for _, prefix := range s.blockedIPs {
				if prefix.Contains(address.Unmap()) {
					log.Printf("security.ip_blocked: ip=[redacted] reason=config")
					sendAPIResponseError(w, "FORBIDDEN", "access denied")
					return
				}
			}
		}
		if s.blocklist == nil {
			next.ServeHTTP(w, r)
			return
		}
		if source, blocked := s.blocklist.MatchIP(ip, now); blocked {
			log.Printf("blocklist_block: source=%s ip=[redacted]", source)
			sendAPIResponseError(w, "FORBIDDEN", "access denied")
			return
		}
		domainHeader := r.Header.Get("Origin")
		domainField := "origin"
		if strings.TrimSpace(domainHeader) == "" {
			domainHeader = r.Header.Get("Referer")
			domainField = "referer"
		}
		if source, blocked := s.blocklist.MatchDomain(requestDomainCandidate(domainHeader), now); blocked {
			log.Printf("blocklist_block: source=%s field=%s", source, domainField)
			sendAPIResponseError(w, "FORBIDDEN", "access denied")
			return
		}
		for _, value := range r.URL.Query() {
			for _, candidate := range value {
				if source, blocked := s.blocklist.MatchDomain(requestDomainCandidate(candidate), now); blocked {
					log.Printf("blocklist_block: source=%s field=query", source)
					sendAPIResponseError(w, "FORBIDDEN", "access denied")
					return
				}
			}
		}
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			if err == nil && len(body) <= 1<<20 {
				r.Body = io.NopCloser(bytes.NewReader(body))
				if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
					if values, parseErr := url.ParseQuery(string(body)); parseErr == nil {
						for key, candidates := range values {
							if strings.Contains(strings.ToLower(key), "email") || strings.Contains(strings.ToLower(key), "url") {
								for _, candidate := range candidates {
									if source, blocked := s.blocklist.MatchDomain(requestDomainCandidate(candidate), now); blocked {
										log.Printf("blocklist_block: source=%s field=form", source)
										sendAPIResponseError(w, "FORBIDDEN", "access denied")
										return
									}
								}
							}
						}
					}
				} else if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
					var fields interface{}
					if json.Unmarshal(body, &fields) == nil {
						for _, candidate := range jsonDomainTargets(fields, false) {
							if source, blocked := s.blocklist.MatchDomain(requestDomainCandidate(candidate), now); blocked {
								log.Printf("blocklist_block: source=%s field=json", source)
								sendAPIResponseError(w, "FORBIDDEN", "access denied")
								return
							}
						}
					}
				}
			} else {
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func jsonDomainTargets(value interface{}, targetField bool) []string {
	var targets []string
	switch item := value.(type) {
	case map[string]interface{}:
		for key, child := range item {
			lower := strings.ToLower(key)
			isTarget := strings.Contains(lower, "email") || strings.Contains(lower, "url")
			targets = append(targets, jsonDomainTargets(child, isTarget)...)
		}
	case []interface{}:
		for _, child := range item {
			targets = append(targets, jsonDomainTargets(child, targetField)...)
		}
	case string:
		if targetField && strings.TrimSpace(item) != "" {
			targets = append(targets, item)
		}
	}
	return targets
}

func requestDomainCandidate(value string) string {
	value = strings.TrimSpace(value)
	if at := strings.LastIndexByte(value, '@'); at >= 0 && at+1 < len(value) {
		return value[at+1:]
	}
	return value
}

// clientIP resolves the client from the nearest proxy hop toward the leftmost
// address, honoring forwarding headers only when the immediate peer is trusted.
func (s *Server) clientIP(r *http.Request) string {
	if identity, ok := r.Context().Value(torRequestContextKey{}).(torRequestIdentity); ok {
		return "tor:" + identity.circuitID
	}
	peer, peerValid := normalizeProxyIP(r.RemoteAddr)
	if !peerValid {
		return ""
	}
	if !s.isTrustedAddress(peer) {
		return peer.String()
	}
	configured := "auto"
	if s.config != nil && s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Server.TrustedProxies.ClientIPHeader) != "" {
		configured = strings.ToLower(strings.TrimSpace(s.currentConfig().Server.TrustedProxies.ClientIPHeader))
	}
	headerNames := []string{configured}
	if configured == "auto" {
		headerNames = []string{"X-Forwarded-For", "Forwarded", "CF-Connecting-IP", "True-Client-IP", "X-Real-IP", "X-Client-IP"}
	}
	for _, name := range headerNames {
		values := r.Header.Values(name)
		if len(values) == 0 {
			continue
		}
		allEmpty := true
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}
		canonicalName := http.CanonicalHeaderKey(name)
		if canonicalName == "X-Forwarded-For" || canonicalName == "Forwarded" {
			chain := forwardedHops(canonicalName, values)
			if len(chain) == 0 {
				continue
			}
			return s.walkProxyChain(chain, peer).String()
		}
		if len(values) != 1 {
			continue
		}
		if addr, ok := normalizeProxyIP(values[0]); ok {
			return addr.String()
		}
	}
	return peer.String()
}

func (s *Server) walkProxyChain(chain []string, peer netip.Addr) netip.Addr {
	if len(chain) > 32 {
		chain = chain[len(chain)-32:]
	}
	nearest := peer
	for index := len(chain) - 1; index >= 0; index-- {
		addr, ok := normalizeProxyIP(chain[index])
		if !ok {
			return nearest
		}
		nearest = addr
		if !s.isTrustedAddress(addr) {
			return addr
		}
	}
	return nearest
}

func forwardedHops(header string, values []string) []string {
	joined := strings.Join(values, ",")
	parts := strings.Split(joined, ",")
	hops := make([]string, 0, len(parts))
	for _, part := range parts {
		if header == "Forwarded" {
			var forValue string
			for _, parameter := range strings.Split(part, ";") {
				key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
				if found && strings.EqualFold(strings.TrimSpace(key), "for") {
					forValue = strings.TrimSpace(value)
					break
				}
			}
			hops = append(hops, forValue)
			continue
		}
		hops = append(hops, strings.TrimSpace(part))
	}
	return hops
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
	addr, ok := normalizeProxyIP(v)
	if !ok {
		return ""
	}
	return addr.String()
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
	return cspDirectivesForCaptcha(tlsActive, api, "simple")
}

func cspDirectivesForCaptcha(tlsActive bool, api, provider string) string {
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
	switch provider {
	case "recaptcha":
		directives = replaceDirective(directives, "script-src", "'self' https://www.google.com/recaptcha/ https://www.gstatic.com/recaptcha/")
		directives = replaceDirective(directives, "frame-src", "'self' https://www.google.com/recaptcha/")
		directives = replaceDirective(directives, "connect-src", "'self' https://www.google.com/recaptcha/")
	case "hcaptcha":
		directives = replaceDirective(directives, "script-src", "'self' https://js.hcaptcha.com https://newassets.hcaptcha.com")
		directives = replaceDirective(directives, "frame-src", "'self' https://newassets.hcaptcha.com https://hcaptcha.com https://*.hcaptcha.com")
		directives = replaceDirective(directives, "connect-src", "'self' https://hcaptcha.com https://*.hcaptcha.com")
	}
	return strings.Join(directives, "; ")
}

func replaceDirective(directives []string, name, value string) []string {
	for index, directive := range directives {
		if strings.HasPrefix(directive, name+" ") {
			directives[index] = name + " " + value
			return directives
		}
	}
	return append(directives, name+" "+value)
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

		sslEnabled := s.currentConfig() != nil && s.currentConfig().Server.SSL.Enabled
		tlsActive := r.TLS != nil || sslEnabled
		if s.isTorRequest(r) || isI2PHost(r.Host) {
			tlsActive = false
		}

		csp := s.trackingCSP(r, s.apiBasePath())
		if mode.IsAppModeDev() {
			h.Set("Content-Security-Policy-Report-Only", csp)
		} else {
			h.Set("Content-Security-Policy", csp)
		}

		base := s.detectServerURL(r)
		reportURL := base + s.apiBasePath() + "/server/reports/default"
		h.Set("Reporting-Endpoints", `default="`+reportURL+`"`)
		h.Set("Report-To", `{"group":"default","max_age":10886400,"endpoints":[{"url":"`+reportURL+`"}]}`)
		h.Set("NEL", `{"report_to":"default","max_age":2592000,"include_subdomains":true}`)

		if tlsActive && !s.isTorRequest(r) {
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
		if s.limiter != nil && !requestIsAllowlisted(r) {
			allowed := s.limiter.allow(s.clientIP(r))
			if s.metrics != nil {
				status := "allowed"
				if !allowed {
					status = "limited"
					s.metrics.RateLimitBlockedTotal.WithLabelValues("per_ip").Inc()
				}
				s.metrics.RateLimitRequestsTotal.WithLabelValues("per_ip", status).Inc()
			}
			if !allowed {
				sendAPIResponseError(w, "RATE_LIMITED", "too many requests")
				return
			}
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
		if s.geoip != nil && s.geoip.Enabled() && !s.isTorRequest(r) && !requestIsAllowlisted(r) {
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
