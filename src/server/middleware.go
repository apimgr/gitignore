package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/mode"
)

type allowlistedContextKey struct{}

func (s *Server) isAllowlisted(ip string) bool {
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	s.securityPolicyMu.RLock()
	defer s.securityPolicyMu.RUnlock()
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
		ip := s.clientIP(r)
		if s.isAllowlisted(ip) {
			s.releaseTemporaryBlock(ip, "allowlisted", r)
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
		if s.isTorRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now().UTC()
		ip := s.clientIP(r)
		if requestIsAllowlisted(r) {
			s.releaseTemporaryBlock(ip, "allowlisted", r)
			next.ServeHTTP(w, r)
			return
		}
		if block, blocked := s.temporaryBlock(ip, now); blocked {
			s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": block.Reason, "duration": block.ExpiresAt.Sub(block.BlockedAt).String()})
			sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
			return
		}
		if address, err := netip.ParseAddr(ip); err == nil {
			s.securityPolicyMu.RLock()
			blockedIPs := append([]netip.Prefix(nil), s.blockedIPs...)
			blockedReasons := make(map[string]string, len(s.blockedIPReasons))
			for key, reason := range s.blockedIPReasons {
				blockedReasons[key] = reason
			}
			s.securityPolicyMu.RUnlock()
			for _, prefix := range blockedIPs {
				if prefix.Contains(address.Unmap()) {
					reason := blockedReasons[prefix.String()]
					if reason == "" {
						reason = "configured"
					}
					log.Printf("security.ip_blocked: ip=[redacted] reason=config")
					s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": reason})
					sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
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
			s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": "blocklist:" + source})
			sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
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
			s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": "domain_blocklist:" + source, "field": domainField})
			sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
			return
		}
		for _, value := range r.URL.Query() {
			for _, candidate := range value {
				if source, blocked := s.blocklist.MatchDomain(requestDomainCandidate(candidate), now); blocked {
					log.Printf("blocklist_block: source=%s field=query", source)
					s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": "domain_blocklist:" + source, "field": "query"})
					sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
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
										s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": "domain_blocklist:" + source, "field": "form"})
										sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
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
								s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": "domain_blocklist:" + source, "field": "json"})
								sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
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

type abuseRequest struct {
	at    time.Time
	class string
}

const (
	maxTrackedAbuseIPs = 10_000
	abusePruneInterval = time.Minute
)

func (s *Server) auditSecurityEvent(event, severity, result string, details map[string]any) {
	if s == nil || s.config == nil || s.config.Paths == nil {
		return
	}
	actor := audit.Actor{Type: "system"}
	if ip, ok := details["ip"].(string); ok && ip != "" {
		actor = audit.Actor{Type: "anonymous", IP: ip}
	}
	s.appendSecurityAudit(actor, event, severity, result, details)
}

func (s *Server) auditSecurityRequestEvent(r *http.Request, event, severity, result string, details map[string]any) {
	if s == nil || r == nil {
		return
	}
	actor := audit.Actor{Type: "anonymous", IP: s.auditRequestIP(r)}
	if raw, ok := parseBearerHeader(r.Header.Get("Authorization")); ok {
		if identity, err := s.AuthenticateBearerToken(raw); err == nil {
			switch identity.Kind {
			case "owner":
				if identity.OwnerID != "" {
					actor.Type, actor.ID = "owner", identity.OwnerID
				}
			case "operator":
				actor.Type = "operator"
			}
		}
	}
	if actor.ID == "" && r.Header.Get("Authorization") == "" {
		for _, cookie := range ownerCookies(r) {
			if identity, err := s.AuthenticateBearerToken(cookie.Value); err == nil && identity.Kind == "owner" && identity.OwnerID != "" {
				actor.Type, actor.ID = "owner", identity.OwnerID
				break
			}
		}
	}
	s.appendSecurityAudit(actor, event, severity, result, details)
}

func (s *Server) auditRequestIP(r *http.Request) string {
	if identity, ok := r.Context().Value(torRequestContextKey{}).(torRequestIdentity); ok {
		if identity.circuitID == "" || identity.circuitID == "tor" {
			return "tor"
		}
		return "tor:" + identity.circuitID
	}
	return s.clientIP(r)
}

func (s *Server) appendSecurityAudit(actor audit.Actor, event, severity, result string, details map[string]any) {
	if s == nil || s.config == nil || s.config.Paths == nil {
		return
	}
	if err := audit.Append(s.config.Paths.GetLogsDir(), audit.Entry{
		Event: event, Category: "security", Severity: severity,
		Actor: actor, Details: details, Result: result,
	}); err != nil {
		log.Printf("audit event %s could not be appended", event)
	}
}

func (s *Server) temporaryBlock(ip string, now time.Time) (db.IPBlock, bool) {
	s.abuseMu.Lock()
	block, ok := s.temporaryBlocks[ip]
	if ok && !now.Before(block.ExpiresAt) {
		delete(s.temporaryBlocks, ip)
		ok = false
	}
	s.abuseMu.Unlock()
	if block.ExpiresAt.IsZero() || now.Before(block.ExpiresAt) {
		return block, ok
	}
	_ = db.DeleteIPBlock(ip)
	s.auditSecurityEvent("security.ip_unblocked", "info", "success", map[string]any{"ip": ip, "reason": "expired"})
	return db.IPBlock{}, false
}

func (s *Server) releaseTemporaryBlock(ip, reason string, request ...*http.Request) {
	s.abuseMu.Lock()
	block, exists := s.temporaryBlocks[ip]
	if exists {
		delete(s.temporaryBlocks, ip)
	}
	s.abuseMu.Unlock()
	if !exists {
		return
	}
	if err := db.DeleteIPBlock(ip); err != nil {
		log.Printf("security: failed to release temporary IP block")
	}
	details := map[string]any{"ip": ip, "reason": reason, "block_reason": block.Reason}
	if len(request) > 0 && request[0] != nil {
		s.auditSecurityRequestEvent(request[0], "security.ip_unblocked", "info", "success", details)
	} else {
		s.auditSecurityEvent("security.ip_unblocked", "info", "success", details)
	}
}

func (s *Server) recordAbuseRequest(r *http.Request, ip, class string, now time.Time) (bool, bool) {
	cfg := s.currentConfig()
	if cfg == nil || ip == "" {
		return false, false
	}
	abuse := cfg.Server.Security.AbuseDetection
	window, err := time.ParseDuration(abuse.RequestFlood.Window)
	if err != nil || window <= 0 {
		window = time.Minute
	}
	multiplier := abuse.RequestFlood.Multiplier
	if multiplier < 1 {
		multiplier = 10
	}
	globalLimit := cfg.Server.RateLimit.GlobalBurst
	classLimit := 120
	if s.limiter != nil {
		globalLimit = s.limiter.limitValue()
	}
	if limiter := s.rateLimiters[class]; limiter != nil {
		classLimit = limiter.limitValue()
	} else if rule, ok := map[string]config.RateLimitRuleConfig{"read": cfg.Server.RateLimit.Read, "write": cfg.Server.RateLimit.Write, "health": cfg.Server.RateLimit.Health}[class]; ok && rule.Requests > 0 {
		classLimit = rule.Requests
	}
	if globalLimit < 1 {
		globalLimit = 240
	}
	if classLimit < 1 {
		classLimit = 1
	}
	s.abuseMu.Lock()
	cutoff := now.Add(-window)
	if s.abusePrunedAt.IsZero() || !now.Before(s.abusePrunedAt.Add(abusePruneInterval)) {
		for trackedIP, tracked := range s.abuseRequests {
			tracked = recentAbuseRequests(tracked, cutoff)
			if len(tracked) == 0 {
				delete(s.abuseRequests, trackedIP)
				delete(s.abuseAlerted, trackedIP)
			} else {
				s.abuseRequests[trackedIP] = tracked
			}
		}
		for trackedIP, lastAlert := range s.abuseAlerted {
			if !lastAlert.After(cutoff) {
				delete(s.abuseAlerted, trackedIP)
			}
		}
		s.abusePrunedAt = now
	}
	if _, tracked := s.abuseRequests[ip]; !tracked && len(s.abuseRequests) >= maxTrackedAbuseIPs {
		for oldestIP := range s.abuseRequests {
			delete(s.abuseRequests, oldestIP)
			delete(s.abuseAlerted, oldestIP)
			break
		}
	}
	requests := recentAbuseRequests(s.abuseRequests[ip], cutoff)
	requests = append(requests, abuseRequest{at: now, class: class})
	s.abuseRequests[ip] = requests
	classCount := 0
	for _, request := range requests {
		if request.class == class {
			classCount++
		}
	}
	globalFlood := len(requests) > safeFloodThreshold(globalLimit, multiplier)
	classFlood := classCount > safeFloodThreshold(classLimit, multiplier)
	if !globalFlood && !classFlood {
		s.abuseMu.Unlock()
		return false, false
	}
	if last, alerted := s.abuseAlerted[ip]; alerted && now.Sub(last) < window {
		s.abuseMu.Unlock()
		return false, abuse.AutoBlockIP
	}
	s.abuseAlerted[ip] = now
	blockDuration, err := time.ParseDuration(abuse.RequestFlood.BlockDuration)
	if err != nil || blockDuration <= 0 {
		blockDuration = time.Hour
	}
	block := db.IPBlock{IP: ip, Type: "temporary", Reason: "request_flood", BlockedAt: now.UTC(), ExpiresAt: now.Add(blockDuration).UTC(), OffenseCount: len(requests), AutoBlocked: abuse.AutoBlockIP}
	if abuse.AutoBlockIP {
		s.temporaryBlocks[ip] = block
	}
	alert := s.abuseAlertHandler
	s.abuseMu.Unlock()
	classThreshold := safeFloodThreshold(classLimit, multiplier)
	globalThreshold := safeFloodThreshold(globalLimit, multiplier)
	s.auditSecurityRequestEvent(r, "abuse.request_flood", "warning", "failure", map[string]any{"ip": ip, "class": class, "count": len(requests), "class_count": classCount, "class_threshold": classThreshold, "global_threshold": globalThreshold, "blocked_until": block.ExpiresAt.Format(time.RFC3339)})
	if abuse.AutoBlockIP {
		if err := db.SaveIPBlock(block); err != nil {
			log.Printf("security: failed to persist temporary IP block")
		}
		s.auditSecurityRequestEvent(r, "security.ip_blocked", "warning", "failure", map[string]any{"ip": ip, "reason": block.Reason, "duration": blockDuration.String()})
	}
	if abuse.AutoAlert && alert != nil {
		alert(block)
	}
	return true, abuse.AutoBlockIP
}

func safeFloodThreshold(limit, multiplier int) int {
	if limit < 1 || multiplier < 1 || limit > int(^uint(0)>>1)/multiplier {
		return int(^uint(0) >> 1)
	}
	return limit * multiplier
}

func recentAbuseRequests(requests []abuseRequest, cutoff time.Time) []abuseRequest {
	first := 0
	for first < len(requests) && !requests[first].at.After(cutoff) {
		first++
	}
	if first == len(requests) {
		return requests[:0]
	}
	return append([]abuseRequest(nil), requests[first:]...)
}

// rateLimiter applies a configured sliding-window budget to a client identity.
type rateLimiter struct {
	mu         sync.Mutex
	hits       map[string]*rateBucket
	scope      string
	persistent bool
	limit      int
	window     time.Duration
	lastGC     time.Time
}

// rateBucket tracks a single client's request count within the current window.
type rateBucket struct {
	requests []time.Time
}

// newRateLimiter builds a limiter allowing limit requests per window seconds.
func newRateLimiter(limit, windowSeconds int) *rateLimiter {
	limiter := newRateLimiterFor("default", limit, windowSeconds)
	limiter.persistent = false
	return limiter
}

func newRateLimiterFor(scope string, limit, windowSeconds int) *rateLimiter {
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	if limit < 1 {
		limit = 1
	}
	return &rateLimiter{
		hits:       make(map[string]*rateBucket),
		scope:      scope,
		persistent: true,
		limit:      limit,
		window:     time.Duration(windowSeconds) * time.Second,
		lastGC:     time.Now(),
	}
}

func (rl *rateLimiter) configure(limit, windowSeconds int) {
	if limit < 1 {
		limit = 1
	}
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	rl.mu.Lock()
	rl.limit = limit
	rl.window = time.Duration(windowSeconds) * time.Second
	rl.mu.Unlock()
}

func (rl *rateLimiter) settings() (int, time.Duration, bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.limit, rl.window, rl.persistent
}

func (rl *rateLimiter) limitValue() int {
	limit, _, _ := rl.settings()
	return limit
}

func (rl *rateLimiter) windowValue() time.Duration {
	_, window, _ := rl.settings()
	return window
}

// allow reports whether the request from ip is within the limit and records it.
func (rl *rateLimiter) allow(ip string) bool {
	return rl.allowKey(ip)
}

func (rl *rateLimiter) allowKey(identity string) bool {
	digest := sha256.Sum256([]byte(rl.scope + ":" + identity))
	key := rl.scope + ":" + hex.EncodeToString(digest[:])
	limit, window, persistent := rl.settings()
	if persistent {
		if allowed, err := db.AllowRateLimit(key, limit, int(window.Seconds())); err == nil {
			return allowed
		}
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, window = rl.limit, rl.window
	now := time.Now()
	cutoff := now.Add(-window)
	if now.Sub(rl.lastGC) > window {
		for k, b := range rl.hits {
			b.requests = recentRateLimitRequests(b.requests, cutoff)
			if len(b.requests) == 0 {
				delete(rl.hits, k)
			}
		}
		rl.lastGC = now
	}

	b, ok := rl.hits[key]
	if !ok {
		b = &rateBucket{}
		rl.hits[key] = b
	}
	b.requests = recentRateLimitRequests(b.requests, cutoff)
	if len(b.requests) >= limit {
		return false
	}
	b.requests = append(b.requests, now)
	return true
}

func recentRateLimitRequests(requests []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(requests) && !requests[first].After(cutoff) {
		first++
	}
	return requests[first:]
}

func (rl *rateLimiter) blockedKey(identity string) bool {
	digest := sha256.Sum256([]byte(rl.scope + ":" + identity))
	key := rl.scope + ":" + hex.EncodeToString(digest[:])
	limit, window, persistent := rl.settings()
	if persistent {
		blocked, err := db.RateLimitBlocked(key, limit, int(window.Seconds()))
		if err == nil {
			return blocked
		}
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	limit, window = rl.limit, rl.window
	bucket := rl.hits[key]
	if bucket == nil {
		return false
	}
	bucket.requests = recentRateLimitRequests(bucket.requests, time.Now().Add(-window))
	return len(bucket.requests) >= limit
}

func (rl *rateLimiter) retryAfter(identity string) int {
	digest := sha256.Sum256([]byte(rl.scope + ":" + identity))
	key := rl.scope + ":" + hex.EncodeToString(digest[:])
	_, window, persistent := rl.settings()
	if persistent {
		if seconds, err := db.RateLimitRetryAfter(key, int(window.Seconds())); err == nil && seconds > 0 {
			return seconds
		}
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	window = rl.window
	if bucket := rl.hits[key]; bucket != nil {
		bucket.requests = recentRateLimitRequests(bucket.requests, time.Now().Add(-window))
		if len(bucket.requests) > 0 {
			seconds := int(time.Until(bucket.requests[0].Add(window)).Seconds())
			if seconds < 1 {
				return 1
			}
			return seconds
		}
	}
	return int(window.Seconds())
}

// rateLimitMiddleware rejects requests exceeding the configured per-IP rate
// with the spec's RATE_LIMITED envelope.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestIsAllowlisted(r) {
			next.ServeHTTP(w, r)
			return
		}
		ip := s.clientIP(r)
		rule := "read"
		if isHealthEndpoint(r.URL.Path) {
			rule = "health"
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			rule = "write"
		}
		_, blocked := s.recordAbuseRequest(r, ip, rule, time.Now().UTC())
		if blocked {
			sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
			return
		}
		if s.limiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		globalAllowed := s.limiter.allowKey("ip:" + ip)
		if !globalAllowed {
			s.rejectRateLimited(w, r, s.limiter, "ip:"+ip)
			return
		}
		identity, tokenPresented, tokenValid := s.requestRateLimitIdentity(r, ip)
		if limiter := s.rateLimiters["auth_failures"]; limiter != nil {
			if tokenPresented && !tokenValid {
				s.recordAuthFailure(ip)
			}
			if limiter.blockedKey("ip:" + ip) {
				s.rejectRateLimited(w, r, limiter, "ip:"+ip)
				return
			}
		}
		limits := s.rateLimiters[rule]
		ruleAllowed := isStrictOwnerTokenAction(r) || limits == nil || limits.allowKey(identity)
		if s.metrics != nil {
			status := "allowed"
			if !globalAllowed || !ruleAllowed {
				status = "limited"
				s.metrics.RateLimitBlockedTotal.WithLabelValues(rule).Inc()
			}
			s.metrics.RateLimitRequestsTotal.WithLabelValues(rule, status).Inc()
		}
		if !ruleAllowed {
			s.rejectRateLimited(w, r, limits, identity)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recordAuthFailure(ip string) bool {
	limiter := s.rateLimiters["auth_failures"]
	if limiter == nil {
		return false
	}
	limiter.allowKey("ip:" + ip)
	return limiter.blockedKey("ip:" + ip)
}

func (s *Server) requestRateLimitIdentity(r *http.Request, ip string) (string, bool, bool) {
	if raw, ok := parseBearerHeader(r.Header.Get("Authorization")); ok {
		identity, err := s.AuthenticateBearerToken(raw)
		if err == nil {
			if identity.Kind == "operator" {
				digest := sha256.Sum256([]byte(raw))
				return "token:operator:" + hex.EncodeToString(digest[:]), true, true
			}
			return "token:" + identity.TokenPrefix, true, true
		}
		return "ip:" + ip, true, false
	}
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return "ip:" + ip, true, false
	}
	for _, cookie := range ownerCookies(r) {
		identity, err := s.AuthenticateBearerToken(cookie.Value)
		if err == nil {
			return "token:" + identity.TokenPrefix, true, true
		}
	}
	return "ip:" + ip, false, false
}

func isHealthEndpoint(path string) bool {
	return path == "/healthz" || strings.HasSuffix(path, "/healthz") || strings.HasSuffix(path, "/healthz.txt")
}

func (s *Server) rejectRateLimited(w http.ResponseWriter, r *http.Request, limiter *rateLimiter, identity string) {
	seconds := 60
	if limiter != nil && limiter.windowValue() > 0 {
		seconds = limiter.retryAfter(identity)
	}
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	sendAPIResponseErrorLocalized(w, r, "RATE_LIMITED")
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
					s.auditSecurityRequestEvent(r, "security.country_blocked", "warning", "failure", map[string]any{"ip": ip.String(), "country_code": country})
					sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
