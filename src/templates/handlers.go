package templates

import (
	"encoding/json"
	"net/http"
	"strings"
)

// writeJSONError writes the unified JSON error envelope (AI.md PART 9/14).
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      false,
		"error":   code,
		"message": message,
	})
}

// HandleGetTemplate returns a specific template
func (m *Manager) HandleGetTemplate(w http.ResponseWriter, r *http.Request, name string) {
	tmpl, err := m.Get(name)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "template not found")
		return
	}

	// Content negotiation
	accept := r.Header.Get("Accept")
	userAgent := r.Header.Get("User-Agent")

	// Check if requesting JSON
	if strings.Contains(accept, "application/json") || strings.HasSuffix(r.URL.Path, ".json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"data":    tmpl,
		})
		return
	}

	// Check if browser (return HTML page via server handler)
	if strings.Contains(userAgent, "Mozilla") {
		// Let server handler deal with HTML rendering
		w.Header().Set("X-Template-Data", "available")
		return
	}

	// Default: return plain text content
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(tmpl.Content))
}

// HandleList returns list of all templates
func (m *Manager) HandleList(w http.ResponseWriter, r *http.Request) {
	templates := m.List()

	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"data":    templates,
			"count":   len(templates),
		})
		return
	}

	// Plain text list (comma-separated for CLI compatibility)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.Join(templates, ",")))
}

// HandleSearch searches templates
func (m *Manager) HandleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSONError(w, http.StatusBadRequest, "BAD_REQUEST", "query parameter 'q' is required")
		return
	}

	results := m.Search(query)

	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"data":    results,
			"count":   len(results),
			"query":   query,
		})
		return
	}

	// Plain text list
	names := make([]string, len(results))
	for i, tmpl := range results {
		names[i] = tmpl.Name
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.Join(names, "\n")))
}

// HandleCombine combines multiple templates
func (m *Manager) HandleCombine(w http.ResponseWriter, r *http.Request) {
	templatesParam := r.URL.Query().Get("templates")
	if templatesParam == "" {
		writeJSONError(w, http.StatusBadRequest, "BAD_REQUEST", "query parameter 'templates' is required")
		return
	}

	// Parse template names (comma-separated)
	names := strings.Split(templatesParam, ",")
	for i, name := range names {
		names[i] = strings.TrimSpace(name)
	}

	combined, err := m.Combine(names)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"data":      combined,
			"templates": names,
		})
		return
	}

	// Default: plain text
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(combined))
}

// HandleCategories returns all categories
func (m *Manager) HandleCategories(w http.ResponseWriter, r *http.Request) {
	categories := m.GetCategories()

	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"data":    categories,
			"count":   len(categories),
		})
		return
	}

	// Plain text list
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.Join(categories, "\n")))
}

// HandleCategoryTemplates returns templates in a category
func (m *Manager) HandleCategoryTemplates(w http.ResponseWriter, r *http.Request, category string) {
	templates := m.GetByCategory(category)

	if len(templates) == 0 {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "category not found")
		return
	}

	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"data":     templates,
			"count":    len(templates),
			"category": category,
		})
		return
	}

	// Plain text list of names
	names := make([]string, len(templates))
	for i, tmpl := range templates {
		names[i] = tmpl.Name
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.Join(names, "\n")))
}

// HandleStats returns template statistics
func (m *Manager) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats := m.Stats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"data":    stats,
	})
}
