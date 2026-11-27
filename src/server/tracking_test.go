package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestConsentCookieUsesPercentEncodedJSON(t *testing.T) {
	want := consentState{Essential: true, Preferences: true, Analytics: false, Timestamp: 1_800_000_000}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeConsentCookie(raw)
	if encoded == string(raw) {
		t.Fatal("consent JSON was not percent-encoded")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "cookie_consent", Value: encoded})
	got, ok := readConsent(req)
	if !ok || got != want {
		t.Fatalf("readConsent() = %#v, %v, want %#v, true", got, ok, want)
	}
}

func TestReadConsentRejectsMalformedPercentEncoding(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "cookie_consent=%zz")
	if _, ok := readConsent(req); ok {
		t.Fatal("malformed consent cookie was accepted")
	}
}

func TestConsentWithdrawalClearsStoredCategoryAndSiteData(t *testing.T) {
	s := &Server{config: &Config{Cfg: config.DefaultConfig()}}
	req := httptest.NewRequest(http.MethodPost, "/server/consent", nil)
	for _, cookie := range []*http.Cookie{
		{Name: "theme", Value: "light"},
		{Name: "lang", Value: "fr"},
		{Name: "gitignore_pref_density", Value: "compact"},
		{Name: ownerTokenCookieName, Value: "credential"},
	} {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	s.applyConsentWithdrawal(rec, req, consentState{Preferences: true, Analytics: true}, consentState{}, false)
	if got := rec.Header().Get("Clear-Site-Data"); got != `"cache", "storage"` {
		t.Fatalf("Clear-Site-Data = %q", got)
	}
	if strings.Contains(rec.Header().Get("Clear-Site-Data"), "cookies") {
		t.Fatal("consent withdrawal must preserve cookies")
	}
	expired := map[string]bool{}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 {
			expired[cookie.Name] = true
		}
	}
	for _, name := range []string{"theme", "lang", "gitignore_pref_density"} {
		if !expired[name] {
			t.Errorf("withdrawn preference cookie %q was not expired", name)
		}
	}
	if expired[ownerTokenCookieName] {
		t.Fatal("consent withdrawal expired the credential cookie")
	}
}
