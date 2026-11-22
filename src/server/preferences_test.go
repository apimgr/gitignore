package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestPreferenceExportAndAdditiveImport(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "http://gitignore.example/server/preferences/sync", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	req.AddCookie(&http.Cookie{Name: preferencePrefix + "sort", Value: "name"})
	req.AddCookie(&http.Cookie{Name: "cookie_consent", Value: "private"})
	export := s.preferenceExport(req)
	parsed, err := url.Parse(export.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("theme"); got != "light" {
		t.Fatalf("export theme = %q", got)
	}
	if got := parsed.Query().Get("lang"); got != "fr" {
		t.Fatalf("export language = %q", got)
	}
	if parsed.Query().Get(preferencePrefix+"sort") != "name" || parsed.Query().Has("cookie_consent") {
		t.Fatalf("export query contains wrong preferences: %s", parsed.RawQuery)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(export.Code)
	if err != nil || string(decoded) != parsed.RawQuery {
		t.Fatalf("export code does not encode canonical query: %q, %v", decoded, err)
	}

	importReq := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?theme=dark&code="+url.QueryEscape(export.Code), nil)
	importReq.AddCookie(&http.Cookie{Name: preferencePrefix + "keep", Value: "existing"})
	recorder := httptest.NewRecorder()
	state := s.applyPreferences(recorder, importReq, preferenceInput(importReq, false, false), false)
	if state.Theme != "dark" || state.Lang != "fr" || state.Prefs["sort"] != "name" || state.Prefs["keep"] != "existing" {
		t.Fatalf("unexpected imported preference state: %+v", state)
	}
	if got := recorder.Result().Header.Values("Set-Cookie"); len(got) != 3 {
		t.Fatalf("import set %d cookies, want theme, lang and sort: %v", len(got), got)
	}
}

func TestPreferenceImportIgnoresInvalidAndExcludedValues(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?theme=neon&lang=xx-YY&cookie_consent=forged&gitignore_pref_sort=name", nil)
	recorder := httptest.NewRecorder()
	values := preferenceInput(req, false, false)
	state := s.applyPreferences(recorder, req, values, false)
	if state.Theme != "dark" || state.Lang != "en" {
		t.Fatalf("invalid values changed defaults: %+v", state)
	}
	if strings.Join(state.Ignored, ",") != "cookie_consent,lang,theme" {
		t.Fatalf("ignored = %v", state.Ignored)
	}
}

func TestPreferenceCodeAcceptsQueryURLAndBothBase64Forms(t *testing.T) {
	query := "lang=fr&theme=light"
	padded := base64.URLEncoding.EncodeToString([]byte(query))
	short := base64.RawURLEncoding.EncodeToString([]byte(query))
	for name, code := range map[string]string{
		"query":    query,
		"full URL": "https://gitignore.example/server/preferences/sync/import?" + query,
		"padded":   padded,
		"unpadded": short,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?code="+url.QueryEscape(code), nil)
			values := preferenceInput(req, false, false)
			if values.Get("lang") != "fr" || values.Get("theme") != "light" {
				t.Fatalf("decoded values = %v", values)
			}
		})
	}
}

func TestPreferenceSaveCodeOverridesSubmittedValues(t *testing.T) {
	code := base64.RawURLEncoding.EncodeToString([]byte("theme=light"))
	req := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader("theme=dark&code="+url.QueryEscape(code)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	values := preferenceInput(req, true, true)
	if values.Get("theme") != "light" {
		t.Fatalf("code did not override form value: %v", values)
	}
}

func TestPreferenceResetExpiresOnlySitePreferences(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
	for _, name := range []string{"theme", "lang", "cookie_consent", "ccpa_opt_out", "gitignore_build", preferencePrefix + "sort", "csrf_token", "owner_token"} {
		req.AddCookie(&http.Cookie{Name: name, Value: "present"})
	}
	recorder := httptest.NewRecorder()
	s.applyPreferences(recorder, req, nil, true)
	if recorder.Result().Header.Get("Clear-Site-Data") != `"storage"` {
		t.Fatalf("Clear-Site-Data = %q", recorder.Result().Header.Get("Clear-Site-Data"))
	}
	seen := map[string]bool{}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.MaxAge >= 0 || cookie.Value != "" {
			t.Errorf("cookie %q was not expired: %+v", cookie.Name, cookie)
		}
		seen[cookie.Name] = true
	}
	for _, name := range []string{"theme", "lang", "cookie_consent", "ccpa_opt_out", "gitignore_build", preferencePrefix + "sort"} {
		if !seen[name] {
			t.Errorf("reset did not expire %q", name)
		}
	}
	if seen["csrf_token"] || seen["owner_token"] {
		t.Fatal("reset expired a credential cookie")
	}
}

