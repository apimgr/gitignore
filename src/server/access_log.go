package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// AccessLogEntry contains the request fields the configured access formatter supports.
type AccessLogEntry struct {
	Time       time.Time
	RemoteIP   string
	Method     string
	Path       string
	Query      string
	Status     int
	Bytes      int64
	Latency    time.Duration
	UserAgent  string
	Referer    string
	RequestID  string
	Host       string
	Protocol   string
	TLSVersion string
	Country    string
	ASN        string
}

type accessResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *accessResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *accessResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
	return n, err
}

func (w *accessResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		if w.status == 0 {
			w.WriteHeader(http.StatusOK)
		}
		flusher.Flush()
	}
}

func (w *accessResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config == nil || s.config.AccessLog == nil {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		wrapped := &accessResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		if wrapped.status == 0 {
			wrapped.status = http.StatusOK
		}
		if wrapped.status >= http.StatusOK && wrapped.status < http.StatusMultipleChoices && successfulHealthRequest(r.Method, r.URL.Path) {
			return
		}
		remoteIP := s.clientIP(r)
		if isI2PHost(r.Host) {
			remoteIP = "i2p"
		}
		tlsVersion := ""
		if r.TLS != nil {
			tlsVersion = tlsVersionName(r.TLS.Version)
		}
		s.config.AccessLog(AccessLogEntry{
			Time: started.UTC(), RemoteIP: remoteIP, Method: r.Method,
			Path: r.URL.Path, Query: sanitizeAccessQuery(r.URL.RawQuery), Status: wrapped.status,
			Bytes: wrapped.bytes, Latency: time.Since(started), UserAgent: r.UserAgent(),
			Referer: sanitizeAccessReferer(r.Referer()), RequestID: middleware.GetReqID(r.Context()),
			Host: r.Host, Protocol: r.Proto, TLSVersion: tlsVersion,
		})
	})
}

func sanitizeAccessReferer(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[redacted]"
	}
	parsed.User = nil
	parsed.RawQuery = sanitizeAccessQuery(parsed.RawQuery)
	return parsed.String()
}

func sanitizeAccessQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[redacted]"
	}
	for key := range values {
		switch strings.ToLower(key) {
		case "token", "session", "code", "key", "password", "secret", "auth", "pwd", "api_key", "apikey", "access_token", "refresh_token", "authtoken", "security_id", "review_token", "csrf_token":
			values[key] = []string{"xxxxx"}
		}
	}
	return values.Encode()
}

func successfulHealthRequest(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	path = strings.TrimSuffix(path, "/")
	return path == "/healthz" || path == "/api/healthz" || path == "/api/healthz.txt" || strings.HasSuffix(path, "/server/healthz")
}

func tlsVersionName(version uint16) string {
	switch version {
	case 0x0301:
		return "TLS1.0"
	case 0x0302:
		return "TLS1.1"
	case 0x0303:
		return "TLS1.2"
	case 0x0304:
		return "TLS1.3"
	default:
		return ""
	}
}
