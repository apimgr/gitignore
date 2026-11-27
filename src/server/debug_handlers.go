package server

import (
	"fmt"
	"net/http"
	"runtime"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/db"
	"github.com/go-chi/chi/v5"
)

// Debug handlers require the operator token and are mounted only when debug is enabled.

// handleDebugRoutes lists all registered routes
func (s *Server) handleDebugRoutes(w http.ResponseWriter, r *http.Request) {
	routes := []string{}
	if err := chi.Walk(s.router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routes = append(routes, fmt.Sprintf("%s %s", method, route))
		return nil
	}); err != nil {
		http.Error(w, i18n.T(r, "errors.route_enumeration_failed"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, map[string]interface{}{
		"success": true,
		"data":    routes,
		"count":   len(routes),
	})
}

// handleDebugConfig shows current configuration
func (s *Server) handleDebugConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"version":    s.config.Version,
			"commit":     s.config.Commit,
			"build_date": s.config.BuildDate,
			"dev_mode":   s.config.DevMode,
			"address":    s.config.Address,
			"port":       s.config.Port,
			"config_dir": s.config.Paths.GetConfigDir(),
			"data_dir":   s.config.Paths.GetDataDir(),
			"logs_dir":   s.config.Paths.GetLogsDir(),
		},
	})
}

// handleDebugTemplates shows template statistics
func (s *Server) handleDebugTemplates(w http.ResponseWriter, r *http.Request) {
	stats := s.config.Templates.Stats()

	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, map[string]interface{}{
		"success": true,
		"data":    stats,
	})
}

func (s *Server) handleDebugCache(w http.ResponseWriter, _ *http.Request) {
	s.healthCoreMu.RLock()
	status := s.healthCore.Cache
	s.healthCoreMu.RUnlock()
	cacheStats := map[string]any{}
	if s.metrics != nil {
		s.refreshCacheMetrics()
		cacheStats = s.metrics.CacheStats()
	}
	writeDebugJSON(w, http.StatusOK, map[string]any{
		"status":  status,
		"metrics": cacheStats,
	})
}

func (s *Server) handleDebugDB(w http.ResponseWriter, _ *http.Request) {
	stats := db.ConnectionPoolStats()
	writeDebugJSON(w, http.StatusOK, map[string]any{
		"open_connections":    stats.OpenConnections,
		"in_use":              stats.InUse,
		"idle":                stats.Idle,
		"wait_count":          stats.WaitCount,
		"wait_duration_ms":    stats.WaitDuration.Milliseconds(),
		"max_idle_closed":     stats.MaxIdleClosed,
		"max_lifetime_closed": stats.MaxLifetimeClosed,
	})
}

func (s *Server) handleDebugScheduler(w http.ResponseWriter, r *http.Request) {
	states, err := db.LoadSchedulerStates()
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "scheduler_error")
		return
	}
	tasks := make([]map[string]any, 0, len(states))
	for _, state := range states {
		tasks = append(tasks, map[string]any{
			"id": state.TaskID, "name": state.TaskName, "schedule": state.Schedule,
			"enabled": state.Enabled, "last_status": state.LastStatus,
			"last_error": state.LastError, "run_count": state.RunCount,
			"fail_count": state.FailCount,
		})
	}
	writeDebugJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "count": len(tasks)})
}

func (s *Server) handleDebugMemory(w http.ResponseWriter, _ *http.Request) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	writeDebugJSON(w, http.StatusOK, map[string]any{
		"alloc_mb":       memory.Alloc / 1024 / 1024,
		"total_alloc_mb": memory.TotalAlloc / 1024 / 1024,
		"sys_mb":         memory.Sys / 1024 / 1024,
		"num_gc":         memory.NumGC,
		"heap_objects":   memory.HeapObjects,
		"goroutines":     runtime.NumGoroutine(),
	})
}

func (s *Server) handleDebugGoroutines(w http.ResponseWriter, _ *http.Request) {
	const stackLimit = 1024 * 1024
	stack := make([]byte, stackLimit)
	stackSize := runtime.Stack(stack, true)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stack[:stackSize])
}

func (s *Server) handleDebugError500(w http.ResponseWriter, r *http.Request) {
	s.renderErrorPage(w, r, http.StatusInternalServerError, requestTranslation(r, "web.handler.error.http_internal_server_error"))
}

func writeDebugJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = encodeJSON(w, value)
}
