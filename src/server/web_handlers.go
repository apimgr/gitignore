package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Web page handlers render server-side HTML templates (AI.md PART 16).

// handleSearchPage serves the search page.
func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	data := map[string]interface{}{"query": query}
	if query != "" {
		data["results"] = s.config.Templates.Search(query)
	}
	s.renderPage(w, r, "search", PageData{Title: "Search", Data: data})
}

// handleTemplatePage serves the template detail page.
func (s *Server) handleTemplatePage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	tmpl, err := s.config.Templates.Get(name)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		s.renderPage(w, r, "template", PageData{
			Title: "Not found",
			Data:  map[string]interface{}{"name": name, "content": "template not found"},
		})
		return
	}
	s.renderPage(w, r, "template", PageData{
		Title: tmpl.Name,
		Data:  map[string]interface{}{"name": tmpl.Name, "content": tmpl.Content},
	})
}

// handleCombinePage serves the combine templates page.
func (s *Server) handleCombinePage(w http.ResponseWriter, r *http.Request) {
	param := r.URL.Query().Get("templates")
	data := map[string]interface{}{"templates": param}
	if param != "" {
		names := strings.Split(param, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		combined, err := s.config.Templates.Combine(names)
		if err != nil {
			data["error"] = err.Error()
		} else {
			data["content"] = combined
		}
	}
	s.renderPage(w, r, "combine", PageData{Title: "Combine", Data: data})
}

// handleCategoriesPage serves the categories page.
func (s *Server) handleCategoriesPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "categories", PageData{
		Title: "Categories",
		Data:  map[string]interface{}{"categories": s.config.Templates.GetCategories()},
	})
}

// handleListPage serves the list-all-templates page.
func (s *Server) handleListPage(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	var names []string
	if category != "" {
		for _, t := range s.config.Templates.GetByCategory(category) {
			names = append(names, t.Name)
		}
	} else {
		names = s.config.Templates.List()
	}
	s.renderPage(w, r, "list", PageData{
		Title: "All Templates",
		Data:  map[string]interface{}{"templates": names},
	})
}

// handleStatsPage serves the statistics page.
func (s *Server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "stats", PageData{Title: "Statistics", Data: s.config.Templates.Stats()})
}

// handleDocsPage serves the API documentation page.
func (s *Server) handleDocsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "docs", PageData{Title: "API Documentation"})
}

// handleCLIPage serves the CLI customization page.
func (s *Server) handleCLIPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "cli", PageData{Title: "CLI"})
}

// handleGraphiQLPage serves the GraphiQL playground.
func (s *Server) handleGraphiQLPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setCacheHeaders(w, "html")
	_, _ = w.Write([]byte(graphiQLHTML))
}