func TestPreferenceAPISaveRejectsNoValidInput(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences", strings.NewReader(`{"theme":"neon"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.handleAPIPreferencesSave(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid save status = %d", recorder.Code)
	}
}

func TestPreferenceAPIImportReturnsJSONArrays(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/sync/import?theme=bad&cookie_consent=forged", nil)
	recorder := httptest.NewRecorder()
	s.handleAPIPreferencesSyncImport(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Result().Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status/cache = %d/%q", recorder.Code, recorder.Result().Header.Get("Cache-Control"))
	}
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"applied", "ignored"} {
		if _, ok := envelope.Data[key].([]interface{}); !ok {
			t.Fatalf("%s must be a JSON array: %#v", key, envelope.Data[key])
		}
	}
}

func TestPreferenceAPIReadReturnsOnlyCurrentState(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	req.AddCookie(&http.Cookie{Name: "gitignore_pref_sort", Value: "name"})
	recorder := httptest.NewRecorder()
	s.handleAPIPreferences(recorder, req)
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 3 || envelope.Data["theme"] != "light" || envelope.Data["lang"] != "en" {
		t.Fatalf("unexpected current preference response: %#v", envelope.Data)
	}
	prefs, ok := envelope.Data["prefs"].(map[string]interface{})
	if !ok || prefs["sort"] != "name" {
		t.Fatalf("unexpected prefs object: %#v", envelope.Data["prefs"])
	}
}

func TestPreferenceSyncPageRendersExportAndImportForm(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "http://gitignore.example/server/preferences/sync", nil)
	recorder := httptest.NewRecorder()
	s.handlePreferencesSyncPage(recorder, req)
	body := recorder.Body.String()
	for _, expected := range []string{"/server/preferences/sync/import?", `name="code"`, `action="/server/preferences/sync"`, `data-copy="preference-url"`, `data-copy="preference-code"`, `class="breadcrumb"`, `class="container container-sm"`} {
		if !strings.Contains(body, expected) {
			t.Errorf("sync page does not contain %q", expected)
		}
	}
}

func TestPreferenceImportSecureCookieUsesTrustedPublicScheme(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TrustedProxies.Additional = []string{"127.0.0.1/32"}
	s := newTestPageServer()
	s.config.Cfg = cfg
	s.trustedProxies = buildTrustedProxies("127.0.0.1:8080", cfg.Server.TrustedProxies.Additional)
	req := httptest.NewRequest(http.MethodGet, "http://gitignore.example/server/preferences/sync/import?theme=light", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	values := preferenceInput(req, false, false)
	s.applyPreferences(recorder, req, values, false)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "theme" || !cookies[0].Secure {
		t.Fatalf("secure cookie attributes = %+v", cookies)
	}
}

func TestPreferenceWebImportRedirectRejectsExternalTarget(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?theme=light&redirect=%2F%2Fevil.example", nil)
	recorder := httptest.NewRecorder()
	s.handlePreferencesSyncImport(recorder, req)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/" || recorder.Body.Len() != 0 || recorder.Result().Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("redirect response = status %d, location %q, body %q, cache %q", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String(), recorder.Result().Header.Get("Cache-Control"))
	}
}
