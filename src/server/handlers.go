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

	"github.com/apimgr/gitignore/src/common/i18n"
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

// handleAPIInfo returns API information
func (s *Server) handleAPIInfo(w http.ResponseWriter, r *http.Request) {
	base := s.apiBasePath()
	sendAPIResponseOK(w, map[string]interface{}{
		"name":        "GitIgnore API",
		"version":     s.config.Version,
		"api_version": s.apiVersion(),
		"api_base":    base,
		"commit":      s.config.Commit,
		"buildDate":   s.config.BuildDate,
		"endpoints": map[string]string{
			"health":           base + "/server/healthz",
			"list":             base + "/list",
			"search":           base + "/search?q={query}",
			"template":         base + "/templates/{name}",
			"combine":          base + "/combine?templates={name1,name2}",
			"categories":       base + "/categories",
			"stats":            base + "/stats",
			"swagger":          base + "/server/swagger",
			"graphql":          base + "/server/graphql",
			"mcp":              base + "/server/mcp",
			"mcp_help":         base + "/server/help/mcp",
			"about":            base + "/server/about",
			"privacy":          base + "/server/privacy",
			"help":             base + "/server/help",
			"terms":            base + "/server/terms",
			"preferences":      base + "/server/preferences",
			"preferences_sync": base + "/server/preferences/sync",
			"autodiscover":     "/api/autodiscover",
		},
	})
}

// handleAPIAutodiscover returns machine-readable server metadata for
// zero-config client discovery (AI.md PART 14 "/api/autodiscover"). CLI
// self-update version feeds are unavailable, so "cli_versions" is omitted
// rather than returning a fabricated value.
func (s *Server) handleAPIAutodiscover(w http.ResponseWriter, r *http.Request) {
	sendAPIResponseOK(w, map[string]interface{}{
		"name":          "GitIgnore API",
		"version":       s.config.Version,
		"commit":        s.config.Commit,
		"buildDate":     s.config.BuildDate,
		"api_version":   s.apiVersion(),
		"api_base":      s.apiBasePath(),
		"swagger":       "/api/swagger",
		"graphql":       s.apiBasePath() + "/server/graphql",
		"graphql_alias": "/api/graphql",
		"mcp":           s.apiBasePath() + "/server/mcp",
		"mcp_aliases":   []string{"/api/mcp", "/mcp"},
		"healthz":       "/api/healthz",
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
	_ = json.NewEncoder(w).Encode(APIResponse{
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
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

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
	data := PageData{
		Title:   i18n.Translate(i18n.LangFromContext(r.Context()), "nav.api_docs"),
		PageCSS: []string{s.requestPath(r, "/static/vendor/swagger/swagger-ui.css")},
		PageScripts: []string{
			s.requestPath(r, "/static/vendor/swagger/swagger-ui-bundle.js"),
			s.requestPath(r, "/static/vendor/swagger/swagger-init.js"),
		},
		Data: map[string]interface{}{"specURL": s.requestPath(r, s.apiBasePath()+"/server/swagger")},
	}
	s.renderPage(w, r, "swagger_ui", data)
}

// handleOpenAPIJSON returns the generated OpenAPI 3.0 specification as JSON.
func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setCacheHeaders(w, "api")
	_ = json.NewEncoder(w).Encode(s.openAPISpec(r))
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
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "image/x-icon")
	_, _ = io.Copy(w, f)
}

// handleRobotsTxt serves robots.txt from config
func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprintln(w, "User-agent: *")
	if s.currentConfig() != nil {
		for _, path := range s.currentConfig().WebRobots.Allow {
			_, _ = fmt.Fprintf(w, "Allow: %s\n", path)
		}
		for _, path := range s.currentConfig().WebRobots.Deny {
			_, _ = fmt.Fprintf(w, "Disallow: %s\n", path)
		}
	} else {
		_, _ = fmt.Fprintln(w, "Allow: /")
		_, _ = fmt.Fprintln(w, "Allow: /api")
		_, _ = fmt.Fprintln(w, "Disallow: /debug")
	}
}

// handleSecurityTxt serves security.txt
func (s *Server) handleSecurityTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	serverURL := s.publicServerURL(r)
	reportURL := "https://github.com/apimgr/gitignore/security/advisories/new"
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.ReportURL) != "" {
		reportURL = strings.TrimSpace(s.currentConfig().Web.Security.ReportURL)
	}
	_, _ = fmt.Fprintf(w, "Contact: %s\n", reportURL)
	if id, err := securityID(time.Now()); err == nil {
		_, _ = fmt.Fprintf(w, "Contact: %s\n", securityReportURL(serverURL, id))
	}
	email := s.securityContactEmail()
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Contact) != "" {
		email = strings.TrimSpace(s.currentConfig().Web.Security.Contact)
	}
	if email != "" {
		_, _ = fmt.Fprintf(w, "Contact: mailto:%s\n", email)
	}
	_, _ = fmt.Fprintf(w, "Policy: %s/server/security\n", serverURL)
	expires := time.Now().UTC().AddDate(1, 0, 0)
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Expires) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(s.currentConfig().Web.Security.Expires)); err == nil {
			expires = parsed.UTC()
		}
	}
	_, _ = fmt.Fprintf(w, "Expires: %s\n", expires.Format(time.RFC3339))
	if metadata, ok := s.visiblePGPMetadata(); ok {
		_, _ = fmt.Fprintf(w, "Encryption: %s/.well-known/pgp-key.asc\n", serverURL)
		_, _ = fmt.Fprintf(w, "# PGP-Fingerprint: %s\n", metadata.Fingerprint)
	}
	_, _ = fmt.Fprintln(w, "Preferred-Languages: en")
	_, _ = fmt.Fprintf(w, "Canonical: %s/.well-known/security.txt\n", serverURL)
}

