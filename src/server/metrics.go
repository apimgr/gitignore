package server

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
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

// metricsServiceHandler constructs one authenticated service endpoint. The
// config is read for each request so token rotation and reloads take effect.
func (s *Server) metricsServiceHandler(service string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCacheHeaders(w, "authenticated")
		cfg := s.currentConfig()
		if cfg == nil || !cfg.Server.Metrics.Auth.AllowUnauthenticated {
			token := metricsServiceToken(cfg, service)
			authorization := r.Header.Get("Authorization")
			want := "Bearer " + token
			if token == "" {
				http.Error(w, i18n.T(r, "errors.forbidden"), http.StatusForbidden)
				return
			}
			if len(authorization) != len(want) || subtle.ConstantTimeCompare([]byte(authorization), []byte(want)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, i18n.T(r, "errors.unauthorized"), http.StatusUnauthorized)
				return
			}
		}
		switch service {
		case "prometheus":
			s.refreshCacheMetrics()
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{}).ServeHTTP(w, r)
		case "grafana":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = encodeJSON(w, grafanaDashboard())
		case "loki":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = encodeJSON(w, s.lokiStreams(cfg))
		default:
			http.NotFound(w, r)
		}
	})
}

func metricsServiceToken(cfg *config.Config, service string) string {
	if cfg == nil {
		return ""
	}
	tokens := cfg.Server.Metrics.Auth.Tokens
	switch service {
	case "prometheus":
		return tokens.Prometheus
	case "grafana":
		return tokens.Grafana
	case "loki":
		return tokens.Loki
	default:
		return ""
	}
}

func grafanaDashboard() map[string]interface{} {
	queries := []struct {
		title string
		expr  string
	}{
		{"Request rate", "sum(rate(gitignore_http_requests_total[5m]))"},
		{"Error rate", `sum(rate(gitignore_http_requests_total{status=~"5.."}[5m])) / sum(rate(gitignore_http_requests_total[5m]))`},
		{"Latency p95", `histogram_quantile(0.95, sum by (le) (rate(gitignore_http_request_duration_seconds_bucket[5m])))`},
		{"Active requests", "gitignore_http_active_requests"},
		{"Database query rate", "sum by (operation) (rate(gitignore_db_queries_total[5m]))"},
		{"Database query errors", "sum by (error_type) (rate(gitignore_db_errors_total[5m]))"},
		{"Database connections", "gitignore_db_connections_open"},
		{"Database connections in use", "gitignore_db_connections_in_use"},
		{"Cache hits", "sum by (cache) (rate(gitignore_cache_hits_total[5m]))"},
		{"Cache misses", "sum by (cache) (rate(gitignore_cache_misses_total[5m]))"},
		{"Authentication attempts", "sum by (method, status) (rate(gitignore_auth_attempts_total[5m]))"},
		{"Active owner tokens", "gitignore_owner_tokens_active"},
		{"Scheduler executions", "sum by (task, status) (rate(gitignore_scheduler_tasks_total[5m]))"},
		{"Scheduler task duration p95", "histogram_quantile(0.95, sum by (task, le) (rate(gitignore_scheduler_task_duration_seconds_bucket[1h])))"},
		{"Scheduler running tasks", "sum(gitignore_scheduler_tasks_running)"},
		{"Seconds since last task run", "time() - gitignore_scheduler_last_run_timestamp"},
		{"Rate limited requests", "sum by (limit, status) (rate(gitignore_ratelimit_requests_total[5m]))"},
		{"System CPU usage", "gitignore_system_cpu_usage_percent"},
		{"System memory usage", "gitignore_system_memory_usage_percent"},
		{"System disk usage", "gitignore_system_disk_usage_percent"},
		{"Goroutines", "gitignore_go_goroutines"},
		{"Template count", "gitignore_templates_total"},
		{"Tor requests", "gitignore_tor_requests_total"},
		{"Uptime", "gitignore_app_uptime_seconds"},
	}
	panels := make([]map[string]interface{}, 0, len(queries))
	for index, query := range queries {
		panels = append(panels, map[string]interface{}{
			"id": index + 1, "title": query.title, "type": "timeseries", "datasource": map[string]string{"type": "prometheus", "uid": "${datasource}"},
			"targets": []map[string]string{{"expr": query.expr, "refId": "A"}},
			"gridPos": map[string]int{"h": 8, "w": 12, "x": index % 2 * 12, "y": index / 2 * 8},
		})
	}
	return map[string]interface{}{
		"id": nil, "uid": "gitignore-metrics", "title": "GitIgnore Metrics", "schemaVersion": 39, "version": 1, "timezone": "browser",
		"templating": map[string]interface{}{"list": []map[string]interface{}{{"name": "datasource", "type": "datasource", "query": "prometheus", "current": map[string]string{"text": "Prometheus", "value": "prometheus"}}}},
		"panels":     panels,
	}
}

type lokiStreamsResponse struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

func (s *Server) lokiStreams(cfg *config.Config) lokiStreamsResponse {
	response := lokiStreamsResponse{Streams: []lokiStream{}}
	if s.config == nil || s.config.Paths == nil {
		return response
	}
	path := filepath.Join(s.config.Paths.GetLogsDir(), "audit.log")
	file, err := os.Open(path)
	if err != nil {
		return response
	}
	maxEntries := cfg.Server.Metrics.Loki.MaxEntries
	maxAge, err := time.ParseDuration(cfg.Server.Metrics.Loki.MaxAge)
	if err != nil {
		maxAge = time.Hour
	}
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if maxEntries > 10000 {
		maxEntries = 10000
	}
	if maxAge <= 0 {
		maxAge = time.Hour
	}
	cutoff := time.Now().UTC().Add(-maxAge)
	type auditEntry struct {
		Time string `json:"time"`
	}
	values := make([][2]string, 0, maxEntries)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		var entry auditEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, entry.Time)
		if err != nil || stamp.Before(cutoff) {
			continue
		}
		values = append(values, [2]string{strconv.FormatInt(stamp.UnixNano(), 10), redactMetricLogLine(line)})
		if len(values) > maxEntries {
			values = values[len(values)-maxEntries:]
		}
	}
	if err := scanner.Err(); err != nil {
		_ = file.Close()
		return response
	}
	if err := file.Close(); err != nil {
		return response
	}
	if len(values) > 0 {
		response.Streams = append(response.Streams, lokiStream{Stream: map[string]string{"service": "gitignore", "source": "audit"}, Values: values})
	}
	return response
}

var metricSecretPattern = regexp.MustCompile(`(?i)("?(?:token|password|secret|api_key|encryption_key|authorization)"?\s*[:=]\s*)("?)[^",\s}]+`)

func redactMetricLogLine(line string) string {
	line = metricSecretPattern.ReplaceAllString(line, `${1}${2}xxxxx`)
	line = strings.ReplaceAll(line, "Bearer ", "Bearer xxxxx")
	return line
}
