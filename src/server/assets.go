package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/common/urlutil"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

//go:embed static
var staticFS embed.FS

//go:embed template
var htmlFS embed.FS

// exOSFile is the sysexits(3) exit code for a missing/unreadable data file
// the app depends on (matches src/main.go's exOSFile) — used here because
// the embedded HTML assets are compiled into the binary and their absence
// means the build itself is broken, not a recoverable runtime condition.
const exOSFile = 72

// pageTemplates holds one fully-composed template per page. Each page file
// defines a "content" block that is rendered inside the shared "layout".
var pageTemplates = map[string]*template.Template{}

// staticHTTPFS is the http.FileSystem rooted at static, used to serve
// /static/* and /favicon.ico.
var staticHTTPFS http.FileSystem

func init() {
	layout, err := htmlFS.ReadFile("template/layout/public.tmpl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "embed: missing template/layout/public.tmpl: %v\n", err)
		os.Exit(exOSFile)
	}

	pages := []string{
		"home", "search", "template", "combine", "categories", "list", "stats", "docs", "cli",
		"server", "about", "privacy", "contact", "help", "mcp_help", "preferences", "preferences_sync", "preferences_tokens", "terms", "dpo", "security", "security_policy", "security_thanks", "security_report_status", "error",
		"swagger_ui", "graphql_ui", "healthz",
	}
	partials := []string{
		"template/partial/head.tmpl",
		"template/partial/scripts.tmpl",
		"template/partial/toast.tmpl",
		"template/partial/public/header.tmpl",
		"template/partial/public/nav.tmpl",
		"template/partial/public/footer.tmpl",
	}
	for _, name := range pages {
		pageFile := name
		if name == "home" {
			pageFile = "index"
		}
		body, err := htmlFS.ReadFile("template/page/" + pageFile + ".tmpl")
		if err != nil {
			fmt.Fprintf(os.Stderr, "embed: missing template/page/%s.tmpl: %v\n", pageFile, err)
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
		for _, partialPath := range partials {
			partial, readErr := htmlFS.ReadFile(partialPath)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "embed: missing %s: %v\n", partialPath, readErr)
				os.Exit(exOSFile)
			}
			template.Must(t.Parse(string(partial)))
		}
		template.Must(t.Parse(string(body)))
		pageTemplates[name] = t
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		fmt.Fprintf(os.Stderr, "embed: static sub: %v\n", err)
		os.Exit(exOSFile)
	}
	staticHTTPFS = http.FS(sub)
}

// PageData is the view model passed to every HTML page template.
type PageData struct {
	Title                       string
	DocumentTitle               string
	ResourceLabelPlural         string
	Description                 string
	Branding                    BrandingPageData
	SEO                         SEOPageData
	Version                     string
	BuildDate                   string
	BuildDateHuman              string
	TorEnabled                  bool
	TorRunning                  bool
	OnionAddress                string
	I2PEnabled                  bool
	I2PRunning                  bool
	I2PAddress                  string
	BaseURL                     string
	APIBase                     string
	Lang                        string
	Dir                         string
	Theme                       string
	AccentCSS                   string
	Path                        string
	NextTheme                   string
	LanguageName                string
	Languages                   []i18n.LanguageInfo
	CSRFToken                   string
	LanguagePreference          string
	PageCSS                     []string
	PageScripts                 []string
	ToastPosition               string
	ToastDuration               int
	ToastWarningDuration        int
	ToastDismissText            string
	FlashKey                    string
	FlashType                   string
	Health                      *healthPageView
	Data                        map[string]interface{}
	TrackingMarkup              template.HTML
	TrackingNoscript            template.HTML
	Consent                     ConsentView
	Announcements               []AnnouncementView
	SuppressAnnouncementDismiss bool
	PrivateCache                bool
}

type BrandingPageData struct {
	Title       string
	Tagline     string
	LogoURL     string
	FaviconURL  string
	TitleSuffix string
}

type SEOPageData struct {
	CanonicalURL   string
	Keywords       string
	Author         string
	Robots         string
	OGEnabled      bool
	OGType         string
	OGSiteName     string
	OGImage        string
	TwitterEnabled bool
	TwitterCard    string
	TwitterSite    string
	TwitterCreator string
	MetaTags       map[string]string
	Verification   []PageMetaTag
	StructuredData template.JS
}

