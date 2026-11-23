package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/path"
)

func TestCSPReportLogsSanitizedViolationAndReturnsNoContent(t *testing.T) {
	paths := path.New()
	paths.SetLogsDir(t.TempDir())
	srv := New(&Config{Paths: paths})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/reports/csp", strings.NewReader(`{"csp-report":{"blocked-uri":"https://cdn.example/font.woff?token=private#fragment","violated-directive":"font-src"}}`))
	request.Header.Set("Content-Type", "application/csp-report")
	recorder := httptest.NewRecorder()
	srv.handleCSPReport(recorder, request)

	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("response = %d %q, want empty 204", recorder.Code, recorder.Body.String())
	}
	log, err := os.ReadFile(filepath.Join(paths.GetLogsDir(), "security.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"security.csp_violation", "font-src", "https://cdn.example/font.woff"} {
		if !strings.Contains(string(log), expected) {
			t.Errorf("security log %q does not contain %q", log, expected)
		}
	}
	if strings.Contains(string(log), "private") || strings.Contains(string(log), "fragment") {
		t.Errorf("security log retained query or fragment data: %q", log)
	}
}

func TestCSPReportAcceptsReportingAPIEnvelope(t *testing.T) {
	paths := path.New()
	paths.SetLogsDir(t.TempDir())
	srv := New(&Config{Paths: paths})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/reports/csp", strings.NewReader(`[{"type":"csp-violation","body":{"blocked-uri":"data","effective-directive":"font-src"}}]`))
	request.Header.Set("Content-Type", "application/reports+json")
	recorder := httptest.NewRecorder()
	srv.handleCSPReport(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	log, err := os.ReadFile(filepath.Join(paths.GetLogsDir(), "security.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "font-src") {
		t.Fatalf("security log does not contain the reported directive: %q", log)
	}
}

func TestDefaultReportLogsCSPViolation(t *testing.T) {
	paths := path.New()
	paths.SetLogsDir(t.TempDir())
	srv := New(&Config{Paths: paths})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/reports/default", strings.NewReader(`[ {"type":"csp-violation","body":{"blocked-uri":"https://cdn.example/script.js?token=private","effective-directive":"script-src"}} ]`))
	recorder := httptest.NewRecorder()
	srv.handleDefaultReport(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	log, err := os.ReadFile(filepath.Join(paths.GetLogsDir(), "security.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"security.csp_violation", "script-src", "https://cdn.example/script.js"} {
		if !strings.Contains(string(log), expected) {
			t.Errorf("security log %q does not contain %q", log, expected)
		}
	}
	if strings.Contains(string(log), "private") {
		t.Errorf("security log retained a query secret: %q", log)
	}
}

func TestDefaultAndNELReportRoutesShareConfiguredLimit(t *testing.T) {
	paths := path.New()
	paths.SetLogsDir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Web.Reports.RateLimitPerMinute = 60
	cfg.Web.Reports.RateLimitPerIPBurst = 1
	srv := New(&Config{Paths: paths, Cfg: cfg})

	for _, route := range []string{"/api/v1/server/reports/csp", "/api/v1/server/reports/default", "/api/v1/server/reports/nel"} {
		request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`[ {"type":"csp-violation","body":{"blocked-uri":"https://cdn.example/script.js","effective-directive":"script-src"}} ]`))
		recorder := httptest.NewRecorder()
		srv.router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
			t.Errorf("POST %s = %d %q, want empty 204", route, recorder.Code, recorder.Body.String())
		}
	}

	log, err := os.ReadFile(filepath.Join(paths.GetLogsDir(), "security.log"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(log), "security.csp_violation"); count != 1 {
		t.Errorf("shared report burst limit allowed %d logged reports, want 1: %q", count, log)
	}
}

func TestReportRateLimiterRefillsAndBoundsLogFields(t *testing.T) {
	limiter := newReportRateLimiter()
	if !limiter.allow("192.0.2.1", 60, 1) {
		t.Fatal("first report was rejected")
	}
	if limiter.allow("192.0.2.1", 60, 1) {
		t.Fatal("report burst exceeded its configured capacity")
	}

	limiter.mu.Lock()
	bucket := limiter.buckets["192.0.2.1"]
	bucket.updated = time.Now().Add(-time.Second)
	bucket.lastSeen = time.Now()
	limiter.buckets["192.0.2.1"] = bucket
	limiter.mu.Unlock()
	if !limiter.allow("192.0.2.1", 60, 1) {
		t.Fatal("report limit did not refill one token after one second")
	}

	if got := reportLogField(strings.Repeat("x", 250), 200); len([]rune(got)) != 200 {
		t.Errorf("bounded report field length = %d, want 200", len([]rune(got)))
	}
	if got := sanitizeCSPReportURI("file:///etc/passwd"); got != "[redacted]" {
		t.Errorf("filesystem URI = %q, want redacted", got)
	}
}
