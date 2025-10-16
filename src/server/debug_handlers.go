package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Debug handlers (dev mode only)

// handleDebugRoutes lists all registered routes
func (s *Server) handleDebugRoutes(w http.ResponseWriter, r *http.Request) {
	routes := []string{}
	chi.Walk(s.router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routes = append(routes, fmt.Sprintf("%s %s", method, route))
		return nil
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    routes,
		"count":   len(routes),
	})
}

// handleDebugConfig shows current configuration
func (s *Server) handleDebugConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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

// handleDebugDB shows database statistics
func (s *Server) handleDebugDB(w http.ResponseWriter, r *http.Request) {
	stats := s.config.Database.GetDB().Stats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"max_open_connections": stats.MaxOpenConnections,
			"open_connections":     stats.OpenConnections,
			"in_use":               stats.InUse,
			"idle":                 stats.Idle,
		},
	})
}

// handleDebugTemplates shows template statistics
func (s *Server) handleDebugTemplates(w http.ResponseWriter, r *http.Request) {
	stats := s.config.Templates.Stats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    stats,
	})
}

// handleDebugReset resets to fresh state (dev mode only)
func (s *Server) handleDebugReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Reset not implemented (would be dangerous in production)",
	})
}