type PageMetaTag struct {
	Name    string
	Content string
}

type AnnouncementView struct {
	ID          string
	Type        string
	Icon        string
	Title       string
	Message     string
	Dismissible bool
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
	if strings.HasPrefix(path, "/static/") && !strings.Contains(path, "?") {
		path += "?v=" + s.assetStamp()
	}
	if prefix == "" {
		return path
	}
	return prefix + path
}

func (s *Server) assetStamp() string {
	version, commit := "dev", "unknown"
	if s != nil && s.config != nil {
		if value := strings.TrimSpace(s.config.Version); value != "" {
			version = value
		}
		if value := strings.TrimSpace(s.config.Commit); value != "" {
			commit = value
			if len(commit) > 7 {
				commit = commit[:7]
			}
		}
	}
	return version + "-" + commit
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
		sendAPIResponseError(w, "SERVER_ERROR", requestTranslation(r, "web.handler.error.unknown_template"))
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
	cfg := s.currentConfig()
	if data.Description == "" {
		if cfg != nil && strings.TrimSpace(cfg.Server.Branding.Description) != "" {
			data.Description = cfg.Server.Branding.Description
		} else if cfg != nil && strings.TrimSpace(cfg.Server.Branding.Tagline) != "" {
			data.Description = cfg.Server.Branding.Tagline
		} else {
			data.Description = requestTranslation(r, "web.handler.description.default")
		}
	}
	if cfg != nil {
		if validAccent(cfg.Web.UI.Accent) {
			data.AccentCSS = s.requestPath(r, "/static/css/accent.css")
		}
		brandTitle := fallbackString(cfg.Server.Branding.Title, "gitignore")
		seoConfig := cfg.Server.SEO
		seoConfig.TitleSeparator = validTitleSeparator(seoConfig.TitleSeparator)
		data.Branding = BrandingPageData{
			Title: brandTitle, Tagline: cfg.Server.Branding.Tagline,
			LogoURL:     s.brandingImageURL(r, cfg.Server.Branding.Logo, "/static/images/icon.svg"),
			FaviconURL:  s.brandingImageURL(r, cfg.Server.Branding.Favicon, "/favicon.ico"),
			TitleSuffix: seoConfig.TitleSeparator,
		}
		data.SEO = buildSEOPageData(s, r, seoConfig, data.Description)
		if cfg.Server.SEO.NoIndex {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
	}
	if data.ResourceLabelPlural == "" {
		data.ResourceLabelPlural = requestTranslation(r, "web.handler.resource_plural")
	}
	if data.Lang == "" {
		data.Lang = i18n.LangFromContext(r.Context())
	}
	if data.BuildDateHuman == "" {
		data.BuildDateHuman = humanTimestampString(data.Lang, data.BuildDate)
	}
	features := s.healthFeatures()
	if !data.TorEnabled {
		data.TorEnabled = features.Tor.Enabled
	}
	if !data.TorRunning {
		data.TorRunning = features.Tor.Running
	}
	if data.OnionAddress == "" {
		if address, ok := s.torOnionAddress.Load().(string); ok && address != "" {
			data.OnionAddress = strings.TrimSuffix(address, ".onion") + ".onion"
		}
	}
	if !data.I2PEnabled {
		data.I2PEnabled = features.I2P.Enabled
	}
	if !data.I2PRunning {
		data.I2PRunning = features.I2P.Running
	}
	if data.I2PAddress == "" {
		data.I2PAddress = features.I2P.Hostname
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
	data.SuppressAnnouncementDismiss = r.URL.Path == "/server/preferences"
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
	toastConfig := s.currentConfig().Server.Notifications.WebUI
	data.ToastPosition = toastConfig.Position
	data.ToastDuration = toastConfig.Duration
	data.ToastWarningDuration = toastConfig.WarningDuration
	data.ToastDismissText = i18n.Translate(data.Lang, "common.close")
	data.FlashKey, data.FlashType = consumeFlashCookie(w, r, preferenceCookieSecure(s, r))
	data.CSRFToken = ensureCSRFTokenWithSecure(w, r, preferenceCookieSecure(s, r))
	data.Consent = s.consentView(r, data.CSRFToken)
	data.Announcements = s.activeAnnouncements(r)
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
		log.Printf("failed to render server page %s: %v", page, err)
		sendAPIResponseError(w, "SERVER_ERROR", requestTranslation(r, "web.handler.error.render_failed"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data.PrivateCache {
		setCacheHeaders(w, "authenticated")
	} else {
		setCacheHeaders(w, "html")
	}
	if strings.HasPrefix(r.URL.Path, "/server/security/report/") {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
	}
	w.Header().Set("ETag", `"`+s.assetStamp()+`"`)
	versionPurge(w, r, s.assetStamp(), status)
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_, _ = buf.WriteTo(w)
}

func (s *Server) brandingImageURL(r *http.Request, value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		if fallback == "" {
			return ""
		}
		return s.requestPath(r, fallback)
	}
	if strings.HasPrefix(strings.ToLower(value), "data:image/") {
		if validBrandingDataURI(value) {
			return value
		}
		return fallbackBrandingURL(s, r, fallback)
	}
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		if strings.EqualFold(parsed.Scheme, "https") {
			if cached := s.cacheRemoteBrandingImage(r.Context(), value); cached != "" {
				return s.requestPath(r, cached)
			}
		}
		return fallbackBrandingURL(s, r, fallback)
	}
	name := filepath.Base(value)
	if name != value || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return fallbackBrandingURL(s, r, fallback)
	}
	if !validBrandingImageExtension(filepath.Ext(name)) {
		return fallbackBrandingURL(s, r, fallback)
	}
	return s.requestPath(r, "/assets/"+url.PathEscape(name))
}

func fallbackBrandingURL(s *Server, r *http.Request, fallback string) string {
	if fallback == "" {
		return ""
	}
	return s.requestPath(r, fallback)
}

func validBrandingDataURI(value string) bool {
	if len(value) > 256<<10 {
		return false
	}
	lower := strings.ToLower(value)
	for _, mediaType := range []string{"data:image/png;", "data:image/jpeg;", "data:image/webp;", "data:image/x-icon;", "data:image/svg+xml;"} {
		if strings.HasPrefix(lower, mediaType) {
			return true
		}
	}
	return false
}

func validBrandingImageExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".svg", ".png", ".ico", ".webp", ".jpg", ".gif":
		return true
	default:
		return false
	}
}

