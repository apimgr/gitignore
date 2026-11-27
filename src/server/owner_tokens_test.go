package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/go-chi/chi/v5"
)

func TestOwnerTokenCookieNamesMatchIDEA(t *testing.T) {
	idea, err := os.ReadFile("../../IDEA.md")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(idea)
	if !strings.Contains(contents, "\nowner_token: "+ownerTokenCookieName+"\n") {
		t.Fatalf("IDEA.md does not record owner cookie name %q", ownerTokenCookieName)
	}
	if !strings.Contains(contents, "\nowner_token_legacy: "+legacyOwnerTokenCookieName+"\n") {
		t.Fatalf("IDEA.md does not record legacy owner cookie name %q", legacyOwnerTokenCookieName)
	}
	if ownerTokenViewCookieName != "gitignore_token_view" {
		t.Fatalf("token view cookie name = %q, want project-scoped name", ownerTokenViewCookieName)
	}
}

func TestOwnerTokenIssueAuthenticateAndEncrypt(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_test_operator_token"
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	s := New(&Config{Cfg: cfg})
	primaryRaw, primary, err := s.issueOwnerToken(time.Now().UTC(), "api", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(primaryRaw) != 36 || !strings.HasPrefix(primaryRaw, "own_") {
		t.Fatalf("invalid primary token format %q", primaryRaw)
	}
	identity, err := s.AuthenticateBearerToken(primaryRaw)
	if err != nil || identity.Kind != "primary" || identity.OwnerID != primary.OwnerID {
		t.Fatalf("primary identity = %+v, err=%v", identity, err)
	}
	operator, err := s.AuthenticateBearerToken(cfg.Server.Token)
	if err != nil || operator.Kind != "operator" {
		t.Fatalf("operator identity = %+v, err=%v", operator, err)
	}
	if _, err := s.AuthenticateBearerToken("own_invalid"); err == nil {
		t.Fatal("invalid token was accepted")
	}
	linkedRaw, linked, err := s.issueLinkedToken(primary.OwnerID, "automation", nil, time.Now().UTC(), "api", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	linkedIdentity, err := s.AuthenticateBearerToken(linkedRaw)
	if err != nil || linkedIdentity.Kind != "linked" || linkedIdentity.OwnerID != primary.OwnerID {
		t.Fatalf("linked identity = %+v, err=%v", linkedIdentity, err)
	}
	request := httptest.NewRequest(http.MethodPost, "/server/preferences/tokens", nil)
	request.Header.Set("Authorization", "Bearer "+linkedRaw)
	if _, ok := s.requirePrimaryOwner(httptest.NewRecorder(), request, true); ok {
		t.Fatal("linked token was allowed to manage tokens")
	}
	decrypted, err := s.decryptOwnerToken(primary.TokenEnc)
	if err != nil || decrypted != primaryRaw {
		t.Fatalf("decrypted primary token = %q, err=%v", decrypted, err)
	}
	if got := s.publicToken(linked, time.Now()).Masked; got == linkedRaw || !strings.HasSuffix(got, linkedRaw[len(linkedRaw)-4:]) {
		t.Fatalf("public token mask = %q", got)
	}
	if got := maskOwnerToken("invalid"); !strings.HasPrefix(got, "own_") {
		t.Fatalf("invalid token mask = %q", got)
	}
	ownerID, err := newOwnerID()
	if err != nil || len(ownerID) != 36 || strings.Count(ownerID, "-") != 4 {
		t.Fatalf("owner ID = %q, err=%v", ownerID, err)
	}
	if s.maxLinkedTokens() != 25 {
		t.Fatalf("default linked-token limit = %d", s.maxLinkedTokens())
	}
	if _, err := s.loadTokenPage(primary.OwnerID, nil, ""); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerCookieMiddlewareAndTokenViewCookie(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	s := New(&Config{Cfg: cfg})
	raw, item, err := s.issueOwnerToken(time.Now().UTC(), "api", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	middleware := s.ownerCookieMiddleware(next)
	request := httptest.NewRequest(http.MethodGet, "/server/preferences", nil)
	request.AddCookie(&http.Cookie{Name: ownerTokenCookieName, Value: raw})
	recorder := httptest.NewRecorder()
	middleware.ServeHTTP(recorder, request)
	if !called || len(recorder.Result().Cookies()) == 0 {
		t.Fatal("valid owner cookie was not refreshed")
	}
	called = false
	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/tokens", nil)
	request.AddCookie(&http.Cookie{Name: ownerTokenCookieName, Value: raw})
	recorder = httptest.NewRecorder()
	middleware.ServeHTTP(recorder, request)
	if !called || len(recorder.Result().Cookies()) != 0 {
		t.Fatal("API request used ambient owner cookie")
	}
	request = httptest.NewRequest(http.MethodGet, "/server/preferences", nil)
	request.AddCookie(&http.Cookie{Name: ownerTokenCookieName, Value: "own_invalid"})
	recorder = httptest.NewRecorder()
	middleware.ServeHTTP(recorder, request)
	if len(recorder.Result().Cookies()) == 0 || recorder.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("invalid owner cookie was not cleared")
	}
	viewCookieResponse := httptest.NewRecorder()
	if err := setOwnerTokenViewCookie(s, viewCookieResponse, httptest.NewRequest(http.MethodPost, "/", nil), item.OwnerID, "primary"); err != nil {
		t.Fatal(err)
	}
	viewCookies := viewCookieResponse.Result().Cookies()
	if len(viewCookies) != 1 || viewCookies[0].Name != ownerTokenViewCookieName || viewCookies[0].Path != "/server/preferences/tokens" || viewCookies[0].MaxAge != 60 || !viewCookies[0].HttpOnly {
		t.Fatalf("token view cookie = %#v", viewCookies)
	}
	if strings.Contains(viewCookies[0].Value, raw) {
		t.Fatal("token view cookie contains the raw token")
	}
	wrongOwnerRequest := httptest.NewRequest(http.MethodGet, "/server/preferences/tokens", nil)
	wrongOwnerRequest.AddCookie(viewCookies[0])
	if card := readOwnerTokenViewCookie(s, httptest.NewRecorder(), wrongOwnerRequest, "own_another_owner"); card != "" {
		t.Fatalf("token view cookie accepted for another owner: %q", card)
	}
	viewRequest := httptest.NewRequest(http.MethodGet, "/server/preferences/tokens", nil)
	viewRequest.AddCookie(&http.Cookie{Name: ownerTokenCookieName, Value: raw})
	viewRequest.AddCookie(viewCookies[0])
	viewResponse := httptest.NewRecorder()
	s.handleOwnerTokensPage(viewResponse, viewRequest)
	if viewResponse.Code != http.StatusOK || !strings.Contains(viewResponse.Body.String(), raw) || viewResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("one-use token view status=%d cache=%q body=%s", viewResponse.Code, viewResponse.Header().Get("Cache-Control"), viewResponse.Body.String())
	}
	cleared := false
	for _, cookie := range viewResponse.Result().Cookies() {
		if cookie.Name == ownerTokenViewCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("token view cookie was not cleared after use")
	}

	pageRequest := httptest.NewRequest(http.MethodGet, "/server/preferences/tokens", nil)
	pageRequest.AddCookie(&http.Cookie{Name: ownerTokenCookieName, Value: raw})
	pageResponse := httptest.NewRecorder()
	s.handleOwnerTokensPage(pageResponse, pageRequest)
	if pageResponse.Code != http.StatusOK || !strings.Contains(pageResponse.Body.String(), "Owner tokens") || !strings.Contains(pageResponse.Body.String(), `id="revoke-primary"`) || !strings.Contains(pageResponse.Body.String(), raw[len(raw)-4:]) || strings.Contains(pageResponse.Body.String(), raw) {
		t.Fatalf("owner token page status=%d body=%s", pageResponse.Code, pageResponse.Body.String())
	}
	csrf := ensureCSRFToken(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	revokeRequest := httptest.NewRequest(http.MethodPost, "/server/preferences/token/revoke", strings.NewReader(fmt.Sprintf("csrf_token=%s&confirm=%s", csrf, raw[len(raw)-4:])))
	revokeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	revokeRequest.AddCookie(&http.Cookie{Name: ownerTokenCookieName, Value: raw})
	revokeRequest.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	revokeResponse := httptest.NewRecorder()
	s.handleRevokePrimaryOwnerTokenWeb(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusSeeOther || revokeResponse.Header().Get("Clear-Site-Data") != `"cache", "storage"` {
		t.Fatalf("primary revoke status=%d headers=%v body=%s", revokeResponse.Code, revokeResponse.Header(), revokeResponse.Body.String())
	}
	if _, err := s.AuthenticateBearerToken(raw); err == nil {
		t.Fatal("primary revoke left the owner token valid")
	}
}

func TestOwnerCookieMiddlewareMigratesLegacyCookie(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	s := New(&Config{Cfg: cfg})
	raw, _, err := s.issueOwnerToken(time.Now().UTC(), "api", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/server/preferences/tokens", nil)
	request.AddCookie(&http.Cookie{Name: legacyOwnerTokenCookieName, Value: raw})
	response := httptest.NewRecorder()
	s.ownerCookieMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, request)
	cookies := response.Result().Cookies()
	var migrated, expiredLegacy bool
	for _, cookie := range cookies {
		if cookie.Name == ownerTokenCookieName && cookie.Value == raw && cookie.MaxAge > 0 {
			migrated = true
		}
		if cookie.Name == legacyOwnerTokenCookieName && cookie.MaxAge < 0 {
			expiredLegacy = true
		}
	}
	if !migrated || !expiredLegacy {
		t.Fatalf("legacy cookie migration missing: migrated=%v expiredLegacy=%v cookies=%v", migrated, expiredLegacy, cookies)
	}
}

func TestOwnerTokenAPIIssueListLinkedActionsAndRotate(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	s := New(&Config{Cfg: cfg})
	s.strictTokenLimiter = newRateLimiter(100, 60)

	response := httptest.NewRecorder()
	s.handleAPIPrimaryOwnerToken(response, httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil))
	if response.Code != http.StatusCreated {
		t.Fatalf("issue primary status=%d body=%s", response.Code, response.Body.String())
	}
	var primaryResponse struct {
		Data struct {
			OwnerToken string `json:"owner_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &primaryResponse); err != nil {
		t.Fatal(err)
	}
	primaryRaw := primaryResponse.Data.OwnerToken
	if len(primaryRaw) != 36 {
		t.Fatalf("issued primary token has invalid length: %q", primaryRaw)
	}
	conflictRequest := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil)
	conflictRequest.Header.Set("Authorization", "Bearer "+primaryRaw)
	conflictResponse := httptest.NewRecorder()
	s.handleAPIPrimaryOwnerToken(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("second primary issue status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/tokens", nil)
	listRequest.Header.Set("Authorization", "Bearer "+primaryRaw)
	response = httptest.NewRecorder()
	s.handleAPIOwnerTokens(response, listRequest)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), primaryRaw) {
		t.Fatalf("token list exposed raw primary or failed: %d %s", response.Code, response.Body.String())
	}

	linkedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens", bytes.NewBufferString(`{"name":"CI","expiry":"30d"}`))
	linkedRequest.Header.Set("Authorization", "Bearer "+primaryRaw)
	linkedRequest.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	s.handleAPIOwnerTokens(response, linkedRequest)
	if response.Code != http.StatusCreated {
		t.Fatalf("create linked status=%d body=%s", response.Code, response.Body.String())
	}
	var linkedResponse struct {
		Data struct {
			Value string `json:"value"`
			Token struct {
				ID int64 `json:"id"`
			} `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &linkedResponse); err != nil {
		t.Fatal(err)
	}
	linkedRaw, linkedID := linkedResponse.Data.Value, linkedResponse.Data.Token.ID
	if linkedID <= 0 || len(linkedRaw) != 36 {
		t.Fatalf("linked token response = %+v", linkedResponse.Data)
	}
	linkedPath := fmt.Sprintf("/api/v1/server/preferences/tokens/%d/reveal", linkedID)
	linkedAction := func(method, path string, body io.Reader) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, body)
		request.Header.Set("Authorization", "Bearer "+primaryRaw)
		request.Header.Set("Content-Type", "application/json")
		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("id", fmt.Sprint(linkedID))
		request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
		recorder := httptest.NewRecorder()
		s.handleAPILinkedTokenAction(recorder, request)
		return recorder
	}
	response = linkedAction(http.MethodPost, linkedPath, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), linkedRaw) {
		t.Fatalf("linked reveal status=%d body=%s", response.Code, response.Body.String())
	}
	response = linkedAction(http.MethodPatch, fmt.Sprintf("/api/v1/server/preferences/tokens/%d", linkedID), bytes.NewBufferString(`{"name":"CI runner","expiry":"never"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("linked update status=%d body=%s", response.Code, response.Body.String())
	}
	response = linkedAction(http.MethodDelete, fmt.Sprintf("/api/v1/server/preferences/tokens/%d", linkedID), nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status": "revoked"`) {
		t.Fatalf("linked revoke status=%d body=%s", response.Code, response.Body.String())
	}

	rotateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary/rotate", nil)
	rotateRequest.Header.Set("Authorization", "Bearer "+primaryRaw)
	response = httptest.NewRecorder()
	s.handleAPIPrimaryTokenAction(response, rotateRequest)
	if response.Code != http.StatusOK {
		t.Fatalf("primary rotate status=%d body=%s", response.Code, response.Body.String())
	}
	var rotateResponse struct {
		Data struct {
			Value string `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rotateResponse); err != nil {
		t.Fatal(err)
	}
	if rotateResponse.Data.Value == primaryRaw {
		t.Fatal("primary rotation did not replace the token")
	}
	if _, err := s.AuthenticateBearerToken(rotateResponse.Data.Value); err != nil {
		t.Fatalf("rotated primary token is not valid: %v", err)
	}
	if _, err := s.AuthenticateBearerToken(primaryRaw); err == nil {
		t.Fatal("old primary token remained valid after rotation")
	}
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/server/preferences/tokens/primary", strings.NewReader(`{"confirm":"wrong"}`))
	revokeRequest.Header.Set("Authorization", "Bearer "+rotateResponse.Data.Value)
	revokeRequest.Header.Set("Content-Type", "application/json")
	revokeResponse := httptest.NewRecorder()
	s.handleAPIPrimaryOwnerRevoke(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid primary revoke status=%d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}
	revokeRequest = httptest.NewRequest(http.MethodDelete, "/api/v1/server/preferences/tokens/primary", strings.NewReader(fmt.Sprintf(`{"confirm":%q}`, rotateResponse.Data.Value[len(rotateResponse.Data.Value)-4:])))
	revokeRequest.Header.Set("Authorization", "Bearer "+rotateResponse.Data.Value)
	revokeRequest.Header.Set("Content-Type", "application/json")
	revokeResponse = httptest.NewRecorder()
	s.handleAPIPrimaryOwnerRevoke(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK || !strings.Contains(revokeResponse.Body.String(), `"resources": 0`) {
		t.Fatalf("primary API revoke status=%d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}
}

func TestOwnerTokenExpiryOptions(t *testing.T) {
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	for _, option := range []string{"", "never"} {
		if got, err := parseOwnerTokenExpiry(option, "", now); err != nil || got != nil {
			t.Errorf("expiry %q = %v, err=%v; want no expiry", option, got, err)
		}
	}
	for option, days := range map[string]time.Duration{"7d": 7, "30d": 30, "90d": 90, "1y": 365} {
		got, err := parseOwnerTokenExpiry(option, "", now)
		if err != nil || got == nil || time.Unix(*got, 0).Sub(now) != days*24*time.Hour {
			t.Errorf("expiry %q = %v, err=%v", option, got, err)
		}
	}
	custom, err := parseOwnerTokenExpiry("custom", "2026-11-23", now)
	if err != nil || custom == nil || time.Unix(*custom, 0).UTC().Format(time.RFC3339) != "2026-11-23T23:59:59Z" {
		t.Errorf("custom expiry = %v, err=%v", custom, err)
	}
	for _, date := range []string{"2026-11-22", "invalid"} {
		if _, err := parseOwnerTokenExpiry("custom", date, now); err == nil {
			t.Errorf("invalid custom expiry %q was accepted", date)
		}
	}
	if _, err := parseOwnerTokenExpiry("2d", "", now); err == nil {
		t.Fatal("unsupported expiry was accepted")
	}
}
