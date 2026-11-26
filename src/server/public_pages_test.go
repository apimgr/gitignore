package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/template"
)

func TestOperatorContentCannotIntroducePageH1(t *testing.T) {
	if got := string(renderPublicContent("# Operator title\n\n<h1>Injected title</h1>", "md")); strings.Contains(got, "<h1>") {
		t.Fatalf("operator content introduced h1: %s", got)
	} else if !strings.Contains(got, "<h2>Operator title</h2>") {
		t.Fatalf("Markdown level-one heading was not rendered as h2: %s", got)
	}
}

func TestPrivacyPageRendersNoJSCCPAFormAndPersistsChoice(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Privacy.Data.Sold = true
	s := New(&Config{Version: "test", Cfg: cfg})

	page := httptest.NewRecorder()
	s.router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/server/privacy", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("privacy page status = %d", page.Code)
	}
	if !strings.Contains(page.Body.String(), `action="/server/ccpa"`) || !strings.Contains(page.Body.String(), `name="csrf_token"`) {
		t.Fatalf("privacy page is missing the CCPA form or CSRF token")
	}
	csrf := ""
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			csrf = cookie.Value
		}
	}
	if csrf == "" {
		t.Fatal("privacy page did not set the CSRF cookie")
	}

	post := func(choice string) *httptest.ResponseRecorder {
		form := url.Values{"csrf_token": {csrf}, "choice": {choice}, "redirect": {"/server/privacy"}}
		req := httptest.NewRequest(http.MethodPost, "/server/ccpa", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		return rec
	}
	optOut := post("opt-out")
	if optOut.Code != http.StatusSeeOther || optOut.Header().Get("Location") != "/server/privacy" {
		t.Fatalf("opt-out status/location = %d/%q", optOut.Code, optOut.Header().Get("Location"))
	}
	found := false
	for _, cookie := range optOut.Result().Cookies() {
		if cookie.Name == "ccpa_opt_out" && cookie.Value == "true" {
			found = true
		}
	}
	if !found {
		t.Fatal("opt-out did not set ccpa_opt_out")
	}
	optIn := post("opt-in")
	if optIn.Code != http.StatusSeeOther {
		t.Fatalf("opt-in status = %d, want 303", optIn.Code)
	}
	found = false
	for _, cookie := range optIn.Result().Cookies() {
		if cookie.Name == "ccpa_opt_out" && cookie.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("opt-in did not clear ccpa_opt_out")
	}
}

func TestAnnouncementPageAndNoJSDismissal(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Web.Announcements.Messages = []config.WebAnnouncementConfig{{
		ID: "maintenance-window", Type: "warning", Title: "Maintenance",
		Message: "Service will pause briefly.", Dismissible: true,
	}}
	s := New(&Config{Version: "test", Cfg: cfg})
	manager, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	s.config.Templates = manager
	page := httptest.NewRecorder()
	s.router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("home page status = %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, `class="site-banner site-banner-warning" role="alert"`) || !strings.Contains(body, "Service will pause briefly.") {
		t.Fatal("configured announcement was not rendered")
	}
	skip := strings.Index(body, `href="#main-content"`)
	announcement := strings.Index(body, `class="site-banner site-banner-warning"`)
	if skip < 0 || announcement < 0 || skip > announcement {
		t.Fatal("announcement rendered before the skip links")
	}
	if consent := strings.Index(body, `id="cookie-consent"`); consent < 0 || consent > announcement {
		t.Fatal("cookie consent banner did not precede site announcements")
	}
	preferences := httptest.NewRecorder()
	s.router.ServeHTTP(preferences, httptest.NewRequest(http.MethodGet, "/server/preferences", nil))
	if !strings.Contains(preferences.Body.String(), `data-announcement-id="maintenance-window"`) || strings.Contains(preferences.Body.String(), `action="/announcements/dismiss"`) {
		t.Fatal("preferences page must show announcements without adding a second POST form")
	}
	csrf := ""
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			csrf = cookie.Value
		}
	}
	if csrf == "" {
		t.Fatal("page did not issue a CSRF cookie")
	}
	form := url.Values{"csrf_token": {csrf}, "id": {"maintenance-window"}, "return_to": {"/server/about?from=notice"}}
	request := httptest.NewRequest(http.MethodPost, "/announcements/dismiss", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/server/about?from=notice" {
		t.Fatalf("dismiss status/location = %d/%q", response.Code, response.Header().Get("Location"))
	}
	found := false
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == dismissedAnnouncementsCookie && cookie.Value == "maintenance-window" {
			found = true
		}
	}
	if !found {
		t.Fatal("dismissal did not persist the announcement ID")
	}
	dismissedPage := httptest.NewRecorder()
	dismissedRequest := httptest.NewRequest(http.MethodGet, "/server/about", nil)
	dismissedRequest.AddCookie(&http.Cookie{Name: dismissedAnnouncementsCookie, Value: "maintenance-window"})
	s.router.ServeHTTP(dismissedPage, dismissedRequest)
	if strings.Contains(dismissedPage.Body.String(), `data-announcement-id="maintenance-window"`) {
		t.Fatal("dismissed announcement was rendered again")
	}
}

