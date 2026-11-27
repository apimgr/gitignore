package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	apppath "github.com/apimgr/gitignore/src/path"
	tmpl "github.com/apimgr/gitignore/src/template"
)

func TestConfiguredAPIVersionIsUsedByRouterAndDiscovery(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_testtoken0123456789012345678901234567"
	cfg.Server.APIVersion = "v9"
	s := New(&Config{Version: "test", Cfg: cfg})

	req := httptest.NewRequest(http.MethodGet, "/api/v9/", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("configured API route status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"api_version": "v9"`) || !strings.Contains(rec.Body.String(), `"/api/v9/list"`) {
		t.Errorf("API info did not advertise configured version: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/", nil)
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("default API route status = %d, want 404 when v9 is configured", rec.Code)
	}
}

func TestAllowlistEmptyDescriptionUsesPlaceholder(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "192.0.2.1"}, {CIDR: "192.0.2.2", Description: "  "}}
	s := New(&Config{Version: "test", Cfg: cfg})
	for _, cidr := range []string{"192.0.2.1/32", "192.0.2.2/32"} {
		if got := s.allowlistSnapshot[cidr]; got != "(no description)" {
			t.Errorf("allowlist description for %s = %q, want placeholder", cidr, got)
		}
	}
}

func TestTorCircuitIDUsesLow32BitsWithoutPrefixValidation(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodGet, "http://example.onion/", nil)
	r.RemoteAddr = "[fd12:3456:789a:bcde:1111:2222:1234:5678]:9000"
	marked := s.markTorRequest(r)
	if marked.RemoteAddr != "tor:12345678" {
		t.Fatalf("Tor remote address = %q, want tor:12345678", marked.RemoteAddr)
	}
	identity, ok := marked.Context().Value(torRequestContextKey{}).(torRequestIdentity)
	if !ok || identity.circuitID != "12345678" {
		t.Fatalf("Tor identity = %+v, present=%t", identity, ok)
	}
}

func TestMCPPublicReadToolFlowAndAliases(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_testtoken0123456789012345678901234567"
	manager, err := tmpl.New()
	if err != nil {
		t.Fatal(err)
	}
	s := New(&Config{Version: "test", Cfg: cfg, Templates: manager})
	request := func(path, body, method, name string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", method)
		if name != "" {
			req.Header.Set("Mcp-Name", name)
		}
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		return rec
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"},"capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	for _, endpoint := range []string{"/api/v1/server/mcp", "/api/mcp", "/mcp"} {
		rec := request(endpoint, initialize, "initialize", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"protocolVersion":"2026-07-28"`) {
			t.Errorf("initialize %s status/body = %d %s", endpoint, rec.Code, rec.Body.String())
		}
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
	list := request("/api/v1/server/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+meta+`}}`, "tools/list", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "get_api_v1_list") {
		t.Fatalf("tools/list status/body = %d %s", list.Code, list.Body.String())
	}
	call := request("/api/v1/server/mcp", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_api_v1_list","arguments":{},`+meta+`}}`, "tools/call", "get_api_v1_list")
	if call.Code != http.StatusOK || !strings.Contains(call.Body.String(), `"isError":false`) || !strings.Contains(call.Body.String(), "Go") {
		t.Errorf("tools/call status/body = %d %s", call.Code, call.Body.String())
	}
}

func TestMCPRequiresBearerAndRejectsLegacyProtocol(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "adm_testtoken0123456789012345678901234567"
	s := New(&Config{Version: "test", Cfg: cfg})
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer invalid")
	request.Header.Set("MCP-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "initialize")
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("invalid bearer status/WWW-Authenticate = %d %q", recorder.Code, recorder.Header().Get("WWW-Authenticate"))
	}
	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	request.Header.Set("MCP-Protocol-Version", "2025-11-25")
	request.Header.Set("Mcp-Method", "initialize")
	recorder = httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "Unsupported protocol version") {
		t.Fatalf("legacy initialize status/body = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMCPRejectsInvalidOriginAndMethod(t *testing.T) {
	s := New(&Config{Version: "test", Cfg: config.DefaultConfig()})
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Host = "gitignore.example"
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("invalid Origin status = %d, want 403", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	recorder = httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET status/Allow = %d %q, want 405 POST", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestMCPOriginRequiresExplicitOrOwnOrigin(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := config.DefaultConfig()
	s := New(&Config{Version: "test", Cfg: cfg})
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Host = "gitignore.example"

	request.Header.Set("Origin", "http://gitignore.example")
	if !s.validMCPOrigin(request) {
		t.Fatal("request's own origin was rejected")
	}
	request.Header.Set("Origin", "https://attacker.example")
	if s.validMCPOrigin(request) {
		t.Fatal("wildcard CORS default authorized an unrelated MCP origin")
	}

	cfg.Server.CORS.AllowedOrigins = []string{"https://client.example"}
	request.Header.Set("Origin", "https://client.example")
	if !s.validMCPOrigin(request) {
		t.Fatal("explicit CORS origin was rejected for MCP")
	}
}

// TestBaseURLHandlerRedirectsBarePrefix verifies a request for the bare
// prefix is redirected to its trailing-slash form (AI.md PART 8 "--baseurl").
func TestBaseURLHandlerRedirectsBarePrefix(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next should not be called for the bare prefix, got path %q", r.URL.Path)
	})
	h := baseURLHandler("/app", next)

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/" {
		t.Errorf("Location = %q, want /app/", loc)
	}
}

