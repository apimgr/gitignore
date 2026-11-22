package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

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
