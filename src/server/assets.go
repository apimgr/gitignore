package server

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"

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
		"server", "about", "privacy", "contact", "help", "mcp_help", "preferences", "preferences_sync", "preferences_tokens", "terms", "dpo", "security", "security_policy", "security_thanks", "security_report_status", "error",
		"swagger_ui", "graphql_ui", "healthz",
	}
	for _, name := range pages {
		body, err := htmlFS.ReadFile("assets/html/" + name + ".html")
		if err != nil {
			fmt.Fprintf(os.Stderr, "embed: missing %s.html: %v\n", name, err)
			os.Exit(exOSFile)
		}
		funcs := template.FuncMap{
			"path": func(value string) string { return value },
			"t":    func(lang, key string) string { return i18n.Translate(lang, key) },
			"tf": func(lang, key string, args ...interface{}) string {
				return i18n.TranslateFormat(lang, key, args...)
			},
		}
		for functionName, function := range healthTemplateFuncs {
			funcs[functionName] = function
		}
		t := template.Must(template.New(name).Funcs(funcs).Parse(string(layout)))
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
	Title               string
	DocumentTitle       string
	ResourceLabelPlural string
	Description         string
	Version             string
	BuildDate           string
	BaseURL             string
	APIBase             string
	Lang                string
	Dir                 string
	Theme               string
	Path                string
	NextTheme           string
	LanguageName        string
	Languages           []i18n.LanguageInfo
	CSRFToken           string
	LanguagePreference  string
	PageCSS             []string
	PageScripts         []string
	Health              *healthPageView
	Data                map[string]interface{}
	TrackingMarkup      template.HTML
	TrackingNoscript    template.HTML
	Consent             ConsentView
}

type ConsentView struct {
	Show        bool
	DataSold    bool
	Message     string
	PolicyURL   string
	PolicyText  string
	AcceptText  string
	DeclineText string
	ManageText  string
	CSRFToken   string
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

func (s *Server) requestPath(r *http.Request, path string) string {
	prefix := ""
	if r != nil && s != nil && s.isTrustedPeer(r.RemoteAddr) {
		for _, name := range []string{"X-Forwarded-Prefix", "X-Forwarded-Path", "X-Script-Name"} {
			if value := r.Header.Get(name); value != "" {
				prefix = normalizePathPrefix(value)
				if prefix != "" {
					break
				}
			}
		}
	}
	if prefix == "" && s != nil && s.currentConfig() != nil {
		prefix = normalizePathPrefix(s.currentConfig().Server.BaseURL)
	}
	path = "/" + strings.TrimLeft(path, "/")
	if prefix == "" {
		return path
	}
	return prefix + path
}

func normalizePathPrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "?#\\,") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, "//") {
		return ""
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	prefix := "/" + strings.Trim(parsed.Path, "/")
	if prefix == "/" {
		return ""
	}
	return prefix
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
	if data.BuildDate == "" {
		data.BuildDate = s.config.BuildDate
	}
	if data.BaseURL == "" {
		data.BaseURL = s.detectServerURL(r)
	}
	if data.APIBase == "" {
		data.APIBase = s.requestPath(r, s.apiBasePath())
	}
	if data.Description == "" {
		data.Description = "Comprehensive .gitignore template API."
	}
	if data.ResourceLabelPlural == "" {
		data.ResourceLabelPlural = "templates"
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
	if data.Path == "" {
		data.Path = r.URL.RequestURI()
	}
	if data.NextTheme == "" {
		switch data.Theme {
		case "dark":
			data.NextTheme = "light"
		case "light":
			data.NextTheme = "auto"
		default:
			data.NextTheme = "dark"
		}
	}
	for _, language := range i18n.AvailableLanguages() {
		if language.Code == data.Lang {
			data.LanguageName = language.NativeName
			break
		}
	}
	data.Languages = i18n.AvailableLanguages()
	data.LanguagePreference = i18n.LanguagePreference(r)
	data.CSRFToken = ensureCSRFTokenWithSecure(w, r, preferenceCookieSecure(s, r))
	data.Consent = s.consentView(r, data.CSRFToken)
	if s.trackingAllowed(r) {
		provider := trackingProviderFor(s.currentConfig().Server.Tracking)
		data.TrackingMarkup = provider.tags
		data.TrackingNoscript = s.trackingNoscript(r)
		if provider.body != "" {
			data.TrackingMarkup += template.HTML(`<script async src="` + template.HTMLEscapeString(s.requestPath(r, "/server/tracking.js")) + `"></script>`)
		}
	}
	if data.Data == nil {
		data.Data = map[string]interface{}{}
	}

	requestTemplate, err := t.Clone()
	if err == nil {
		requestTemplate = requestTemplate.Funcs(template.FuncMap{"path": func(value string) string { return s.requestPath(r, value) }})
	}
	var buf bytes.Buffer
	if err == nil {
		err = requestTemplate.ExecuteTemplate(&buf, "layout", data)
	}
	if err != nil {
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

func ensureCSRFToken(w http.ResponseWriter, r *http.Request) string {
	return ensureCSRFTokenWithSecure(w, r, r.TLS != nil)
}

func ensureCSRFTokenWithSecure(w http.ResponseWriter, r *http.Request, secure bool) string {
	if cookie, err := r.Cookie("csrf_token"); err == nil && len(cookie.Value) == 64 {
		if _, err := hex.DecodeString(cookie.Value); err == nil {
			return cookie.Value
		}
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return ""
	}
	value := hex.EncodeToString(token[:])
	http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: value, Path: "/", MaxAge: 3600, SameSite: http.SameSiteStrictMode, Secure: secure, HttpOnly: true})
	return value
}
