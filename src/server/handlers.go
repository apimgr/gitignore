package server

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleHome serves the home page
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "home", PageData{
		Title: "Home",
		Data: map[string]interface{}{
			"total":      s.config.Templates.Count(),
			"categories": len(s.config.Templates.GetCategories()),
		},
	})
}

// handleHealthz handles the content-negotiated health check (AI.md PART 13).
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ".txt") {
		s.handleHealthzText(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	setCacheHeaders(w, "html")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"version":   s.config.Version,
		"commit":    s.config.Commit,
		"buildDate": s.config.BuildDate,
	})
}

// handleHealthzText handles health check (plain text)
func (s *Server) handleHealthzText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "healthy\n")
}

// handleAPIInfo returns API information
func (s *Server) handleAPIInfo(w http.ResponseWriter, r *http.Request) {
	base := apiBasePath()
	sendAPIResponseOK(w, map[string]interface{}{
		"name":      "GitIgnore API",
		"version":   s.config.Version,
		"commit":    s.config.Commit,
		"buildDate": s.config.BuildDate,
		"endpoints": map[string]string{
			"health":       base + "/server/healthz",
			"list":         base + "/list",
			"search":       base + "/search?q={query}",
			"template":     base + "/templates/{name}",
			"combine":      base + "/combine?templates={name1,name2}",
			"categories":   base + "/categories",
			"stats":        base + "/stats",
			"swagger":      base + "/server/swagger",
			"graphql":      base + "/server/graphql",
			"autodiscover": "/api/autodiscover",
		},
	})
}

// handleAPIAutodiscover returns machine-readable server metadata for
// zero-config client discovery (AI.md PART 14 "/api/autodiscover"). CLI
// self-update version feeds are not implemented (see TODO.AI.md), so
// "cli_versions" is intentionally omitted rather than faked.
func (s *Server) handleAPIAutodiscover(w http.ResponseWriter, r *http.Request) {
	sendAPIResponseOK(w, map[string]interface{}{
		"name":        "GitIgnore API",
		"version":     s.config.Version,
		"commit":      s.config.Commit,
		"buildDate":   s.config.BuildDate,
		"api_version": apiVersion,
		"api_base":    apiBasePath(),
		"swagger":     "/api/swagger",
		"graphql":     "/api/graphql",
		"healthz":     "/api/healthz",
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
	setCacheHeaders(w, "api")
	json.NewEncoder(w).Encode(APIResponse{
		OK:   true,
		Data: templates,
		Meta: map[string]interface{}{"count": len(templates)},
	})
}

// handleAPITemplatesTarGz streams every template as a gzip-compressed tar
// archive (AI.md PART 14).
func (s *Server) handleAPITemplatesTarGz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="gitignore-templates.tar.gz"`)
	setCacheHeaders(w, "api")

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, tmpl := range s.config.Templates.ListAll() {
		content := []byte(tmpl.Content)
		hdr := &tar.Header{
			Name:    tmpl.Name + ".gitignore",
			Mode:    0o644,
			Size:    int64(len(content)),
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return
		}
		if _, err := tw.Write(content); err != nil {
			return
		}
	}
}

// handleSwaggerUI serves a Swagger UI page bound to the OpenAPI JSON endpoint.
func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setCacheHeaders(w, "html")
	fmt.Fprintf(w, swaggerUIHTML, apiBasePath())
}

// handleOpenAPIJSON returns the generated OpenAPI 3.0 specification as JSON.
func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setCacheHeaders(w, "api")
	json.NewEncoder(w).Encode(s.openAPISpec(r))
}

// handleGraphQL handles GraphQL queries. A full GraphQL engine is not bundled;
// the endpoint responds with the spec-compliant NOT_IMPLEMENTED envelope so
// clients receive a structured, machine-readable error.
func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	sendAPIResponseError(w, "NOT_IMPLEMENTED", "GraphQL endpoint is not enabled on this server")
}

// handleGraphQLSchema returns the GraphQL SDL schema for the API.
func (s *Server) handleGraphQLSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	setCacheHeaders(w, "api")
	_, _ = w.Write([]byte(graphQLSchema))
}

// handleStatic serves embedded static assets under /static/.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	setCacheHeaders(w, "static")
	http.StripPrefix("/static/", http.FileServer(staticHTTPFS)).ServeHTTP(w, r)
}

// handleFavicon serves the embedded favicon.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	setCacheHeaders(w, "static")
	f, err := staticHTTPFS.Open("favicon.ico")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/x-icon")
	_, _ = io.Copy(w, f)
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
		sendAPIResponseError(w, "NOT_FOUND", "template not found")
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
		sendAPIResponseError(w, "BAD_REQUEST", "query parameter 'q' is required")
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
