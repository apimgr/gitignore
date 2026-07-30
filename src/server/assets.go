package server

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"

	"github.com/apimgr/gitignore/src/common/i18n"
)

//go:embed assets/static
var staticFS embed.FS

//go:embed assets/html
var htmlFS embed.FS

// exOSFile is the sysexits(3) exit code for a missing/unreadable data file
// the app depends on (matches src/main.go's exOSFile) — used here because
// the embedded HTML assets are compiled into the binary and their absence
// means the build itself is broken, not a recoverable runtime condition.
const exOSFile = 72

// pageTemplates holds one fully-composed template per page. Each page file
// defines a "content" block that is rendered inside the shared "layout".
var pageTemplates = map[string]*template.Template{}

// staticHTTPFS is the http.FileSystem rooted at assets/static, used to serve
// /static/* and /favicon.ico.
var staticHTTPFS http.FileSystem

func init() {
	layout, err := htmlFS.ReadFile("assets/html/layout.html")
	if err != nil {
		fmt.Fprintf(os.Stderr, "embed: missing layout.html: %v\n", err)
		os.Exit(exOSFile)
	}

	pages := []string{
		"home", "search", "template", "combine", "categories", "list", "stats", "docs", "cli",
		"server", "about", "privacy", "contact", "help", "terms", "error",
	}
	for _, name := range pages {
		body, err := htmlFS.ReadFile("assets/html/" + name + ".html")
		if err != nil {
			fmt.Fprintf(os.Stderr, "embed: missing %s.html: %v\n", name, err)
			os.Exit(exOSFile)
		}
		t := template.Must(template.New(name).Parse(string(layout)))
		template.Must(t.Parse(string(body)))
		pageTemplates[name] = t
	}

	sub, err := fs.Sub(staticFS, "assets/static")
	if err != nil {
		fmt.Fprintf(os.Stderr, "embed: static sub: %v\n", err)
		os.Exit(exOSFile)
	}
	staticHTTPFS = http.FS(sub)
}

// PageData is the view model passed to every HTML page template.
type PageData struct {
	Title       string
	Description string
	Version     string
	BaseURL     string
	Lang        string
	Dir         string
	Theme       string
	Data        map[string]interface{}
}

// validThemes is the set of theme values accepted from the theme cookie and
// rendered as the theme-* class on <html> (AI.md PART 16 "Theme preference
// source"). The default when the cookie is missing or invalid is "dark".
var validThemes = map[string]bool{"dark": true, "light": true, "auto": true}

// themeFromRequest reads the server-side theme preference from the "theme"
// cookie, falling back to the dark default so the theme class renders with no
// client-side detection and no FOUC (AI.md PART 16).
func themeFromRequest(r *http.Request) string {
	if c, err := r.Cookie("theme"); err == nil && validThemes[c.Value] {
		return c.Value
	}
	return "dark"
}

// renderPage composes and writes an HTML page with a 200 status, applying the
// no-store cache policy required for HTML responses (AI.md PART 9).
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page string, data PageData) {
	s.renderPageStatus(w, r, page, http.StatusOK, data)
}

// renderPageStatus is renderPage with an explicit HTTP status. The status is
// written after the response headers so themed error pages keep their
// Content-Type and cache headers (AI.md PART 16 "Error Pages").
func (s *Server) renderPageStatus(w http.ResponseWriter, r *http.Request, page string, status int, data PageData) {
	t, ok := pageTemplates[page]
	if !ok {
		sendAPIResponseError(w, "SERVER_ERROR", "unknown page template")
		return
	}
	if data.Version == "" {
		data.Version = s.config.Version
	}
	if data.BaseURL == "" {
		data.BaseURL = s.detectServerURL(r)
	}
	if data.Description == "" {
		data.Description = "Comprehensive .gitignore template API."
	}
	if data.Lang == "" {
		data.Lang = i18n.LangFromContext(r.Context())
	}
	if data.Dir == "" {
		data.Dir = i18n.Direction(data.Lang)
	}
	if data.Theme == "" {
		data.Theme = themeFromRequest(r)
	}
	if data.Data == nil {
		data.Data = map[string]interface{}{}
	}

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "template render failed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setCacheHeaders(w, "html")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_, _ = buf.WriteTo(w)
}
