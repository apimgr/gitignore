// Package metrics implements the Prometheus-compatible metrics subsystem
// (AI.md PART 20). It is built on github.com/prometheus/client_golang and
// exposes the mandated "gitignore_"-prefixed metric set through a private
// registry so tests can construct isolated instances without colliding with
// the process-global default registry.
package metrics

import (
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// namespace is the mandated project prefix applied to every metric name
// (AI.md PART 20 "Metric Naming Conventions": prefix `{project_name}_`).
const namespace = "gitignore"

// defaultDurationBuckets mirrors the AI.md PART 20 duration histogram buckets.
var defaultDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// defaultSizeBuckets mirrors the AI.md PART 20 request/response size buckets.
var defaultSizeBuckets = []float64{100, 1000, 10000, 100000, 1000000, 10000000}

// Options configures a Metrics instance.
type Options struct {
	Version         string
	Commit          string
	BuildDate       string
	IncludeRuntime  bool
	IncludeSystem   bool
	DurationBuckets []float64
	SizeBuckets     []float64
	// TemplatesFn reports the current loaded-template count for the
	// gitignore_templates_total business gauge. Optional; nil omits the gauge.
	TemplatesFn        func() int
	DatabaseStatsFn    func() (open, inUse int)
	OwnerTokenCountsFn func() (primary, linked int)
	SystemFn           func() SystemSnapshot
}

// SystemSnapshot contains bounded system resource measurements for one scrape.
type SystemSnapshot struct {
	CPUPercent       float64
	MemoryPercent    float64
	MemoryUsedBytes  float64
	MemoryTotalBytes float64
	DiskPath         string
	DiskPercent      float64
	DiskUsedBytes    float64
	DiskTotalBytes   float64
}

// Metrics holds the registry and the HTTP metric vectors. Application, runtime,
// and business metrics are produced at scrape time by an internal collector so
// they always reflect current state.
type Metrics struct {
	reg *prometheus.Registry

	HTTPRequestsTotal      *prometheus.CounterVec
	HTTPRequestDuration    *prometheus.HistogramVec
	HTTPRequestSize        *prometheus.HistogramVec
	HTTPResponseSize       *prometheus.HistogramVec
	HTTPActiveRequests     prometheus.Gauge
	DBQueriesTotal         *prometheus.CounterVec
	DBQueryDuration        *prometheus.HistogramVec
	DBErrorsTotal          *prometheus.CounterVec
	AuthAttemptsTotal      *prometheus.CounterVec
	CacheHitsTotal         *prometheus.CounterVec
	CacheMissesTotal       *prometheus.CounterVec
	CacheEvictionsTotal    *prometheus.CounterVec
	CacheSize              *prometheus.GaugeVec
	CacheBytes             *prometheus.GaugeVec
	SchedulerTasksTotal    *prometheus.CounterVec
	SchedulerTaskDuration  *prometheus.HistogramVec
	SchedulerTasksRunning  *prometheus.GaugeVec
	SchedulerLastRun       *prometheus.GaugeVec
	RateLimitRequestsTotal *prometheus.CounterVec
	RateLimitBlockedTotal  *prometheus.CounterVec
	TorRequestsTotal       prometheus.Counter
	includeSystem          bool
	databaseStatsFn        func() (int, int)
	ownerTokenCountsFn     func() (int, int)
	systemFn               func() SystemSnapshot
}

// New builds a Metrics instance with its own registry and registers every
// mandated collector. It never panics on duplicate registration because the
// registry is private to this instance.
func New(opts Options) *Metrics {
	durationBuckets := opts.DurationBuckets
	if len(durationBuckets) == 0 {
		durationBuckets = defaultDurationBuckets
	}
	sizeBuckets := opts.SizeBuckets
	if len(sizeBuckets) == 0 {
		sizeBuckets = defaultSizeBuckets
	}

	m := &Metrics{
		reg:                prometheus.NewRegistry(),
		includeSystem:      opts.IncludeSystem,
		databaseStatsFn:    opts.DatabaseStatsFn,
		ownerTokenCountsFn: opts.OwnerTokenCountsFn,
		systemFn:           opts.SystemFn,
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests processed.",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds.",
				Buckets:   durationBuckets,
			},
			[]string{"method", "path"},
		),
		HTTPRequestSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_size_bytes",
				Help:      "HTTP request body size in bytes.",
				Buckets:   sizeBuckets,
			},
			[]string{"method", "path"},
		),
		HTTPResponseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_response_size_bytes",
				Help:      "HTTP response body size in bytes.",
				Buckets:   sizeBuckets,
			},
			[]string{"method", "path"},
		),
		HTTPActiveRequests: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "http_active_requests",
				Help:      "Number of HTTP requests currently being processed.",
			},
		),
		DBQueriesTotal:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "db_queries_total", Help: "Total database queries."}, []string{"operation", "table"}),
		DBQueryDuration:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "db_query_duration_seconds", Help: "Database query latency in seconds.", Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1}}, []string{"operation", "table"}),
		DBErrorsTotal:          prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "db_errors_total", Help: "Total database errors."}, []string{"operation", "error_type"}),
		AuthAttemptsTotal:      prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "auth_attempts_total", Help: "Total authentication attempts."}, []string{"method", "status"}),
		CacheHitsTotal:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "cache_hits_total", Help: "Total cache hits."}, []string{"cache"}),
		CacheMissesTotal:       prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "cache_misses_total", Help: "Total cache misses."}, []string{"cache"}),
		CacheEvictionsTotal:    prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "cache_evictions_total", Help: "Total cache evictions."}, []string{"cache"}),
		CacheSize:              prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "cache_size", Help: "Current number of cache items."}, []string{"cache"}),
		CacheBytes:             prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "cache_bytes", Help: "Current cache size in bytes."}, []string{"cache"}),
		SchedulerTasksTotal:    prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "scheduler_tasks_total", Help: "Total scheduled task executions."}, []string{"task", "status"}),
		SchedulerTaskDuration:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "scheduler_task_duration_seconds", Help: "Scheduled task duration in seconds.", Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 600}}, []string{"task"}),
		SchedulerTasksRunning:  prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "scheduler_tasks_running", Help: "Currently running scheduled tasks."}, []string{"task"}),
		SchedulerLastRun:       prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "scheduler_last_run_timestamp", Help: "Unix timestamp of the last scheduled task run."}, []string{"task"}),
		RateLimitRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "ratelimit_requests_total", Help: "Total requests evaluated by rate limits."}, []string{"limit", "status"}),
		RateLimitBlockedTotal:  prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "ratelimit_blocked_total", Help: "Total requests blocked by rate limits."}, []string{"limit"}),
		TorRequestsTotal:       prometheus.NewCounter(prometheus.CounterOpts{Namespace: namespace, Name: "tor_requests_total", Help: "Total requests via Tor hidden service."}),
	}

	m.reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.HTTPRequestSize,
		m.HTTPResponseSize,
		m.HTTPActiveRequests,
		m.DBQueriesTotal, m.DBQueryDuration, m.DBErrorsTotal,
		m.AuthAttemptsTotal,
		m.CacheHitsTotal, m.CacheMissesTotal, m.CacheEvictionsTotal, m.CacheSize, m.CacheBytes,
		m.SchedulerTasksTotal, m.SchedulerTaskDuration, m.SchedulerTasksRunning, m.SchedulerLastRun,
		m.RateLimitRequestsTotal, m.RateLimitBlockedTotal, m.TorRequestsTotal,
		newInfoCollector(opts),
		newOperationalCollector(m),
	)
	return m
}