func TestAnnouncementDismissalRejectsExternalReturnURL(t *testing.T) {
	if got := safeAnnouncementReturn("//attacker.example/path"); got != "/" {
		t.Fatalf("unsafe announcement return target = %q", got)
	}
}

func TestAnnouncementDismissalRequiresCSRF(t *testing.T) {
	s := newTestPageServer()
	s.setupRoutes()
	request := httptest.NewRequest(http.MethodPost, "/announcements/dismiss", strings.NewReader("id=maintenance-window"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("dismiss without CSRF status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestHTMLBuildStampAndVersionPurge(t *testing.T) {
	stamp := "1.2.3-abc1234"
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "gitignore_build", Value: "old-build"})
	response := httptest.NewRecorder()
	versionPurge(response, request, stamp, http.StatusOK)
	if got := response.Header().Get("Clear-Site-Data"); got != `"cache", "storage"` {
		t.Fatalf("Clear-Site-Data = %q", got)
	}
	var buildCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "gitignore_build" {
			buildCookie = cookie
		}
	}
	if buildCookie == nil || buildCookie.Value != stamp || buildCookie.Path != "/" || buildCookie.MaxAge != 31536000 || !buildCookie.Secure || buildCookie.HttpOnly || buildCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("build stamp cookie = %#v", buildCookie)
	}
	server := newTestPageServer()
	page := httptest.NewRecorder()
	server.renderPage(page, httptest.NewRequest(http.MethodGet, "/", nil), "home", PageData{Title: "Home"})
	if page.Header().Get("ETag") != `"test-unknown"` {
		t.Errorf("HTML ETag = %q", page.Header().Get("ETag"))
	}
	foundBuildCookie := false
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == "gitignore_build" && cookie.Value == "test-unknown" {
			foundBuildCookie = true
		}
	}
	if !foundBuildCookie {
		t.Error("HTML page did not set its build-stamp cookie")
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "gitignore_build", Value: stamp})
	response = httptest.NewRecorder()
	versionPurge(response, request, stamp, http.StatusOK)
	if response.Header().Get("Clear-Site-Data") != "" {
		t.Fatal("matching build stamp triggered a purge")
	}

	for _, tc := range []struct {
		name   string
		method string
		status int
		fetch  bool
	}{
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "error", method: http.MethodGet, status: http.StatusNotFound},
		{name: "iframe", method: http.MethodGet, status: http.StatusOK, fetch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			req.AddCookie(&http.Cookie{Name: "gitignore_build", Value: "old-build"})
			if tc.fetch {
				req.Header.Set("Sec-Fetch-Dest", "iframe")
				req.Header.Set("Sec-Fetch-Mode", "navigate")
			}
			rec := httptest.NewRecorder()
			versionPurge(rec, req, stamp, tc.status)
			if rec.Header().Get("Clear-Site-Data") != "" {
				t.Fatal("non-document response triggered a purge")
			}
		})
	}
}

