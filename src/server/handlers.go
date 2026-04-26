package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleHome serves the home page
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	// TODO: Render HTML template
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>GitIgnore API Server v%s</h1>", s.config.Version)
	fmt.Fprintf(w, "<p>API: <a href='/api/v1'>/api/v1</a></p>")
	fmt.Fprintf(w, "<p>Admin: <a href='/admin'>/admin</a></p>")
}

// handleHealthz handles health check
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"version": s.config.Version,
		"commit":  s.config.Commit,
	})
}

// handleHealthzText handles health check (plain text)
func (s *Server) handleHealthzText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "healthy\n")
}

// handleAPIInfo returns API information
func (s *Server) handleAPIInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":      "GitIgnore API",
		"version":   s.config.Version,
		"commit":    s.config.Commit,
		"buildDate": s.config.BuildDate,
		"endpoints": map[string]string{
			"health":     "/api/v1/healthz",
			"list":       "/api/v1/list",
			"search":     "/api/v1/search?q={query}",
			"template":   "/api/v1/template/{name}",
			"combine":    "/api/v1/combine?templates={name1,name2}",
			"categories": "/api/v1/categories",
			"stats":      "/api/v1/stats",
			"docs":       "/api/v1/docs",
			"graphql":    "/api/v1/graphql",
		},
	})
}

// handleAPITemplate returns a template's content
func (s *Server) handleAPITemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.config.Templates.HandleGetTemplate(w, r, name)
}

// handleAPITemplateJSON returns a template's metadata as JSON
func (s *Server) handleAPITemplateJSON(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	r.Header.Set("Accept", "application/json")
	s.config.Templates.HandleGetTemplate(w, r, name)
}

// handleAPIList returns list of all templates
func (s *Server) handleAPIList(w http.ResponseWriter, r *http.Request) {
	s.config.Templates.HandleList(w, r)
}

// handleAPISearch searches templates
func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	s.config.Templates.HandleSearch(w, r)
}

// handleAPICombine combines multiple templates
func (s *Server) handleAPICombine(w http.ResponseWriter, r *http.Request) {
	s.config.Templates.HandleCombine(w, r)
}

// handleAPICategories returns all categories
func (s *Server) handleAPICategories(w http.ResponseWriter, r *http.Request) {
	s.config.Templates.HandleCategories(w, r)
}

// handleAPICategoryTemplates returns templates in a category
func (s *Server) handleAPICategoryTemplates(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "name")
	s.config.Templates.HandleCategoryTemplates(w, r, category)
}

// handleAPIStats returns template statistics
func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	s.config.Templates.HandleStats(w, r)
}

// handleAPITemplatesJSON returns all templates as JSON
func (s *Server) handleAPITemplatesJSON(w http.ResponseWriter, r *http.Request) {
	templates := s.config.Templates.ListAll()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    templates,
		"count":   len(templates),
	})
}

// handleAPITemplatesTarGz returns all templates as tar.gz
func (s *Server) handleAPITemplatesTarGz(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement tar.gz export
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleSwaggerUI serves Swagger UI
func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement Swagger UI
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleOpenAPIJSON returns OpenAPI spec as JSON
func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement OpenAPI spec generation
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleOpenAPIYAML returns OpenAPI spec as YAML
func (s *Server) handleOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement OpenAPI spec generation
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleGraphQL handles GraphQL queries
func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement GraphQL
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleGraphQLSchema returns GraphQL schema
func (s *Server) handleGraphQLSchema(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement GraphQL schema
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleStatic serves static files
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// TODO: Serve embedded static files
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleFavicon serves favicon
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	// TODO: Serve embedded favicon
	http.Error(w, "Not implemented yet", http.StatusNotImplemented)
}

// handleRobotsTxt serves robots.txt from config
func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "User-agent: *")
	if s.config.Cfg != nil {
		for _, path := range s.config.Cfg.WebRobots.Allow {
			fmt.Fprintf(w, "Allow: %s\n", path)
		}
		for _, path := range s.config.Cfg.WebRobots.Deny {
			fmt.Fprintf(w, "Disallow: %s\n", path)
		}
	} else {
		fmt.Fprintln(w, "Allow: /")
		fmt.Fprintln(w, "Allow: /api")
		fmt.Fprintln(w, "Disallow: /debug")
	}
}