func (s *Server) cacheRemoteBrandingImage(ctx context.Context, rawURL string) string {
	if s == nil || s.config == nil || s.config.Paths == nil {
		return ""
	}
	digest := sha256.Sum256([]byte(rawURL))
	base := hex.EncodeToString(digest[:])
	directory := filepath.Join(s.config.Paths.GetDataDir(), "cache", "branding")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return ""
	}
	for _, extension := range []string{".png", ".jpg", ".gif", ".webp", ".ico"} {
		path := filepath.Join(directory, base+extension)
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() <= 10<<20 {
			return "/branding/cache/" + base + extension
		}
	}
	body, contentType, err := urlutil.FetchRemoteImage(ctx, rawURL, urlutil.DefaultFetchRemoteImageConfig())
	if err != nil {
		log.Printf("branding remote image could not be fetched: source=remote")
		return ""
	}
	extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/x-icon": ".ico"}[contentType]
	if extension == "" {
		return ""
	}
	temporary, err := os.CreateTemp(directory, ".branding-cache-*")
	if err != nil {
		return ""
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return ""
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return ""
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ""
	}
	if err := temporary.Close(); err != nil {
		return ""
	}
	if err := os.Rename(temporaryName, filepath.Join(directory, base+extension)); err != nil {
		return ""
	}
	return "/branding/cache/" + base + extension
}

