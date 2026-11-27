package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

func TestFlashMessageSurvivesPostRedirectWithoutJavaScript(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s := New(&ServerOptions{Version: "test", Cfg: config.DefaultConfig()})
	s.currentConfig().Server.Privacy.Data.Sold = true
	handler := flashRedirectMiddleware(s, s.router)

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/server/privacy", nil))
	var csrf string
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			csrf = cookie.Value
		}
	}
	if csrf == "" {
		t.Fatal("privacy page did not issue a CSRF token")
	}

	form := url.Values{"csrf_token": {csrf}, "choice": {"opt-out"}, "redirect": {"/server/privacy"}}
	request := httptest.NewRequest(http.MethodPost, "/server/ccpa", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, request)
	if post.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want %d", post.Code, http.StatusSeeOther)
	}
	var flash *http.Cookie
	for _, cookie := range post.Result().Cookies() {
		if cookie.Name == flashCookieName {
			flash = cookie
		}
	}
	if flash == nil || flash.Value == "" || !flash.HttpOnly || flash.SameSite != http.SameSiteLaxMode {
		t.Fatal("redirect did not set a protected flash cookie")
	}

	redirect := httptest.NewRequest(http.MethodGet, "/server/privacy", nil)
	redirect.AddCookie(flash)
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, redirect)
	if !strings.Contains(get.Body.String(), `class="flash-message flash-success"`) || !strings.Contains(get.Body.String(), "Your privacy choice was saved.") {
		t.Fatal("redirect page did not render the server-side success message")
	}
	cleared := false
	for _, cookie := range get.Result().Cookies() {
		if cookie.Name == flashCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("rendering did not clear the one-time flash cookie")
	}
}

func TestFlashMessageSkipsAJAXAndUnknownKeys(t *testing.T) {
	if key, kind := flashForRedirect(httptest.NewRequest(http.MethodPost, "/server/example", nil), "/"); key != "web.flash.action_completed" || kind != "info" {
		t.Fatalf("fallback flash = %q/%q", key, kind)
	}
	if key, _ := flashForRedirect(httptest.NewRequest(http.MethodPost, "/server/contact", nil), "/server/contact?sent=1"); key != "" {
		t.Fatal("contact success page must not receive duplicate page-specific and flash messages")
	}
	for key := range flashKeys {
		if strings.Contains(key, "\"<") {
			t.Fatalf("unsafe key in flash whitelist: %q", key)
		}
	}
	called := false
	handler := flashRedirectMiddleware(&Server{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusSeeOther)
	}))
	request := httptest.NewRequest(http.MethodPost, "/server/example", strings.NewReader("a=b"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Requested-With", "fetch")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !called || len(response.Result().Cookies()) != 0 {
		t.Fatal("AJAX POST should pass through without a flash cookie")
	}
	resetHandler := flashRedirectMiddleware(&Server{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusSeeOther)
	}))
	reset := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader("action=reset"))
	reset.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resetResponse := httptest.NewRecorder()
	resetHandler.ServeHTTP(resetResponse, reset)
	if len(resetResponse.Result().Cookies()) != 0 {
		t.Fatal("preference reset must not create a new flash cookie")
	}
}
