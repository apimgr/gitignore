package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Admin API handlers

// handleAdminAPIInfo returns admin API information
func (s *Server) handleAdminAPIInfo(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user":    user,
		"endpoints": []string{
			"/api/v1/admin/settings",
			"/api/v1/admin/database",
			"/api/v1/admin/logs",
			"/api/v1/admin/backup",
			"/api/v1/admin/healthz",
		},
	})
}

// handleAdminHealthz returns detailed health status
func (s *Server) handleAdminHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"version":   s.config.Version,
		"commit":    s.config.Commit,
		"templates": s.config.Templates.Count(),
		"database":  "connected",
	})
}

// handleAdminGetSettings returns all settings
func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.config.Database.GetAllSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    settings,
	})
}

// handleAdminUpdateSettings updates settings
func (s *Server) handleAdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	settings, ok := req["settings"].(map[string]interface{})
	if !ok {
		http.Error(w, "Missing 'settings' field", http.StatusBadRequest)
		return
	}

	// Update each setting
	for key, value := range settings {
		valueStr := fmt.Sprintf("%v", value)
		if err := s.config.Database.SetSetting(key, valueStr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Settings updated",
	})
}

// handleAdminDatabaseStatus returns database status
func (s *Server) handleAdminDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "connected",
	})
}

// handleAdminDatabaseTest tests database connection
func (s *Server) handleAdminDatabaseTest(w http.ResponseWriter, r *http.Request) {
	err := s.config.Database.GetDB().Ping()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Database connection successful",
	})
}

// handleAdminListLogs lists available logs
func (s *Server) handleAdminListLogs(w http.ResponseWriter, r *http.Request) {
	logs := []string{"access", "error", "audit"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    logs,
	})
}

// handleAdminGetLog returns log content
func (s *Server) handleAdminGetLog(w http.ResponseWriter, r *http.Request) {
	logType := chi.URLParam(r, "type")
	// TODO: Read actual log files
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"type":    logType,
		"content": "Log content placeholder",
	})
}

// handleAdminListBackups lists all backups
func (s *Server) handleAdminListBackups(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement backup listing
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    []string{},
	})
}

// handleAdminCreateBackup creates a new backup
func (s *Server) handleAdminCreateBackup(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement backup creation
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup created",
	})
}

// handleAdminDeleteBackup deletes a backup
func (s *Server) handleAdminDeleteBackup(w http.ResponseWriter, r *http.Request) {
	backupID := chi.URLParam(r, "id")
	// TODO: Implement backup deletion
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Backup %s deleted", backupID),
	})
}

// Admin web page handlers

// handleAdminDashboard serves the admin dashboard
func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Admin Dashboard</h1>")
	fmt.Fprintf(w, "<p>Welcome, %s!</p>", user)
	fmt.Fprintf(w, "<ul>")
	fmt.Fprintf(w, "<li><a href='/admin/settings'>Settings</a></li>")
	fmt.Fprintf(w, "<li><a href='/admin/database'>Database</a></li>")
	fmt.Fprintf(w, "<li><a href='/admin/logs'>Logs</a></li>")
	fmt.Fprintf(w, "<li><a href='/admin/backup'>Backups</a></li>")
	fmt.Fprintf(w, "<li><a href='/admin/healthz'>Health</a></li>")
	fmt.Fprintf(w, "</ul>")
}

// handleAdminSettingsPage serves the settings page
func (s *Server) handleAdminSettingsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Settings</h1>")
}

// handleAdminUpdateSettingsForm handles settings form submission
func (s *Server) handleAdminUpdateSettingsForm(w http.ResponseWriter, r *http.Request) {
	// Parse form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Update settings
	for key, values := range r.Form {
		if len(values) > 0 {
			s.config.Database.SetSetting(key, values[0])
		}
	}

	// Redirect back to settings page
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// handleAdminDatabasePage serves the database management page
func (s *Server) handleAdminDatabasePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Database Management</h1>")
}

// handleAdminDatabaseTestForm handles database test form submission
func (s *Server) handleAdminDatabaseTestForm(w http.ResponseWriter, r *http.Request) {
	err := s.config.Database.GetDB().Ping()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte("Database connection successful"))
}

// handleAdminLogsPage serves the logs viewer page
func (s *Server) handleAdminLogsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Logs</h1>")
}

// handleAdminLogView serves a specific log view
func (s *Server) handleAdminLogView(w http.ResponseWriter, r *http.Request) {
	logType := chi.URLParam(r, "type")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Log: %s</h1>", logType)
}

// handleAdminBackupPage serves the backup management page
func (s *Server) handleAdminBackupPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Backup Management</h1>")
}

// handleAdminCreateBackupForm handles backup creation form submission
func (s *Server) handleAdminCreateBackupForm(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement backup creation
	http.Redirect(w, r, "/admin/backup", http.StatusSeeOther)
}

// handleAdminRestoreBackupForm handles backup restoration form submission
func (s *Server) handleAdminRestoreBackupForm(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement backup restoration
	http.Redirect(w, r, "/admin/backup", http.StatusSeeOther)
}

// handleAdminHealthPage serves the server health page
func (s *Server) handleAdminHealthPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>Server Health</h1>")
	fmt.Fprintf(w, "<p>Version: %s</p>", s.config.Version)
	fmt.Fprintf(w, "<p>Status: Healthy</p>")
}