// TestBaseURLHandlerStripsPrefix verifies non-bare requests are dispatched to
// next with the prefix stripped from the path.
func TestBaseURLHandlerStripsPrefix(t *testing.T) {
	var gotPath string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	h := baseURLHandler("/app", next)

	req := httptest.NewRequest(http.MethodGet, "/app/list", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotPath != "/list" {
		t.Errorf("stripped path = %q, want /list", gotPath)
	}
}

// TestParseCookieMaxAge verifies duration suffixes are parsed into seconds and
// invalid input falls back to the one-year default (AI.md PART 30).
func TestParseCookieMaxAge(t *testing.T) {
	const oneYear = 365 * 24 * 60 * 60
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty falls back", "", oneYear},
		{"days", "30d", 30 * 24 * 60 * 60},
		{"hours", "12h", 12 * 60 * 60},
		{"minutes", "5m", 5 * 60},
		{"seconds suffix", "90s", 90},
		{"bare seconds", "3600", 3600},
		{"unknown unit falls back", "10x", oneYear},
		{"negative falls back", "-5d", oneYear},
		{"zero falls back", "0d", oneYear},
		{"non-numeric falls back", "abc", oneYear},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseCookieMaxAge(c.in); got != c.want {
				t.Errorf("parseCookieMaxAge(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestHandleLocaleJSON verifies the default locale catalog is served as JSON.
func TestHandleLocaleJSON(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/locales/en.json", nil)
	req = withChiParam(req, "lang", "en")
	rec := httptest.NewRecorder()
	s.handleLocaleJSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty locale catalog")
	}
}

// TestHandleLocaleJSONUnsupportedFallsBack verifies an unsupported language
// falls back to the default locale rather than 404ing.
func TestHandleLocaleJSONUnsupportedFallsBack(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/locales/xx-not-real.json", nil)
	req = withChiParam(req, "lang", "xx-not-real")
	rec := httptest.NewRecorder()
	s.handleLocaleJSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	want, ok := i18n.LocaleJSON(i18n.DefaultLang)
	if !ok {
		t.Fatalf("default locale %q should exist", i18n.DefaultLang)
	}
	if rec.Body.String() != string(want) {
		t.Errorf("expected default locale catalog body")
	}
}

// TestDetectServerURLFromConfiguredFQDN verifies the configured FQDN wins when
// no trusted-proxy headers are present.
func TestDetectServerURLFromConfiguredFQDN(t *testing.T) {
	s := &Server{config: &Config{Cfg: &config.Config{Server: config.ServerConfig{FQDN: "example.com"}}}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"

	if got := s.detectServerURL(req); got != "https://example.com" {
		t.Errorf("detectServerURL() = %q, want https://example.com", got)
	}
}

// TestDetectServerURLDefaultsToRequestHost verifies the fallback to the
// request's Host header and scheme when nothing else is configured.
func TestDetectServerURLDefaultsToRequestHost(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Host = "gitignore.example"

	if got := s.detectServerURL(req); got != "http://gitignore.example" {
		t.Errorf("detectServerURL() = %q, want http://gitignore.example", got)
	}
}

// TestDetectServerURLTrustsForwardedHeadersFromTrustedPeer verifies
// X-Forwarded-* headers are honored only from a trusted proxy peer
// (AI.md PART 12 "X-Forwarded-* trust gate").
func TestDetectServerURLTrustsForwardedHeadersFromTrustedPeer(t *testing.T) {
	s := &Server{config: &Config{}}
	s.trustedProxies = buildTrustedProxies("127.0.0.1", nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "proxied.example")

	if got := s.detectServerURL(req); got != "https://proxied.example" {
		t.Errorf("detectServerURL() = %q, want https://proxied.example", got)
	}
}

// TestDetectServerURLIgnoresForwardedHeadersFromUntrustedPeer verifies a
// non-trusted peer cannot forge the proto/host used for URL construction.
func TestDetectServerURLIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	s := &Server{config: &Config{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Host = "direct.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example")

	if got := s.detectServerURL(req); got != "http://direct.example" {
		t.Errorf("detectServerURL() = %q, want http://direct.example", got)
	}
}

// TestNewBuildsRoutableServer verifies New wires up a server with middleware
// and routes mounted, so a basic request resolves.
func TestNewBuildsRoutableServer(t *testing.T) {
	mgr, err := tmpl.New()
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	pm := apppath.New()
	pm.SetDataDir(t.TempDir())

	s := New(&Config{
		Address:   "127.0.0.1",
		Port:      0,
		Templates: mgr,
		Paths:     pm,
		Version:   "test",
		Cfg:       config.DefaultConfig(),
	})
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.router == nil {
		t.Fatal("expected router to be set")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}

	localized := httptest.NewRecorder()
	s.router.ServeHTTP(localized, httptest.NewRequest(http.MethodGet, "/server/about?lang=es", nil))
	if localized.Code != http.StatusOK || !strings.Contains(localized.Body.String(), `lang="es"`) || !strings.Contains(localized.Body.String(), i18n.Translate("es", "web.handler.title.about")) {
		t.Fatalf("server router did not localize About page: status %d, body %s", localized.Code, localized.Body.String())
	}
	for _, cookie := range localized.Result().Cookies() {
		if cookie.Name == "lang" {
			t.Fatal("server persisted the language cookie without Preferences consent")
		}
	}

	consentedRequest := httptest.NewRequest(http.MethodGet, "/server/about?lang=es", nil)
	consentedRequest.AddCookie(&http.Cookie{Name: "cookie_consent", Value: encodeConsentCookie([]byte(`{"essential":true,"preferences":true,"timestamp":1800000000}`))})
	consented := httptest.NewRecorder()
	s.router.ServeHTTP(consented, consentedRequest)
	foundLanguageCookie := false
	for _, cookie := range consented.Result().Cookies() {
		if cookie.Name == "lang" && cookie.Value == "es" && cookie.MaxAge == 365*24*60*60 {
			foundLanguageCookie = true
		}
	}
	if !foundLanguageCookie {
		t.Fatal("server did not persist language selection after Preferences consent")
	}
}

// TestNewMountsBaseURLPrefix verifies a non-root baseurl prefix is mounted so
// requests must include the prefix to reach the router.
func TestNewMountsBaseURLPrefix(t *testing.T) {
	mgr, err := tmpl.New()
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	pm := apppath.New()
	pm.SetDataDir(t.TempDir())

	cfg := &config.Config{}
	cfg.Server.BaseURL = "/app"
	s := New(&Config{
		Address:   "127.0.0.1",
		Port:      0,
		Templates: mgr,
		Paths:     pm,
		Version:   "test",
		Cfg:       cfg,
	})

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /app/ = %d, want 200", rec.Code)
	}
}

// TestShutdownOnUnstartedServer verifies Shutdown returns cleanly on a server
// that was never started (net/http.Server.Shutdown is safe to call in that
// case).
func TestShutdownOnUnstartedServer(t *testing.T) {
	s := &Server{server: &http.Server{Addr: "127.0.0.1:0"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestOpenAPIAndMCPTextUseRequestLanguage(t *testing.T) {
	s := New(&Config{Version: "test", Cfg: config.DefaultConfig()})
	var spec map[string]interface{}
	openAPIHandler := i18n.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spec = s.openAPISpec(r)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/swagger", nil)
	request.Header.Set("Accept-Language", "es")
	openAPIHandler.ServeHTTP(httptest.NewRecorder(), request)

	info := spec["info"].(map[string]interface{})
	wantTitle := i18n.Translate("es", "swagger.openapi.info.title")
	if info["title"] != wantTitle || wantTitle == i18n.Translate("en", "swagger.openapi.info.title") {
		t.Errorf("OpenAPI title = %v, want Spanish translation %q", info["title"], wantTitle)
	}
	paths := spec["paths"].(map[string]interface{})
	list := paths["/api/v1/list"].(map[string]interface{})["get"].(map[string]interface{})
	wantSummary := i18n.Translate("es", "swagger.openapi.operation.templates_list")
	if list["summary"] != wantSummary || wantSummary == i18n.Translate("en", "swagger.openapi.operation.templates_list") {
		t.Errorf("OpenAPI summary = %v, want Spanish translation %q", list["summary"], wantSummary)
	}

	var tools []mcpTool
	toolsHandler := i18n.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tools = s.mcpToolsForRequest(r)
	}))
	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/mcp", nil)
	request.Header.Set("Accept-Language", "es")
	toolsHandler.ServeHTTP(httptest.NewRecorder(), request)
	wantDescription := i18n.Translate("es", "swagger.openapi.operation.templates_list")
	for _, tool := range tools {
		if tool.Name == "get_api_v1_list" {
			if tool.Description != wantDescription || wantDescription == i18n.Translate("en", "swagger.openapi.operation.templates_list") {
				t.Errorf("MCP tool description = %q, want Spanish translation %q", tool.Description, wantDescription)
			}
			return
		}
	}
	t.Fatal("localized MCP tools did not include get_api_v1_list")
}

func TestMCPErrorUsesRequestLanguage(t *testing.T) {
	s := New(&Config{Version: "test", Cfg: config.DefaultConfig()})
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Accept-Language", "es")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("unauthorized MCP status/header = %d/%q", recorder.Code, recorder.Header().Get("WWW-Authenticate"))
	}
	var response struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode MCP error response: %v", err)
	}
	want := i18n.Translate("es", "mcp.errors.unauthorized")
	if response.Error.Code != -32001 || response.Error.Message != want || want == i18n.Translate("en", "mcp.errors.unauthorized") {
		t.Fatalf("MCP error = %#v, want localized code -32001 and message %q", response.Error, want)
	}
}
