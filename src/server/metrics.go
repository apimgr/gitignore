package server

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// metricsRegistry holds hand-rolled Prometheus counters. No external metrics
// library is bundled; the registry emits the Prometheus text exposition format
// directly (AI.md PART 20). All metric names carry the mandated
// "gitignore_" project prefix.
type metricsRegistry struct {
	mu           sync.Mutex
	startTime    time.Time
	httpRequests map[httpKey]uint64
}

// httpKey identifies a unique HTTP request-counter series.
type httpKey struct {
	method string
	path   string
	status int
}

// newMetricsRegistry creates an empty registry stamped with the process start.
func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		startTime:    time.Now(),
		httpRequests: make(map[httpKey]uint64),
	}
}

// recordHTTP increments the request counter for a method/path/status series.
func (m *metricsRegistry) recordHTTP(method, path string, status int) {
	m.mu.Lock()
	m.httpRequests[httpKey{method: method, path: path, status: status}]++
	m.mu.Unlock()
}

// statusRecorder captures the response status for metrics accounting.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status code before delegating.
func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// Write ensures a 200 default status is recorded when WriteHeader is skipped.
func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	return sr.ResponseWriter.Write(b)
}

// metricsMiddleware records one HTTP request counter per response. The route
// pattern is used rather than the raw path to keep label cardinality bounded.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.metrics == nil {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		path := r.URL.Path
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if pattern := rctx.RoutePattern(); pattern != "" {
				path = pattern
			}
		}
		s.metrics.recordHTTP(r.Method, path, rec.status)
	})
}

// handleMetrics writes the Prometheus text exposition. INTERNAL ONLY — the
// operator is responsible for firewalling this endpoint (AI.md PART 20).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	setCacheHeaders(w, "authenticated")

	fmt.Fprintf(w, "# HELP gitignore_app_info Application build information.\n")
	fmt.Fprintf(w, "# TYPE gitignore_app_info gauge\n")
	fmt.Fprintf(w, "gitignore_app_info{version=%q,commit=%q,build_date=%q,go_version=%q} 1\n",
		s.config.Version, s.config.Commit, s.config.BuildDate, runtime.Version())

	fmt.Fprintf(w, "# HELP gitignore_uptime_seconds Seconds since the process started.\n")
	fmt.Fprintf(w, "# TYPE gitignore_uptime_seconds gauge\n")
	fmt.Fprintf(w, "gitignore_uptime_seconds %d\n", int64(time.Since(s.metrics.startTime).Seconds()))

	fmt.Fprintf(w, "# HELP gitignore_templates_total Number of loaded gitignore templates.\n")
	fmt.Fprintf(w, "# TYPE gitignore_templates_total gauge\n")
	fmt.Fprintf(w, "gitignore_templates_total %d\n", s.config.Templates.Count())

	s.metrics.mu.Lock()
	keys := make([]httpKey, 0, len(s.metrics.httpRequests))
	for k := range s.metrics.httpRequests {
		keys = append(keys, k)
	}
	counts := make(map[httpKey]uint64, len(s.metrics.httpRequests))
	for k, v := range s.metrics.httpRequests {
		counts[k] = v
	}
	s.metrics.mu.Unlock()

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})

	fmt.Fprintf(w, "# HELP gitignore_http_requests_total Total HTTP requests.\n")
	fmt.Fprintf(w, "# TYPE gitignore_http_requests_total counter\n")
	for _, k := range keys {
		fmt.Fprintf(w, "gitignore_http_requests_total{method=%q,path=%q,status=%q} %d\n",
			k.method, k.path, strconv.Itoa(k.status), counts[k])
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(w, "# HELP gitignore_goroutines Current number of goroutines.\n")
	fmt.Fprintf(w, "# TYPE gitignore_goroutines gauge\n")
	fmt.Fprintf(w, "gitignore_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "# HELP gitignore_memory_alloc_bytes Bytes of allocated heap objects.\n")
	fmt.Fprintf(w, "# TYPE gitignore_memory_alloc_bytes gauge\n")
	fmt.Fprintf(w, "gitignore_memory_alloc_bytes %d\n", ms.Alloc)
}
