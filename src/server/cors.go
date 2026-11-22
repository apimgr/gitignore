package server

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var corsAllowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
var corsAllowedHeaders = "Content-Type, Accept, X-Requested-With, Authorization, X-CSRF-Token, X-XSRF-Token, MCP-Protocol-Version, Mcp-Method, Mcp-Name"

const (
	corsLearnedOriginLimit = 256
	corsLearnedOriginTTL   = time.Hour
)

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		allowed, wildcard, credentials, maxAge := s.corsOriginAllowed(r, origin)
		if origin != "" {
			addVaryHeader(w.Header(), "Origin")
		}
		if allowed {
			if wildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Expose-Headers", "Link")
			if credentials && !wildcard {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		if r.Method == http.MethodOptions {
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
				if maxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(maxAge))
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsOriginAllowed(r *http.Request, origin string) (allowed, wildcard, credentials bool, maxAge int) {
	cfg := s.currentConfig()
	if cfg == nil {
		return origin != "", origin != "", false, 86400
	}
	origins := make([]string, 0, len(cfg.Server.CORS.AllowedOrigins)+4)
	for _, configured := range cfg.Server.CORS.AllowedOrigins {
		if strings.TrimSpace(configured) == "" && len(cfg.Server.CORS.AllowedOrigins) == 1 {
			return false, false, false, cfg.Server.CORS.MaxAge
		}
		if canonical, ok := canonicalOrigin(configured); ok || strings.TrimSpace(configured) == "*" {
			if strings.TrimSpace(configured) == "*" {
				origins = append(origins, "*")
			} else {
				origins = append(origins, canonical)
			}
		}
	}
	for _, domain := range strings.FieldsFunc(os.Getenv("DOMAIN"), func(value rune) bool {
		return value == ',' || value == ';' || value == ' ' || value == '\t' || value == '\n'
	}) {
		candidate := domain
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		if canonical, ok := canonicalOrigin(candidate); ok {
			origins = append(origins, canonical)
		}
	}
	s.learnForwardedOrigin(r)
	origins = append(origins, s.learnedCORSOrigins(time.Now())...)
	if len(origins) == 0 {
		origins = append(origins, "*")
	}
	maxAge = cfg.Server.CORS.MaxAge
	if maxAge < 0 {
		maxAge = 0
	}
	credentials = cfg.Server.CORS.AllowCredentials
	canonicalRequestOrigin, valid := canonicalOrigin(origin)
	if !valid {
		return false, false, credentials, maxAge
	}
	for _, candidate := range origins {
		if candidate == "*" {
			return true, true, credentials, maxAge
		}
		if canonical, ok := canonicalOrigin(candidate); ok && canonical == canonicalRequestOrigin {
			return true, false, credentials, maxAge
		}
	}
	return false, false, credentials, maxAge
}

func (s *Server) learnForwardedOrigin(r *http.Request) {
	if r == nil || !s.isTrustedPeer(r.RemoteAddr) {
		return
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		return
	}
	scheme := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]))
	if scheme != "http" && scheme != "https" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if origin, ok := canonicalOrigin(scheme + "://" + host); ok {
		s.learnedOriginsMu.Lock()
		defer s.learnedOriginsMu.Unlock()
		if s.learnedOrigins == nil {
			s.learnedOrigins = make(map[string]time.Time)
		}
		if _, exists := s.learnedOrigins[origin]; !exists && len(s.learnedOrigins) >= corsLearnedOriginLimit {
			var oldestOrigin string
			var oldestExpiry time.Time
			for candidate, expires := range s.learnedOrigins {
				if oldestOrigin == "" || expires.Before(oldestExpiry) {
					oldestOrigin, oldestExpiry = candidate, expires
				}
			}
			delete(s.learnedOrigins, oldestOrigin)
		}
		s.learnedOrigins[origin] = time.Now().Add(corsLearnedOriginTTL)
	}
}

func (s *Server) learnedCORSOrigins(now time.Time) []string {
	s.learnedOriginsMu.Lock()
	defer s.learnedOriginsMu.Unlock()
	origins := make([]string, 0, len(s.learnedOrigins))
	for origin, expires := range s.learnedOrigins {
		if !now.Before(expires) {
			delete(s.learnedOrigins, origin)
			continue
		}
		origins = append(origins, origin)
	}
	return origins
}

func (s *Server) hasLearnedOrigin(origin string, now time.Time) bool {
	s.learnedOriginsMu.Lock()
	defer s.learnedOriginsMu.Unlock()
	expires, ok := s.learnedOrigins[origin]
	if !ok {
		return false
	}
	if !now.Before(expires) {
		delete(s.learnedOrigins, origin)
		return false
	}
	return true
}

func canonicalOrigin(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", false
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		host += ":" + port
	}
	return strings.ToLower(parsed.Scheme) + "://" + host, true
}

func addVaryHeader(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