// handleSecurityTxt serves security.txt
func (s *Server) handleSecurityTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	admin := "security@apimgr.us"
	if s.config.Cfg != nil && s.config.Cfg.WebSecurity.Admin != "" {
		admin = s.config.Cfg.WebSecurity.Admin
	}
	fmt.Fprintf(w, "Contact: mailto:%s\n", admin)
	fmt.Fprintln(w, "Expires: 2026-12-31T23:59:59.000Z")
	fmt.Fprintln(w, "Preferred-Languages: en")
	fmt.Fprintln(w, "Canonical: https://gitignore.apimgr.us/.well-known/security.txt")
}

// handleManifest serves PWA manifest
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	manifest := `{
  "name": "GitIgnore API",
  "short_name": "GitIgnore",
  "description": "Comprehensive .gitignore template API",
  "start_url": "/",
  "display": "standalone",
  "background_color": "#1a1a1a",
  "theme_color": "#0066cc",
  "icons": [
    {
      "src": "/static/images/icon-192.png",
      "sizes": "192x192",
      "type": "image/png"
    },
    {
      "src": "/static/images/icon-512.png",
      "sizes": "512x512",
      "type": "image/png"
    }
  ]
}`
	fmt.Fprint(w, manifest)
}

// handleServiceWorker serves the service worker
func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	sw := `// GitIgnore Service Worker
const CACHE_NAME = 'gitignore-v1';
const urlsToCache = ['/', '/static/css/main.css', '/manifest.json'];

self.addEventListener('install', function(event) {
  event.waitUntil(
    caches.open(CACHE_NAME).then(function(cache) {
      return cache.addAll(urlsToCache);
    })
  );
});

self.addEventListener('fetch', function(event) {
  event.respondWith(
    caches.match(event.request).then(function(response) {
      return response || fetch(event.request);
    })
  );
});
`
	fmt.Fprint(w, sw)
}

// handleAPITemplateText returns a template's content as plain text
func (s *Server) handleAPITemplateText(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	// Remove .txt extension if present
	if len(name) > 4 && name[len(name)-4:] == ".txt" {
		name = name[:len(name)-4]
	}
	tmpl, err := s.config.Templates.Get(name)
	if err != nil {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, tmpl.Content)
}

// handleAPIListText returns list of all templates as text
func (s *Server) handleAPIListText(w http.ResponseWriter, r *http.Request) {
	templates := s.config.Templates.List()
	w.Header().Set("Content-Type", "text/plain")
	for _, name := range templates {
		fmt.Fprintln(w, name)
	}
}

// handleAPISearchText searches templates (text output)
func (s *Server) handleAPISearchText(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query parameter 'q' is required", http.StatusBadRequest)
		return
	}
	results := s.config.Templates.Search(query)
	w.Header().Set("Content-Type", "text/plain")
	for _, tmpl := range results {
		fmt.Fprintln(w, tmpl.Name)
	}
}

// handleAPICombineText combines multiple templates (text output)
func (s *Server) handleAPICombineText(w http.ResponseWriter, r *http.Request) {
	s.handleAPICombine(w, r)
}

// handleAPICategoriesText returns all categories as text
func (s *Server) handleAPICategoriesText(w http.ResponseWriter, r *http.Request) {
	categories := s.config.Templates.GetCategories()
	w.Header().Set("Content-Type", "text/plain")
	for _, cat := range categories {
		fmt.Fprintln(w, cat)
	}
}

// handleAPICategoryTemplatesText returns templates in a category as text
func (s *Server) handleAPICategoryTemplatesText(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "name")
	templates := s.config.Templates.GetByCategory(category)
	w.Header().Set("Content-Type", "text/plain")
	for _, tmpl := range templates {
		fmt.Fprintln(w, tmpl.Name)
	}
}

// handleAPIStatsText returns template statistics as text
func (s *Server) handleAPIStatsText(w http.ResponseWriter, r *http.Request) {
	stats := s.config.Templates.Stats()
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "Total Templates: %d\n", stats["total_templates"])
	fmt.Fprintf(w, "Categories: %d\n", stats["categories"])
	fmt.Fprintf(w, "Total Size: %d bytes\n", stats["total_size_bytes"])
}
