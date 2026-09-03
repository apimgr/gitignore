package server

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// uuidRegex and numericIDRegex normalize dynamic path segments so metric label
// cardinality stays bounded (AI.md PART 20 "Cardinality warning").
var (
	uuidRegex      = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	numericIDRegex = regexp.MustCompile(`/\d+(?:/|$)`)
)

// normalizePath collapses UUIDs and numeric IDs to ":id" for label cardinality.
func normalizePath(path string) string {
	path = uuidRegex.ReplaceAllString(path, ":id")
	path = numericIDRegex.ReplaceAllString(path, "/:id/")
	return path
}

// metricsResponseWriter captures the status code and response byte count for
// HTTP metrics accounting.
type metricsResponseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

// WriteHeader records the status code before delegating.
func (rw *metricsResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write records the number of bytes written, defaulting the status to 200 when
// WriteHeader was never called explicitly.
func (rw *metricsResponseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

// metricsMiddleware records request count, duration, size, and in-flight gauge
// for every HTTP request (AI.md PART 20). It uses the chi route pattern as the
// path label to keep cardinality bounded.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track public-safe aggregate request stats for /server/healthz
		// (AI.md PART 13 "stats") regardless of whether Prometheus metrics
		// are enabled.
		if s.stats != nil {
			done := s.stats.begin(time.Now())
			defer done()
		}

		if s.metrics == nil {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		s.metrics.HTTPActiveRequests.Inc()
		defer s.metrics.HTTPActiveRequests.Dec()

		rw := &metricsResponseWriter{ResponseWriter: w, status: 0}
		next.ServeHTTP(rw, r)
		if rw.status == 0 {
			rw.status = http.StatusOK
		}

		// Prefer the chi route pattern; fall back to a normalized raw path.
		path := normalizePath(r.URL.Path)
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if pattern := rctx.RoutePattern(); pattern != "" {
				path = pattern
			}
		}

		status := strconv.Itoa(rw.status)
		s.metrics.HTTPRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		s.metrics.HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
		if r.ContentLength > 0 {
			s.metrics.HTTPRequestSize.WithLabelValues(r.Method, path).Observe(float64(r.ContentLength))
		}
		s.metrics.HTTPResponseSize.WithLabelValues(r.Method, path).Observe(float64(rw.size))
	})
}

// metricsHandler returns the Prometheus exposition handler for the instance
// registry, optionally gated behind a bearer token (AI.md PART 20). The
// endpoint is INTERNAL ONLY — operators must firewall it externally regardless
// of the token.
func (s *Server) metricsHandler() http.Handler {
	handler := promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{})

	token := ""
	if s.config.Cfg != nil {
		token = s.config.Cfg.Server.Metrics.Token
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCacheHeaders(w, "authenticated")
		if token != "" {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, i18n.T(r, "errors.unauthorized"), http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(w, r)
	})
}
