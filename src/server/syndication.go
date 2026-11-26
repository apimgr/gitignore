package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const sitemapNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

type sitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

type sitemapDocument struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapIndex struct {
	XMLName xml.Name           `xml:"sitemapindex"`
	XMLNS   string             `xml:"xmlns,attr"`
	Items   []sitemapIndexItem `xml:"sitemap"`
}

type sitemapIndexItem struct {
	Loc string `xml:"loc"`
}

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    []feedLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type feedLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	Title   string `xml:"title"`
	ID      string `xml:"id"`
	Updated string `xml:"updated"`
}

type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	AtomLink    feedLink  `xml:"http://www.w3.org/2005/Atom link"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
	GUID  string `xml:"guid"`
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	urls := s.publicSitemapURLs(r)
	limit := s.sitemapLimit()
	if len(urls) > limit {
		pages := (len(urls) + limit - 1) / limit
		index := sitemapIndex{XMLNS: sitemapNamespace, Items: make([]sitemapIndexItem, 0, pages)}
		for page := 1; page <= pages; page++ {
			index.Items = append(index.Items, sitemapIndexItem{Loc: s.absoluteURL(r, "/sitemap-"+strconv.Itoa(page)+".xml")})
		}
		body := marshalXML(index)
		s.writeXML(w, r, body)
		return
	}
	body := marshalXML(sitemapDocument{XMLNS: sitemapNamespace, URLs: urls})
	s.writeXML(w, r, body)
}

func (s *Server) handleSitemapPart(w http.ResponseWriter, r *http.Request) {
	part, err := strconv.Atoi(chi.URLParam(r, "part"))
	if err != nil || part < 1 {
		http.NotFound(w, r)
		return
	}
	urls := s.publicSitemapURLs(r)
	limit := s.sitemapLimit()
	if part > (len(urls)+limit-1)/limit {
		http.NotFound(w, r)
		return
	}
	start := (part - 1) * limit
	end := start + limit
	if end > len(urls) {
		end = len(urls)
	}
	body := marshalXML(sitemapDocument{XMLNS: sitemapNamespace, URLs: urls[start:end]})
	s.writeXML(w, r, body)
}

func (s *Server) sitemapLimit() int {
	limit := 50000
	if cfg := s.currentConfig(); cfg != nil && cfg.Server.SEO.Sitemap.MaxURLs > 0 && cfg.Server.SEO.Sitemap.MaxURLs < limit {
		limit = cfg.Server.SEO.Sitemap.MaxURLs
	}
	return limit
}

func (s *Server) publicSitemapURLs(r *http.Request) []sitemapURL {
	lastmod := s.contentLastModified().Format("2006-01-02")
	type page struct {
		path, changefreq, priority string
	}
	pages := []page{
		{path: "/", changefreq: "daily", priority: "1.0"},
		{path: "/search", changefreq: "weekly", priority: "0.8"},
		{path: "/list", changefreq: "weekly", priority: "0.8"},
		{path: "/categories", changefreq: "weekly", priority: "0.8"},
		{path: "/combine", changefreq: "weekly", priority: "0.8"},
		{path: "/stats", changefreq: "weekly", priority: "0.8"},
		{path: "/docs", changefreq: "weekly", priority: "0.8"},
		{path: "/cli", changefreq: "weekly", priority: "0.8"},
		{path: "/server/about", changefreq: "weekly", priority: "0.8"},
		{path: "/server/privacy", changefreq: "weekly", priority: "0.8"},
		{path: "/server/dpo", changefreq: "weekly", priority: "0.8"},
		{path: "/server/terms", changefreq: "weekly", priority: "0.8"},
		{path: "/server/help", changefreq: "weekly", priority: "0.8"},
		{path: "/server/help/mcp", changefreq: "weekly", priority: "0.8"},
		{path: "/server/healthz", changefreq: "weekly", priority: "0.8"},
		{path: "/server/security", changefreq: "weekly", priority: "0.8"},
		{path: "/server/security/policy", changefreq: "weekly", priority: "0.8"},
		{path: "/server/security/thanks", changefreq: "weekly", priority: "0.8"},
		{path: "/server/docs/swagger", changefreq: "weekly", priority: "0.7"},
		{path: "/server/docs/graphql", changefreq: "weekly", priority: "0.7"},
	}
	capacity := len(pages)
	if s.config != nil && s.config.Templates != nil {
		capacity += len(s.config.Templates.ListAll())
	}
	result := make([]sitemapURL, 0, capacity)
	for _, p := range pages {
		if s.allowedInRobots(p.path) {
			result = append(result, sitemapURL{Loc: s.absoluteURL(r, p.path), LastMod: lastmod, ChangeFreq: p.changefreq, Priority: p.priority})
		}
	}
	if s.config != nil && s.config.Templates != nil && s.allowedInRobots("/template/") {
		names := s.config.Templates.List()
		sort.Strings(names)
		for _, name := range names {
			path := "/template/" + url.PathEscape(name)
			if s.allowedInRobots(path) {
				result = append(result, sitemapURL{Loc: s.absoluteURL(r, path), LastMod: lastmod, ChangeFreq: "weekly", Priority: "0.6"})
			}
		}
	}
	return result
}

func (s *Server) allowedInRobots(path string) bool {
	cfg := s.currentConfig()
	if cfg == nil {
		return !strings.HasPrefix(path, "/api/") && !s.isRegisteredPreferencePath(path) && path != "/server/contact"
	}
	allowed := len(cfg.Web.Robots.Allow) == 0
	for _, pattern := range cfg.Web.Robots.Allow {
		if robotsPathMatch(pattern, path) {
			allowed = true
		}
	}
	for _, pattern := range cfg.Web.Robots.Deny {
		if robotsPathMatch(pattern, path) {
			allowed = false
		}
	}
	return allowed && !strings.HasPrefix(path, "/api/") && !s.isRegisteredPreferencePath(path) && path != "/server/contact"
}

func (s *Server) isRegisteredPreferencePath(path string) bool {
	if path == "/server/preferences" {
		return true
	}
	for _, page := range serverPageRegistry {
		if page.preference && page.enabled(s) && page.path == path {
			return true
		}
	}
	return false
}

func robotsPathMatch(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	if pattern == "/" {
		return strings.HasPrefix(path, "/")
	}
	return path == pattern || strings.HasPrefix(path, strings.TrimRight(pattern, "/")+"/")
}

func (s *Server) handleAtomFeed(w http.ResponseWriter, r *http.Request) {
	baseURL := s.publicServerURL(r)
	title := safeXMLText(s.feedTitle())
	updated := s.contentLastModified().UTC().Format(time.RFC3339)
	body := marshalXML(atomFeed{
		Title:   title,
		ID:      baseURL + "/",
		Updated: updated,
		Link:    []feedLink{{Href: baseURL + "/", Rel: "alternate", Type: "text/html"}, {Href: baseURL + "/feed.xml", Rel: "self", Type: "application/atom+xml"}},
	})
	s.writeFeed(w, r, "application/atom+xml; charset=utf-8", body)
}

func (s *Server) handleRSSFeed(w http.ResponseWriter, r *http.Request) {
	baseURL := s.publicServerURL(r)
	title := safeXMLText(s.feedTitle())
	body := marshalXML(rssDocument{
		Version: "2.0",
		Channel: rssChannel{
			Title: title, Link: baseURL + "/", Description: title,
			AtomLink: feedLink{Href: baseURL + "/rss.xml", Rel: "self", Type: "application/rss+xml"},
		},
	})
	s.writeFeed(w, r, "application/rss+xml; charset=utf-8", body)
}

func (s *Server) feedTitle() string {
	if cfg := s.currentConfig(); cfg != nil && strings.TrimSpace(cfg.Server.SEO.Feed.Title) != "" {
		return strings.TrimSpace(cfg.Server.SEO.Feed.Title)
	}
	if cfg := s.currentConfig(); cfg != nil && strings.TrimSpace(cfg.Server.Branding.Title) != "" {
		return strings.TrimSpace(cfg.Server.Branding.Title)
	}
	return "GitIgnore API"
}

func (s *Server) contentLastModified() time.Time {
	if s.config != nil && strings.TrimSpace(s.config.BuildDate) != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02", "20060102T150405Z"} {
			if parsed, err := time.Parse(layout, s.config.BuildDate); err == nil {
				return parsed.UTC().Truncate(time.Second)
			}
		}
	}
	return time.Unix(0, 0).UTC()
}

func marshalXML(value any) []byte {
	body, _ := xml.Marshal(value)
	return append([]byte(xml.Header), body...)
}

func safeXMLText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x9 || r == 0xA || r == 0xD || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF {
			return r
		}
		return -1
	}, value)
}

func (s *Server) absoluteURL(r *http.Request, path string) string {
	return strings.TrimRight(s.publicServerURL(r), "/") + path
}

func (s *Server) writeXML(w http.ResponseWriter, r *http.Request, body []byte) {
	s.writeCachedXML(w, r, "application/xml; charset=utf-8", body, "public, max-age=300")
}

func (s *Server) writeFeed(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	s.writeCachedXML(w, r, contentType, body, "public, max-age=300")
}

func (s *Server) writeCachedXML(w http.ResponseWriter, r *http.Request, contentType string, body []byte, cacheControl string) {
	digest := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	modified := s.contentLastModified()
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", cacheControl)
	header.Set("ETag", etag)
	header.Set("Last-Modified", modified.Format(http.TimeFormat))
	if etagMatches(r.Header.Get("If-None-Match"), etag) || (r.Header.Get("If-None-Match") == "" && notModifiedSince(r, modified)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func notModifiedSince(r *http.Request, modified time.Time) bool {
	value := r.Header.Get("If-Modified-Since")
	if value == "" {
		return false
	}
	parsed, err := http.ParseTime(value)
	return err == nil && !modified.After(parsed)
}