func TestTermsPageUsesConfiguredOverrideAndSanitizesIt(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Content.Terms.Format = "html"
	cfg.Server.Branding.Content.Terms.Body = `<p>Custom terms</p><script>alert(1)</script><a href="/server/contact">contact</a>`
	s := New(&Config{Version: "test", Cfg: cfg})
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/server/terms", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("terms page status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Custom terms") || strings.Contains(body, "alert(1)") || strings.Contains(body, `href="/server/contact"`) {
		t.Fatalf("configured terms were not safely rendered: %s", body)
	}
}

func TestBuiltInPrivacyAndTermsContentUsesRequestLanguage(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(&Config{Version: "test", Cfg: cfg})

	privacy := httptest.NewRecorder()
	s.router.ServeHTTP(privacy, httptest.NewRequest(http.MethodGet, "/server/privacy?lang=es", nil))
	if privacy.Code != http.StatusOK {
		t.Fatalf("privacy page status = %d", privacy.Code)
	}
	privacyBody := privacy.Body.String()
	for _, text := range []string{
		"Solo recopilamos lo necesario para prestar nuestro servicio",
		"Sus datos se utilizan únicamente para",
		"Son necesarias para que el sitio funcione",
		"Este mensaje se muestra de conformidad con la normativa RGPD",
	} {
		if !strings.Contains(privacyBody, text) {
			t.Errorf("Spanish privacy page is missing %q", text)
		}
	}
	if strings.Contains(privacyBody, "We collect only what is necessary") || strings.Contains(privacyBody, "Required for the site to function") {
		t.Error("Spanish privacy page contains built-in English copy")
	}

	terms := httptest.NewRecorder()
	s.router.ServeHTTP(terms, httptest.NewRequest(http.MethodGet, "/server/terms?lang=es", nil))
	if terms.Code != http.StatusOK {
		t.Fatalf("terms page status = %d", terms.Code)
	}
	termsBody := terms.Body.String()
	if !strings.Contains(termsBody, "El uso de GitIgnore API implica la aceptación de estas condiciones") || strings.Contains(termsBody, "Using GitIgnore API means accepting these terms") {
		t.Error("Spanish terms page did not use the translated built-in terms")
	}

	api := httptest.NewRecorder()
	s.router.ServeHTTP(api, httptest.NewRequest(http.MethodGet, s.apiBasePath()+"/server/privacy?lang=es", nil))
	if api.Code != http.StatusOK || !strings.Contains(api.Body.String(), "Sus datos se almacenan en nuestros servidores") || strings.Contains(api.Body.String(), "Your data is stored on our servers") {
		t.Errorf("Spanish privacy API did not localize built-in content: status %d, body %s", api.Code, api.Body.String())
	}
}

func TestCookieConsentPreservesOperatorCustomText(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Privacy.Consent.Message = "Operator consent copy."
	cfg.Server.Privacy.Cookies.Essential.Description = "Operator essential-cookie copy."
	s := New(&Config{Version: "test", Cfg: cfg})

	page := httptest.NewRecorder()
	s.router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/server/privacy?lang=es", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("privacy page status = %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "Operator consent copy.") || !strings.Contains(body, "Operator essential-cookie copy.") {
		t.Error("custom consent or cookie-category copy was not preserved")
	}

	cfg.Server.Privacy.Data.Sold = true
	cfg.Server.Privacy.Consent.MessageIfSold = "Operator sold-data consent copy."
	s.ReplaceConfig(cfg)
	soldPage := httptest.NewRecorder()
	s.router.ServeHTTP(soldPage, httptest.NewRequest(http.MethodGet, "/server/privacy?lang=es", nil))
	if !strings.Contains(soldPage.Body.String(), "Operator sold-data consent copy.") {
		t.Error("custom sold-data consent copy was not preserved")
	}
}

func TestSoldPrivacyDefaultsUseLocalizedConsentAndUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Privacy.Data.Sold = true
	s := New(&Config{Version: "test", Cfg: cfg})

	page := httptest.NewRecorder()
	s.router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/server/privacy?lang=es", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("privacy page status = %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "Sus datos pueden compartirse con terceros o venderse") || !strings.Contains(body, "sus datos pueden compartirse con terceros o venderse") {
		t.Error("sold-data privacy defaults were not rendered in Spanish")
	}
}