type operationalCollector struct {
	metrics                                                                                                     *Metrics
	dbOpen, dbInUse, ownerTokens, cpu, memoryPercent, memoryUsed, memoryTotal, diskPercent, diskUsed, diskTotal *prometheus.Desc
}

func newOperationalCollector(m *Metrics) *operationalCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, labels, nil)
	}
	return &operationalCollector{metrics: m,
		dbOpen:        desc("db_connections_open", "Number of open database connections."),
		dbInUse:       desc("db_connections_in_use", "Number of database connections actively in use."),
		ownerTokens:   desc("owner_tokens_active", "Active owner tokens by kind.", "kind"),
		cpu:           desc("system_cpu_usage_percent", "Current system CPU usage percentage."),
		memoryPercent: desc("system_memory_usage_percent", "Current system memory usage percentage."),
		memoryUsed:    desc("system_memory_used_bytes", "System memory currently in use."),
		memoryTotal:   desc("system_memory_total_bytes", "Total system memory."),
		diskPercent:   desc("system_disk_usage_percent", "Disk usage percentage.", "path"),
		diskUsed:      desc("system_disk_used_bytes", "Disk space used.", "path"),
		diskTotal:     desc("system_disk_total_bytes", "Total disk space.", "path"),
	}
}

func (c *operationalCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.dbOpen, c.dbInUse, c.ownerTokens} {
		ch <- desc
	}
	if c.metrics.includeSystem {
		for _, desc := range []*prometheus.Desc{c.cpu, c.memoryPercent, c.memoryUsed, c.memoryTotal, c.diskPercent, c.diskUsed, c.diskTotal} {
			ch <- desc
		}
	}
}

