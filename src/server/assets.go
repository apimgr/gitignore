package server

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
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

	pages := []string{"home", "search", "template", "combine", "categories", "list", "stats", "docs", "cli"}
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
	Data        map[string]interface{}
}

// renderPage composes and writes an HTML page, applying the no-store cache
// policy required for HTML responses (AI.md PART 9).
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page string, data PageData) {
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
	_, _ = buf.WriteTo(w)
}
