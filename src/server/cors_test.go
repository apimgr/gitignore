package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

func TestCORSMiddlewareHonorsConfiguredOriginAndCredentials(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := config.DefaultConfig()
	cfg.Server.CORS.AllowedOrigins = []string{"https://client.example"}
	s := New(&Config{Cfg: cfg})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/list", nil)
	request.Header.Set("Origin", "https://client.example")
	recorder := httptest.NewRecorder()
	s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://client.example" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q, want true", got)
	}
}

func TestCORSMiddlewareWildcardOmitsCredentials(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := config.DefaultConfig()
	cfg.Server.CORS.AllowedOrigins = []string{"*"}
	s := New(&Config{Cfg: cfg})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/list", nil)
	request.Header.Set("Origin", "https://client.example")
	recorder := httptest.NewRecorder()
	s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q, want wildcard", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow-credentials = %q, want omitted for wildcard", got)
	}
}

func TestCORSMiddlewareExplicitEmptyOriginDisablesCORSHeaders(t *testing.T) {
	t.Setenv("DOMAIN", "enabled.example")
	cfg := config.DefaultConfig()
	cfg.Server.CORS.AllowedOrigins = []string{""}
	s := New(&Config{Cfg: cfg})
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/list", nil)
	request.Header.Set("Origin", "https://client.example")
	recorder := httptest.NewRecorder()
	s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { t.Fatal("preflight reached next handler") })).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", recorder.Code)
	}
	for name := range recorder.Header() {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			t.Errorf("disabled CORS emitted %s", name)
		}
	}
}

func TestCORSMiddlewareLearnsOriginFromTrustedProxy(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := config.DefaultConfig()
	cfg.Server.CORS.AllowedOrigins = []string{}
	s := New(&Config{Cfg: cfg})
	s.trustedProxies = buildTrustedProxies("127.0.0.1", nil)
	learn := httptest.NewRequest(http.MethodGet, "/", nil)
	learn.RemoteAddr = "127.0.0.1:54321"
	learn.Header.Set("X-Forwarded-Host", "learned.example")
	learn.Header.Set("X-Forwarded-Proto", "https")
	_, _, _, _ = s.corsOriginAllowed(learn, "")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/list", nil)
	request.Header.Set("Origin", "https://learned.example")
	allowed, _, _, _ := s.corsOriginAllowed(request, request.Header.Get("Origin"))
	if !allowed {
		t.Fatal("trusted proxy origin was not learned")
	}
}

func TestCORSLearnedOriginsAreBounded(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(&Config{Cfg: cfg})
	s.trustedProxies = buildTrustedProxies("127.0.0.1", nil)
	for index := 0; index < corsLearnedOriginLimit+20; index++ {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = "127.0.0.1:54321"
		request.Header.Set("X-Forwarded-Host", fmt.Sprintf("host-%d.example", index))
		request.Header.Set("X-Forwarded-Proto", "https")
		s.learnForwardedOrigin(request)
	}
	if got := len(s.learnedCORSOrigins(time.Now())); got > corsLearnedOriginLimit {
		t.Fatalf("learned origin count = %d, limit = %d", got, corsLearnedOriginLimit)
	}
}
