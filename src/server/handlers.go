package server

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/go-chi/chi/v5"
)

// handleHome serves the home page
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "home", PageData{
		Title: i18n.T(r, "web.home.title"),
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
// zero-config clients and the companion CLI (AI.md PART 14 and PART 32).
func (s *Server) handleAPIAutodiscover(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"name":          "gitignore",
		"version":       s.config.Version,
		"api":           map[string]interface{}{"current_version": s.apiVersion()},
		"urls":          s.autodiscoverURLs(r),
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
	}
	s.addCLIDiscovery(data)
	if cfg := s.currentConfig(); cfg != nil && cfg.Server.Tor.Expose {
		if address, _ := s.torOnionAddress.Load().(string); address != "" {
			data["onion"] = address + ".onion"
		}
	}
	if cfg := s.currentConfig(); cfg != nil && cfg.Server.I2P.Expose && s.config.I2PStatus != nil {
		if info := s.config.I2PStatus(); info.Running && info.Hostname != "" {
			data["i2p"] = info.Hostname
		}
	}
	sendAPIResponseOK(w, data)
}

func (s *Server) autodiscoverURLs(r *http.Request) []string {
	urls := make([]string, 0)
	if s.isTorRequest(r) {
		return urls
	}
	seen := make(map[string]bool)
	add := func(value string) {
		origin, ok := canonicalOrigin(value)
		if !ok {
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return
		}
		hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		if _, overlay := overlayRequestHost(parsed.Host); overlay || strings.HasSuffix(hostname, ".onion") || strings.HasSuffix(hostname, ".b32.i2p") || seen[origin] {
			return
		}
		seen[origin] = true
		urls = append(urls, origin)
	}
	if r != nil {
		host := r.Host
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if s.isTrustedPeer(r.RemoteAddr) {
			if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
				host = forwardedHost
			}
			if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto == "http" || forwardedProto == "https" {
				scheme = forwardedProto
			}
		}
		add(scheme + "://" + host)
	}
	configured := make([]string, 0)
	for _, domain := range strings.FieldsFunc(os.Getenv("DOMAIN"), func(value rune) bool {
		return value == ',' || value == ';' || value == ' ' || value == '\t' || value == '\n'
	}) {
		candidate := strings.TrimSpace(domain)
		if candidate != "" && !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		configured = append(configured, candidate)
	}
	if cfg := s.currentConfig(); cfg != nil && strings.TrimSpace(cfg.Server.FQDN) != "" {
		candidate := strings.TrimSpace(cfg.Server.FQDN)
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		configured = append(configured, candidate)
	}
	sort.Strings(configured)
	for _, origin := range configured {
		add(origin)
	}
	learned := s.learnedCORSOrigins(time.Now())
	sort.Strings(learned)
	for _, origin := range learned {
		add(origin)
	}
	return urls
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
	_ = encodeJSON(w, APIResponse{
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
		Title: i18n.Translate(i18n.LangFromContext(r.Context()), "nav.api_docs"),
		PageCSS: []string{
			s.requestPath(r, "/static/vendor/swagger/swagger-ui.css"),
			s.requestPath(r, "/static/vendor/swagger/swagger-custom.css"),
		},
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
	_ = encodeJSON(w, s.openAPISpec(r))
}

// handleStatic serves embedded static assets under /static/.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.TrimPrefix(r.URL.Path, "/static/") == "css/accent.css" {
		cfg := s.currentConfig()
		if cfg == nil {
			http.NotFound(w, r)
			return
		}
		css, ok := accentCSS(cfg.Web.UI.Accent)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", accentETag(css))
		if r.Header.Get("If-None-Match") == accentETag(css) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = io.WriteString(w, css)
		return
	}
	setVersionedCacheHeaders(w, r, s.assetStamp())
	http.StripPrefix("/static/", http.FileServer(staticHTTPFS)).ServeHTTP(w, r)
}

// handleFavicon serves the embedded favicon.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	setCacheHeaders(w, "static")
	w.Header().Set("ETag", `"`+s.assetStamp()+`"`)
	f, err := staticHTTPFS.Open("favicon.ico")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "image/x-icon")
	_, _ = io.Copy(w, f)
}

func (s *Server) handleBrandingAsset(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	contentType := map[string]string{".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".webp": "image/webp", ".jpg": "image/jpeg"}[ext]
	if contentType == "" || s.config == nil || s.config.Paths == nil {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.config.Paths.GetConfigDir(), "assets", name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		http.NotFound(w, r)
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(body)
}

func (s *Server) handleCachedBrandingAsset(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || s.config == nil || s.config.Paths == nil {
		http.NotFound(w, r)
		return
	}
	extension := strings.ToLower(filepath.Ext(name))
	contentType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon"}[extension]
	base := strings.TrimSuffix(name, extension)
	if contentType == "" || len(base) != 64 {
		http.NotFound(w, r)
		return
	}
	for _, character := range base {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			http.NotFound(w, r)
			return
		}
	}
	path := filepath.Join(s.config.Paths.GetDataDir(), "cache", "branding", name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
		http.NotFound(w, r)
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+base+`"`)
	_, _ = w.Write(body)
}

// handleRobotsTxt serves robots.txt from config
func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "User-agent: *")
	if s.currentConfig() != nil {
		for _, path := range s.currentConfig().Web.Robots.Allow {
			_, _ = fmt.Fprintf(w, "Allow: %s\n", path)
		}
		for _, path := range s.currentConfig().Web.Robots.Deny {
			_, _ = fmt.Fprintf(w, "Disallow: %s\n", path)
		}
	} else {
		_, _ = fmt.Fprintln(w, "Allow: /")
		_, _ = fmt.Fprintln(w, "Allow: /api")
		_, _ = fmt.Fprintln(w, "Disallow: /debug")
	}
	_, _ = fmt.Fprintf(w, "Sitemap: %s\n", s.absoluteURL(r, "/sitemap.xml"))
}