// handleManifest serves PWA manifest
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	manifest := `{
  "name": "GitIgnore API",
  "short_name": "GitIgnore",
  "description": "Comprehensive .gitignore template API",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#1a1a1a",
  "theme_color": "#1a1a1a",
  "icons": [
    {
      "src": "/static/images/icon.svg",
      "sizes": "any",
      "type": "image/svg+xml",
      "purpose": "any maskable"
    }
  ]
}`
	_, _ = fmt.Fprint(w, manifest)
}

// handleServiceWorker serves the service worker
func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	sw := `// GitIgnore Service Worker
const CACHE_NAME = 'gitignore-v2';
const OFFLINE_URL = '/static/offline.html';
const urlsToCache = [
  '/',
  '/static/css/main.css',
  '/static/js/app.js',
  '/static/images/icon.svg',
  '/manifest.json',
  OFFLINE_URL
];

self.addEventListener('install', function(event) {
  event.waitUntil(
    caches.open(CACHE_NAME).then(function(cache) {
      return cache.addAll(urlsToCache);
    })
  );
  self.skipWaiting();
});

self.addEventListener('activate', function(event) {
  event.waitUntil(
    caches.keys().then(function(keys) {
      return Promise.all(keys.filter(function(k) {
        return k !== CACHE_NAME;
      }).map(function(k) {
        return caches.delete(k);
      }));
    })
  );
  self.clients.claim();
});

self.addEventListener('fetch', function(event) {
  if (event.request.method !== 'GET') {
    return;
  }
  event.respondWith(
    caches.match(event.request).then(function(response) {
      return response || fetch(event.request).catch(function() {
        return caches.match(OFFLINE_URL);
      });
    })
  );
});
`
	_, _ = fmt.Fprint(w, sw)
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
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND", "template not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprint(w, tmpl.Content)
}

// handleAPIListText returns list of all templates as text
func (s *Server) handleAPIListText(w http.ResponseWriter, r *http.Request) {
	templates := s.config.Templates.List()
	w.Header().Set("Content-Type", "text/plain")
	for _, name := range templates {
		_, _ = fmt.Fprintln(w, name)
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
		_, _ = fmt.Fprintln(w, tmpl.Name)
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
		_, _ = fmt.Fprintln(w, cat)
	}
}

// handleAPICategoryTemplatesText returns templates in a category as text
func (s *Server) handleAPICategoryTemplatesText(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "name")
	templates := s.config.Templates.GetByCategory(category)
	w.Header().Set("Content-Type", "text/plain")
	for _, tmpl := range templates {
		_, _ = fmt.Fprintln(w, tmpl.Name)
	}
}

// handleAPIStatsText returns template statistics as text
func (s *Server) handleAPIStatsText(w http.ResponseWriter, r *http.Request) {
	stats := s.config.Templates.Stats()
	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprintf(w, "Total Templates: %d\n", stats["total_templates"])
	_, _ = fmt.Fprintf(w, "Categories: %d\n", stats["categories"])
	_, _ = fmt.Fprintf(w, "Total Size: %d bytes\n", stats["total_size_bytes"])
}