func buildSEOPageData(s *Server, r *http.Request, cfg config.SEOConfig, description string) SEOPageData {
	robots := "noindex,nofollow"
	if !cfg.NoIndex && isPublicSEOPage(r.URL.Path) {
		robots = "index,follow"
	}
	canonical := strings.TrimSpace(cfg.Canonical)
	if parsed, err := url.Parse(canonical); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		canonical = s.detectServerURL(r)
	}
	canonical = strings.TrimRight(canonical, "/") + r.URL.EscapedPath()
	siteName := strings.TrimSpace(cfg.OpenGraph.SiteName)
	if siteName == "" {
		siteName = s.currentConfig().Server.Branding.Title
	}
	keywords := strings.Join(cfg.Keywords, ", ")
	title := fallbackString(s.currentConfig().Server.Branding.Title, "gitignore")
	meta := make(map[string]string)
	for name, content := range cfg.MetaTags {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || len(name) > 64 || len(meta) >= 20 || !validSEOName(name) || reservedSEOMetaName(name) {
			continue
		}
		meta[name] = content
	}
	verification := []PageMetaTag{}
	for name, content := range map[string]string{
		"google-site-verification":     cfg.Verification.Google,
		"msvalidate.01":                cfg.Verification.Bing,
		"yandex-verification":          cfg.Verification.Yandex,
		"baidu-site-verification":      cfg.Verification.Baidu,
		"p:domain_verify":              cfg.Verification.Pinterest,
		"facebook-domain-verification": cfg.Verification.Facebook,
	} {
		if strings.TrimSpace(content) != "" {
			verification = append(verification, PageMetaTag{Name: name, Content: strings.TrimSpace(content)})
		}
	}
	for _, tag := range cfg.Verification.Custom {
		name := strings.ToLower(strings.TrimSpace(tag.Name))
		if validSEOName(name) && !reservedSEOMetaName(name) && strings.TrimSpace(tag.Content) != "" {
			verification = append(verification, PageMetaTag{Name: name, Content: strings.TrimSpace(tag.Content)})
		}
	}
	structured, _ := json.Marshal(map[string]string{
		"@context": "https://schema.org", "@type": "WebSite", "name": title,
		"description": description, "url": canonical,
	})
	return SEOPageData{
		CanonicalURL: canonical, Keywords: keywords, Author: cfg.Author, Robots: robots,
		OGEnabled: cfg.OpenGraph.Enabled, OGType: fallbackString(cfg.OpenGraph.Type, "website"),
		OGSiteName: siteName, OGImage: s.brandingImageURL(r, cfg.OGImage, ""),
		TwitterEnabled: cfg.Twitter.Enabled, TwitterCard: fallbackString(cfg.Twitter.Card, "summary_large_image"),
		TwitterSite: cfg.TwitterHandle, TwitterCreator: cfg.Twitter.Creator, MetaTags: meta, Verification: verification,
		StructuredData: template.JS(structured),
	}
}

func validTitleSeparator(value string) string {
	if value == "" || len([]rune(value)) > 8 {
		return " - "
	}
	return value
}

func isPublicSEOPage(path string) bool {
	switch {
	case path == "/", path == "/search", path == "/combine", path == "/categories", path == "/list", path == "/stats":
		return true
	case strings.HasPrefix(path, "/template/"):
		return true
	case path == "/server/docs" || path == "/server/docs/swagger" || path == "/server/docs/graphql":
		return true
	case path == "/server/about" || path == "/server/privacy" || path == "/server/contact" || path == "/server/help" || path == "/server/terms" || path == "/server/dpo" || path == "/server/security" || path == "/server/security/policy" || path == "/server/security/thanks":
		return true
	default:
		return false
	}
}

func validSEOName(name string) bool {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune(":_.-", r)) {
			return false
		}
	}
	return name != ""
}

func reservedSEOMetaName(name string) bool {
	switch name {
	case "description", "keywords", "author", "robots", "viewport":
		return true
	default:
		return false
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func ensureCSRFToken(w http.ResponseWriter, r *http.Request) string {
	return ensureCSRFTokenWithSecure(w, r, r.TLS != nil)
}

func ensureCSRFTokenWithSecure(w http.ResponseWriter, r *http.Request, secure bool) string {
	if cookie, err := r.Cookie("csrf_token"); err == nil && validCSRFToken(cookie.Value) {
		return cookie.Value
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return ""
	}
	secret, err := db.ApplicationSecret("csrf_token_secret")
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(token[:])
	value := hex.EncodeToString(token[:]) + hex.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: value, Path: "/", MaxAge: 3600, SameSite: http.SameSiteStrictMode, Secure: secure, HttpOnly: false})
	return value
}

func validCSRFToken(value string) bool {
	if len(value) != 128 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 64 {
		return false
	}
	secret, err := db.ApplicationSecret("csrf_token_secret")
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(decoded[:32])
	return subtle.ConstantTimeCompare(decoded[32:], mac.Sum(nil)) == 1
}
