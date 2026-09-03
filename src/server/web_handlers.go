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
		s.renderPageStatus(w, r, "template", http.StatusNotFound, PageData{
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

// handleServerPage serves the /server index page.
func (s *Server) handleServerPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "server", PageData{Title: "Server"})
}

// handleAboutPage serves the About standard page (AI.md PART 16).
func (s *Server) handleAboutPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "about", PageData{Title: "About"})
}

// handlePrivacyPage serves the Privacy standard page.
func (s *Server) handlePrivacyPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "privacy", PageData{Title: "Privacy"})
}

// handleContactPage serves the Contact standard page.
func (s *Server) handleContactPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "contact", PageData{Title: "Contact"})
}

// handleHelpPage serves the Help standard page.
func (s *Server) handleHelpPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "help", PageData{Title: "Help"})
}

// handleTermsPage serves the Terms standard page.
func (s *Server) handleTermsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "terms", PageData{Title: "Terms"})
}

// handleThemeSet is the no-JS theme switch: it persists the chosen theme in the
// "theme" cookie and redirects back, so a visitor without JavaScript can still
// change theme via the <noscript> form (AI.md PART 16).
func (s *Server) handleThemeSet(w http.ResponseWriter, r *http.Request) {
	theme := r.FormValue("theme")
	if !validThemes[theme] {
		theme = "dark"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "theme",
		Value:    theme,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	dest := r.FormValue("return")
	if dest == "" || !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// renderErrorPage renders a themed error page (AI.md PART 16 "Error Pages").
// It never leaks stack traces; the message is a short, user-facing string.
func (s *Server) renderErrorPage(w http.ResponseWriter, r *http.Request, code int, message string) {
	s.renderPageStatus(w, r, "error", code, PageData{
		Title: http.StatusText(code),
		Data: map[string]interface{}{
			"code":    code,
			"status":  http.StatusText(code),
			"message": message,
		},
	})
}

// handleNotFound renders the themed 404 page for unmatched routes.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderErrorPage(w, r, http.StatusNotFound, "The page you requested could not be found.")
}

// handleMethodNotAllowed renders the themed 405 page.
func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.renderErrorPage(w, r, http.StatusMethodNotAllowed, "That method is not allowed on this resource.")
}

// handleGraphiQLPage serves the GraphiQL playground.
func (s *Server) handleGraphiQLPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setCacheHeaders(w, "html")
	_, _ = w.Write([]byte(graphiQLHTML))
}