func (c *operationalCollector) Collect(ch chan<- prometheus.Metric) {
	open, inUse := 0, 0
	if c.metrics.databaseStatsFn != nil {
		open, inUse = c.metrics.databaseStatsFn()
	}
	ch <- prometheus.MustNewConstMetric(c.dbOpen, prometheus.GaugeValue, float64(open))
	ch <- prometheus.MustNewConstMetric(c.dbInUse, prometheus.GaugeValue, float64(inUse))
	primary, linked := 0, 0
	if c.metrics.ownerTokenCountsFn != nil {
		primary, linked = c.metrics.ownerTokenCountsFn()
	}
	ch <- prometheus.MustNewConstMetric(c.ownerTokens, prometheus.GaugeValue, float64(primary), "primary")
	ch <- prometheus.MustNewConstMetric(c.ownerTokens, prometheus.GaugeValue, float64(linked), "linked")
	if !c.metrics.includeSystem {
		return
	}
	snapshot := SystemSnapshot{}
	if c.metrics.systemFn != nil {
		snapshot = c.metrics.systemFn()
	}
	ch <- prometheus.MustNewConstMetric(c.cpu, prometheus.GaugeValue, snapshot.CPUPercent)
	ch <- prometheus.MustNewConstMetric(c.memoryPercent, prometheus.GaugeValue, snapshot.MemoryPercent)
	ch <- prometheus.MustNewConstMetric(c.memoryUsed, prometheus.GaugeValue, snapshot.MemoryUsedBytes)
	ch <- prometheus.MustNewConstMetric(c.memoryTotal, prometheus.GaugeValue, snapshot.MemoryTotalBytes)
	if snapshot.DiskPath != "" {
		ch <- prometheus.MustNewConstMetric(c.diskPercent, prometheus.GaugeValue, snapshot.DiskPercent, snapshot.DiskPath)
		ch <- prometheus.MustNewConstMetric(c.diskUsed, prometheus.GaugeValue, snapshot.DiskUsedBytes, snapshot.DiskPath)
		ch <- prometheus.MustNewConstMetric(c.diskTotal, prometheus.GaugeValue, snapshot.DiskTotalBytes, snapshot.DiskPath)
	}
}

// Registry exposes the underlying registry for the promhttp handler.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.reg
}