// handleSecurityTxt serves security.txt
func (s *Server) handleSecurityTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	serverURL := s.publicServerURL(r)
	reportURL := "https://github.com/apimgr/gitignore/security/advisories/new"
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.ReportURL) != "" {
		reportURL = strings.TrimSpace(s.currentConfig().Web.Security.ReportURL)
	}
	if !s.isTorRequest(r) {
		_, _ = fmt.Fprintf(w, "Contact: %s\n", reportURL)
	}
	if id, err := securityID(time.Now()); err == nil {
		_, _ = fmt.Fprintf(w, "Contact: %s\n", securityReportURL(serverURL, id))
	}
	email := s.responseSecurityContactEmail(r)
	if !s.isTorRequest(r) && s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Contact) != "" {
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
	if !s.isTorRequest(r) {
		_, _ = fmt.Fprintln(w, "Preferred-Languages: en")
	}
	_, _ = fmt.Fprintf(w, "Canonical: %s/.well-known/security.txt\n", serverURL)
}

// handleManifest serves PWA manifest
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	setRevalidatedCacheHeaders(w, s.assetStamp())
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
	setRevalidatedCacheHeaders(w, s.assetStamp())
	stamp := s.assetStamp()
	staticURL := func(path string) string { return path + "?v=" + stamp }
	sw := fmt.Sprintf(`// GitIgnore Service Worker
const CACHE_NAME = %q;
const CACHE_PREFIX = 'gitignore-cache-';
const OFFLINE_URL = %q;
const PRECACHE_ASSETS = [%q, %q, %q, %q, %q, %q, %q];
const NAV_TIMEOUT_MS = 8000;

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE_NAME).then(cache => cache.addAll(PRECACHE_ASSETS)));
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(
    keys.filter(key => key.startsWith(CACHE_PREFIX) && key !== CACHE_NAME).map(key => caches.delete(key))
  )).then(() => self.clients.claim()));
});

self.addEventListener('message', event => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

function withTimeout(promise, milliseconds) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('navigation timeout')), milliseconds);
    promise.then(value => { clearTimeout(timer); resolve(value); }, error => { clearTimeout(timer); reject(error); });
  });
}

function offlineResponse() {
  return new Response('<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Offline</title><main><h1>Offline</h1><p>This page is unavailable offline. Reconnect and try again.</p></main></html>', {
    status: 503, statusText: 'Service Unavailable', headers: { 'Content-Type': 'text/html; charset=utf-8' }
  });
}

function remember(request, response) {
  if (response.ok && !response.redirected) {
    const copy = response.clone();
    caches.open(CACHE_NAME).then(cache => cache.put(request, copy)).catch(() => {});
  }
  return response;
}

async function navigationFallback(request) {
  try {
    const cachedPage = await caches.match(request, { ignoreSearch: true });
    if (cachedPage) return cachedPage;
    const offlinePage = await caches.match(OFFLINE_URL, { ignoreSearch: true });
    if (offlinePage) return offlinePage;
  } catch (_) {}
  return offlineResponse();
}

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;

  if (request.mode === 'navigate') {
    event.respondWith(withTimeout(fetch(request), NAV_TIMEOUT_MS).then(response => {
      if (response.redirected) throw new Error('redirected navigation');
      return remember(request, response);
    }).catch(() => navigationFallback(request)));
    return;
  }

  if (url.pathname.startsWith('/static/')) {
    event.respondWith(caches.match(request, { ignoreSearch: true }).then(cached => cached || fetch(request).then(response => remember(request, response)))
      .catch(() => new Response('', { status: 504, statusText: 'Gateway Timeout' })));
  }
});
`, "gitignore-cache-"+stamp, "/static/offline.html", staticURL("/static/css/common.css"), staticURL("/static/css/components.css"), staticURL("/static/css/public.css"), staticURL("/static/js/app.js"), staticURL("/static/images/icon.svg"), staticURL("/manifest.json"), staticURL("/static/offline.html"))
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
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
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
		sendAPIResponseErrorLocalized(w, r, "BAD_REQUEST")
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
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "%s\n", i18n.TF(r, "web.stats.total_templates", "count", fmt.Sprint(stats["total_templates"])))
	_, _ = fmt.Fprintf(w, "%s\n", i18n.TF(r, "web.stats.categories", "count", fmt.Sprint(stats["categories"])))
	_, _ = fmt.Fprintf(w, "%s\n", i18n.TF(r, "web.stats.total_size", "size", fmt.Sprint(stats["total_size_bytes"])))
}
