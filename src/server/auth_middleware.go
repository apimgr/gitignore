package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
)

type contextKey string

const (
	contextKeyUser = contextKey("user")
)

// adminAuthMiddleware validates Bearer token for API routes
func (s *Server) adminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Check for Bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Invalid authorization format. Use: Bearer <token>", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		// Validate token
		valid, err := s.config.Database.ValidateToken(token)
		if err != nil {
			http.Error(w, "Authentication error", http.StatusInternalServerError)
			return
		}

		if !valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Token is valid, proceed
		ctx := context.WithValue(r.Context(), contextKeyUser, "admin")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// basicAuthMiddleware validates username/password for web routes
func (s *Server) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			// Prompt for authentication
			w.Header().Set("WWW-Authenticate", `Basic realm="GitIgnore Admin"`)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		// Check for Basic auth
		if !strings.HasPrefix(authHeader, "Basic ") {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitIgnore Admin"`)
			http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}

		// Decode credentials
		encoded := strings.TrimPrefix(authHeader, "Basic ")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitIgnore Admin"`)
			http.Error(w, "Invalid authorization encoding", http.StatusUnauthorized)
			return
		}

		// Split username:password
		credentials := string(decoded)
		parts := strings.SplitN(credentials, ":", 2)
		if len(parts) != 2 {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitIgnore Admin"`)
			http.Error(w, "Invalid credentials format", http.StatusUnauthorized)
			return
		}

		username := parts[0]
		password := parts[1]

		// Validate credentials
		valid, err := s.config.Database.ValidatePassword(username, password)
		if err != nil {
			http.Error(w, "Authentication error", http.StatusInternalServerError)
			return
		}

		if !valid {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitIgnore Admin"`)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Credentials are valid, proceed
		ctx := context.WithValue(r.Context(), contextKeyUser, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// reverseProxyMiddleware detects and handles reverse proxy headers
func (s *Server) reverseProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if proxy support is enabled
		proxyEnabled, _ := s.config.Database.GetSetting("proxy.enabled")

		if proxyEnabled == "true" {
			// Detect real IP from proxy headers (in priority order)
			realIP := r.RemoteAddr

			// Cloudflare
			if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
				realIP = cfIP
			} else if trueClientIP := r.Header.Get("True-Client-IP"); trueClientIP != "" {
				// Akamai/Cloudflare
				realIP = trueClientIP
			} else if xRealIP := r.Header.Get("X-Real-IP"); xRealIP != "" {
				realIP = xRealIP
			} else if xForwardedFor := r.Header.Get("X-Forwarded-For"); xForwardedFor != "" {
				// X-Forwarded-For can contain multiple IPs
				ips := strings.Split(xForwardedFor, ",")
				if len(ips) > 0 {
					realIP = strings.TrimSpace(ips[0])
				}
			}

			// Update request RemoteAddr
			r.RemoteAddr = realIP

			// Detect protocol
			if xForwardedProto := r.Header.Get("X-Forwarded-Proto"); xForwardedProto != "" {
				if xForwardedProto == "https" {
					r.URL.Scheme = "https"
				}
			}

			// Detect host
			if xForwardedHost := r.Header.Get("X-Forwarded-Host"); xForwardedHost != "" {
				r.Host = xForwardedHost
			}
		}

		// ALWAYS detect and persist server URL on every request
		// This ensures the database stays up-to-date with proxy changes
		// Even when proxy is disabled, we still detect from public IP or hostname
		_ = s.detectServerURL(r)

		next.ServeHTTP(w, r)
	})
}

// getUserFromContext retrieves the authenticated user from context
func getUserFromContext(r *http.Request) string {
	if user, ok := r.Context().Value(contextKeyUser).(string); ok {
		return user
	}
	return ""
}