// infoCollector produces application, runtime, and business metrics on demand
// at scrape time via prometheus.Collector. Computing them at scrape avoids a
// background goroutine and guarantees the reported uptime and memory figures
// are current.
type infoCollector struct {
	startTime      time.Time
	version        string
	commit         string
	buildDate      string
	includeRuntime bool
	templatesFn    func() int

	appInfo      *prometheus.Desc
	appUptime    *prometheus.Desc
	appStart     *prometheus.Desc
	templates    *prometheus.Desc
	goGoroutines *prometheus.Desc
	goMemAlloc   *prometheus.Desc
	goMemSys     *prometheus.Desc
	goGCRuns     *prometheus.Desc
	goGCPause    *prometheus.Desc
}

// newInfoCollector wires the static descriptors for the on-demand collector.
func newInfoCollector(opts Options) *infoCollector {
	return &infoCollector{
		startTime:      time.Now(),
		version:        opts.Version,
		commit:         opts.Commit,
		buildDate:      opts.BuildDate,
		includeRuntime: opts.IncludeRuntime,
		templatesFn:    opts.TemplatesFn,
		appInfo: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "app_info"),
			"Application build information; always 1, labels carry the values.",
			[]string{"version", "commit", "build_date", "go_version"}, nil,
		),
		appUptime: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "app_uptime_seconds"),
			"Seconds since the application started.", nil, nil,
		),
		appStart: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "app_start_timestamp"),
			"Unix timestamp when the application started.", nil, nil,
		),
		templates: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "templates_total"),
			"Number of loaded gitignore templates.", nil, nil,
		),
		goGoroutines: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "go_goroutines"),
			"Current number of goroutines.", nil, nil,
		),
		goMemAlloc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "go_mem_alloc_bytes"),
			"Bytes of allocated heap objects currently in use.", nil, nil,
		),
		goMemSys: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "go_mem_sys_bytes"),
			"Total bytes of memory obtained from the OS.", nil, nil,
		),
		goGCRuns: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "go_gc_runs_total"),
			"Total number of completed garbage collection cycles.", nil, nil,
		),
		goGCPause: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "go_gc_pause_total_seconds"),
			"Total time spent in garbage collection pauses, in seconds.", nil, nil,
		),
	}
}

// Describe implements prometheus.Collector.
func (c *infoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.appInfo
	ch <- c.appUptime
	ch <- c.appStart
	if c.templatesFn != nil {
		ch <- c.templates
	}
	if c.includeRuntime {
		ch <- c.goGoroutines
		ch <- c.goMemAlloc
		ch <- c.goMemSys
		ch <- c.goGCRuns
		ch <- c.goGCPause
	}
}

// Collect implements prometheus.Collector, emitting current values on scrape.
func (c *infoCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(c.appInfo, prometheus.GaugeValue, 1,
		c.version, c.commit, c.buildDate, runtime.Version())
	ch <- prometheus.MustNewConstMetric(c.appUptime, prometheus.GaugeValue,
		time.Since(c.startTime).Seconds())
	ch <- prometheus.MustNewConstMetric(c.appStart, prometheus.GaugeValue,
		float64(c.startTime.Unix()))

	if c.templatesFn != nil {
		ch <- prometheus.MustNewConstMetric(c.templates, prometheus.GaugeValue,
			float64(c.templatesFn()))
	}

	if c.includeRuntime {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		ch <- prometheus.MustNewConstMetric(c.goGoroutines, prometheus.GaugeValue,
			float64(runtime.NumGoroutine()))
		ch <- prometheus.MustNewConstMetric(c.goMemAlloc, prometheus.GaugeValue,
			float64(ms.Alloc))
		ch <- prometheus.MustNewConstMetric(c.goMemSys, prometheus.GaugeValue,
			float64(ms.Sys))
		ch <- prometheus.MustNewConstMetric(c.goGCRuns, prometheus.CounterValue,
			float64(ms.NumGC))
		ch <- prometheus.MustNewConstMetric(c.goGCPause, prometheus.CounterValue,
			float64(ms.PauseTotalNs)/1e9)
	}
}
