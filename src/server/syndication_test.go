package server

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/template"
	"github.com/go-chi/chi/v5"
)

func TestSitemapContainsPublicPagesAndCatalog(t *testing.T) {
	manager, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	s := newTestPageServer()
	s.config.Templates = manager
	r := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	r.Host = "catalog.example.test"
	w := httptest.NewRecorder()
	s.handleSitemap(w, r)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("sitemap response = %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var doc sitemapDocument
	if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.URLs) == 0 {
		t.Fatal("sitemap has no public URLs")
	}
	seen := make(map[string]bool, len(doc.URLs))
	for _, item := range doc.URLs {
		if !strings.HasPrefix(item.Loc, "http://catalog.example.test/") {
			t.Errorf("sitemap URL is not absolute for request host: %s", item.Loc)
		}
		if strings.Contains(item.Loc, "/api/") || strings.Contains(item.Loc, "/server/preferences") || strings.Contains(item.Loc, "/server/contact") {
			t.Errorf("private or excluded route in sitemap: %s", item.Loc)
		}
		seen[item.Loc] = true
	}
	for _, path := range []string{"/", "/server/docs/swagger", "/server/docs/graphql", "/template/Go"} {
		if !seen["http://catalog.example.test"+path] {
			t.Errorf("sitemap omitted %s", path)
		}
	}
}

func TestSitemapHonorsRobotsDenyAndSplitsLargeCatalog(t *testing.T) {
	s := newTestPageServer()
	s.config.Cfg.Web.Robots.Allow = []string{"/"}
	s.config.Cfg.Web.Robots.Deny = []string{"/server/docs", "/template/Go"}
	s.config.Cfg.Server.SEO.Sitemap.MaxURLs = 2
	s.config.Templates = &template.Manager{}
	urls := s.publicSitemapURLs(httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	for _, item := range urls {
		if strings.Contains(item.Loc, "/server/docs") || strings.HasSuffix(item.Loc, "/template/Go") {
			t.Errorf("robots-denied URL is in sitemap: %s", item.Loc)
		}
	}
	// The two configured slots force the index route even without catalog data.
	r := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	r.Host = "catalog.example.test"
	w := httptest.NewRecorder()
	s.handleSitemap(w, r)
	if !strings.Contains(w.Body.String(), "sitemapindex") {
		t.Fatalf("expected sitemap index, got %s", w.Body.String())
	}
	part := httptest.NewRequest(http.MethodGet, "/sitemap-1.xml", nil)
	part.Host = r.Host
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("part", "1")
	part = part.WithContext(context.WithValue(part.Context(), chi.RouteCtxKey, routeContext))
	partw := httptest.NewRecorder()
	s.handleSitemapPart(partw, part)
	if partw.Code != http.StatusOK || !strings.Contains(partw.Body.String(), "urlset") {
		t.Fatalf("sitemap part response = %d %s", partw.Code, partw.Body.String())
	}
}

func TestSyndicationFeedsAreValidEmptyAndCacheable(t *testing.T) {
	s := newTestPageServer()
	s.config.Cfg.Server.SEO.Feed.Title = "GitIgnore \x01 & <catalog>"
	for _, tc := range []struct {
		path, contentType string
		handler           http.HandlerFunc
	}{{"/feed.xml", "application/atom+xml", s.handleAtomFeed}, {"/rss.xml", "application/rss+xml", s.handleRSSFeed}} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.Host = "catalog.example.test"
			w := httptest.NewRecorder()
			tc.handler(w, r)
			if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("feed response = %d %q", w.Code, w.Header().Get("Content-Type"))
			}
			if !strings.Contains(w.Body.String(), "catalog.example.test") || w.Header().Get("ETag") == "" || w.Header().Get("Last-Modified") == "" {
				t.Fatalf("feed omitted request URL or cache validators: headers=%v body=%s", w.Header(), w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "&amp; &lt;catalog&gt;") || strings.ContainsRune(w.Body.String(), '\x01') {
				t.Errorf("feed title was not XML-safe: %s", w.Body.String())
			}
			cached := httptest.NewRequest(http.MethodGet, tc.path, nil)
			cached.Host = r.Host
			cached.Header.Set("If-None-Match", w.Header().Get("ETag"))
			cachedResponse := httptest.NewRecorder()
			tc.handler(cachedResponse, cached)
			if cachedResponse.Code != http.StatusNotModified {
				t.Errorf("matching ETag status = %d, want 304", cachedResponse.Code)
			}
		})
	}
}
