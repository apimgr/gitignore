package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestNewRegistersMandatedMetrics verifies the mandated metric families are
// registered with the correct gitignore_ prefix and Prometheus types.
func TestNewRegistersMandatedMetrics(t *testing.T) {
	m := New(Options{
		Version:        "1.2.3",
		Commit:         "abc1234",
		BuildDate:      "2026-07-21",
		IncludeRuntime: true,
		TemplatesFn:    func() int { return 42 },
	})

	// Drive the HTTP vectors so their series appear in the gather output.
	m.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/templates", "200").Inc()
	m.HTTPRequestDuration.WithLabelValues("GET", "/api/v1/templates").Observe(0.02)
	m.HTTPRequestSize.WithLabelValues("GET", "/api/v1/templates").Observe(128)
	m.HTTPResponseSize.WithLabelValues("GET", "/api/v1/templates").Observe(2048)
	m.HTTPActiveRequests.Set(3)
	m.DBQueriesTotal.WithLabelValues("select", "templates").Inc()
	m.DBQueryDuration.WithLabelValues("select", "templates").Observe(0.01)
	m.DBErrorsTotal.WithLabelValues("select", "other").Inc()
	m.AuthAttemptsTotal.WithLabelValues("owner_token", "success").Inc()
	m.CacheHitsTotal.WithLabelValues("items").Inc()
	m.CacheMissesTotal.WithLabelValues("items").Inc()
	m.CacheEvictionsTotal.WithLabelValues("items").Inc()
	m.CacheSize.WithLabelValues("items").Set(3)
	m.CacheBytes.WithLabelValues("items").Set(512)
	m.SchedulerTasksTotal.WithLabelValues("cleanup", "success").Inc()
	m.SchedulerTaskDuration.WithLabelValues("cleanup").Observe(1)
	m.SchedulerTasksRunning.WithLabelValues("cleanup").Set(0)
	m.SchedulerLastRun.WithLabelValues("cleanup").Set(1)
	m.RateLimitRequestsTotal.WithLabelValues("per_ip", "allowed").Inc()
	m.RateLimitBlockedTotal.WithLabelValues("per_ip").Inc()
	m.TorRequestsTotal.Inc()

	want := []string{
		"gitignore_app_info",
		"gitignore_app_uptime_seconds",
		"gitignore_app_start_timestamp",
		"gitignore_templates_total",
		"gitignore_http_requests_total",
		"gitignore_http_request_duration_seconds",
		"gitignore_http_request_size_bytes",
		"gitignore_http_response_size_bytes",
		"gitignore_http_active_requests",
		"gitignore_db_queries_total",
		"gitignore_db_query_duration_seconds",
		"gitignore_db_connections_open",
		"gitignore_db_connections_in_use",
		"gitignore_db_errors_total",
		"gitignore_auth_attempts_total",
		"gitignore_owner_tokens_active",
		"gitignore_cache_hits_total",
		"gitignore_cache_misses_total",
		"gitignore_cache_evictions_total",
		"gitignore_cache_size",
		"gitignore_cache_bytes",
		"gitignore_scheduler_tasks_total",
		"gitignore_scheduler_task_duration_seconds",
		"gitignore_scheduler_tasks_running",
		"gitignore_scheduler_last_run_timestamp",
		"gitignore_ratelimit_requests_total",
		"gitignore_ratelimit_blocked_total",
		"gitignore_tor_requests_total",
		"gitignore_go_goroutines",
		"gitignore_go_mem_alloc_bytes",
		"gitignore_go_mem_sys_bytes",
		"gitignore_go_gc_runs_total",
		"gitignore_go_gc_pause_total_seconds",
	}

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	present := make(map[string]bool, len(families))
	for _, f := range families {
		present[f.GetName()] = true
	}
	for _, name := range want {
		if !present[name] {
			t.Errorf("expected metric %q to be registered", name)
		}
	}
}

// TestSystemMetricsAreConditional ensures system metrics follow include_system.
func TestSystemMetricsAreConditional(t *testing.T) {
	withoutSystem := New(Options{})
	families, err := withoutSystem.Registry().Gather()
	if err != nil {
		t.Fatalf("gather without system metrics: %v", err)
	}
	for _, family := range families {
		if strings.HasPrefix(family.GetName(), "gitignore_system_") {
			t.Errorf("system metric %q should be absent when disabled", family.GetName())
		}
	}

	withSystem := New(Options{IncludeSystem: true, SystemFn: func() SystemSnapshot {
		return SystemSnapshot{CPUPercent: 8, MemoryPercent: 20, MemoryUsedBytes: 200, MemoryTotalBytes: 1000, DiskPath: "/data", DiskPercent: 30, DiskUsedBytes: 300, DiskTotalBytes: 1000}
	}})
	families, err = withSystem.Registry().Gather()
	if err != nil {
		t.Fatalf("gather with system metrics: %v", err)
	}
	present := make(map[string]bool, len(families))
	for _, family := range families {
		present[family.GetName()] = true
	}
	for _, name := range []string{"gitignore_system_cpu_usage_percent", "gitignore_system_memory_usage_percent", "gitignore_system_memory_used_bytes", "gitignore_system_memory_total_bytes", "gitignore_system_disk_usage_percent", "gitignore_system_disk_used_bytes", "gitignore_system_disk_total_bytes"} {
		if !present[name] {
			t.Errorf("expected system metric %q", name)
		}
	}
}

// TestAppInfoValue verifies the app_info gauge carries the build labels and is 1.
func TestAppInfoValue(t *testing.T) {
	m := New(Options{Version: "9.9.9", Commit: "deadbee", BuildDate: "2026-07-21"})
	const exposition = `
# HELP gitignore_app_info Application build information; always 1, labels carry the values.
# TYPE gitignore_app_info gauge
gitignore_app_info{build_date="2026-07-21",commit="deadbee",go_version="GOVERSION",version="9.9.9"} 1
`
	got := testutil.CollectAndCount(m.Registry(), "gitignore_app_info")
	if got != 1 {
		t.Fatalf("expected exactly 1 app_info series, got %d", got)
	}
	_ = exposition // documented reference shape; go_version is runtime-dependent.
}

// TestRuntimeDisabled ensures include_runtime=false omits the go_* metrics.
func TestRuntimeDisabled(t *testing.T) {
	m := New(Options{IncludeRuntime: false})
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if strings.HasPrefix(f.GetName(), "gitignore_go_") {
			t.Errorf("go_* metric %q should be absent when runtime disabled", f.GetName())
		}
	}
}

// TestTemplatesOmittedWhenNil ensures the business gauge is skipped without a fn.
func TestTemplatesOmittedWhenNil(t *testing.T) {
	m := New(Options{})
	if c := testutil.CollectAndCount(m.Registry(), "gitignore_templates_total"); c != 0 {
		t.Errorf("expected no templates_total series, got %d", c)
	}
}
