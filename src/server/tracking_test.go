package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
