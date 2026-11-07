package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	apppath "github.com/apimgr/gitignore/src/path"
)

// TestBannerClass verifies the status banner CSS class matches each status.
func TestBannerClass(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"healthy", "status-ok"},
		{"degraded", "status-warning"},
		{"unhealthy", "status-error"},
		{"unknown", "status-error"},
	}
	for _, c := range cases {
		if got := bannerClass(c.status); got != c.want {
			t.Errorf("bannerClass(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestStatusIcon verifies the status icon matches each status.
func TestStatusIcon(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"healthy", "✅"},
		{"degraded", "⚠️"},
		{"unhealthy", "❌"},
		{"unknown", "❌"},
	}
	for _, c := range cases {
		if got := statusIcon(c.status); got != c.want {
			t.Errorf("statusIcon(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestStatusText verifies the human-readable status text matches each status.
func TestStatusText(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"healthy", "All Systems Operational"},
		{"degraded", "Degraded Performance"},
		{"unhealthy", "Service Unavailable"},
		{"unknown", "Service Unavailable"},
	}
	for _, c := range cases {
		if got := statusText(c.status); got != c.want {
			t.Errorf("statusText(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestOverallStatus verifies status aggregation: a failed database is fatal,
// any other failed check degrades, and all-ok is healthy.
func TestOverallStatus(t *testing.T) {
	cases := []struct {
		name string
		c    ChecksInfo
		want string
	}{
		{"all ok", ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "ok"}, "healthy"},
		{"database error is fatal", ChecksInfo{Database: "error", Cache: "ok", Disk: "ok", Scheduler: "ok"}, "unhealthy"},
		{"cache error degrades", ChecksInfo{Database: "ok", Cache: "error", Disk: "ok", Scheduler: "ok"}, "degraded"},
		{"disk error degrades", ChecksInfo{Database: "ok", Cache: "ok", Disk: "error", Scheduler: "ok"}, "degraded"},
		{"scheduler error degrades", ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "error"}, "degraded"},
		{"tor error degrades", ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "ok", Tor: "error"}, "degraded"},
		{"database error wins over other errors", ChecksInfo{Database: "error", Cache: "error", Disk: "error", Scheduler: "error"}, "unhealthy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := overallStatus(c.c); got != c.want {
				t.Errorf("overallStatus(%+v) = %q, want %q", c.c, got, c.want)
			}
		})
	}
}

// TestFormatUptime verifies the compact human-readable duration formatting,
// including the negative-duration clamp to zero.
func TestFormatUptime(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0m"},
		{"negative clamps to zero", -5 * time.Minute, "0m"},
		{"minutes only", 42 * time.Minute, "42m"},
		{"hours and minutes", 3*time.Hour + 15*time.Minute, "3h 15m"},
		{"days hours minutes", 2*24*time.Hour + 5*time.Hour + 30*time.Minute, "2d 5h 30m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatUptime(c.d); got != c.want {
				t.Errorf("formatUptime(%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

// newTestHealthServer builds a Server wired to a real in-memory-on-disk store so
// the health probes (db.Ping, scheduler load, disk stat) resolve to "ok",
// yielding an overall "healthy" status for the shape assertions.
func newTestHealthServer(t *testing.T) *Server {
	t.Helper()
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pm := apppath.New()
	pm.SetDataDir(dataDir)

	return &Server{
		config: &Config{
			Version: "test",
			Paths:   pm,
			Cfg: &config.Config{
				Server: config.ServerConfig{
					Branding: config.BrandingConfig{Title: "gitignore"},
					Mode:     "production",
				},
			},
		},
		startTime: time.Now(),
		stats:     newStatsCollector(),
	}
}

// doHealth issues a GET against handleHealthz with the given path, Accept header,
// and User-Agent, returning the recorder.
func doHealth(s *Server, path, accept, userAgent string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, req)
	return rec
}

// TestHealthzJSONShape verifies the JSON rendering carries every mandated
// top-level field and reports a healthy status with a 200 (AI.md PART 13).
func TestHealthzJSONShape(t *testing.T) {
	s := newTestHealthServer(t)
	rec := doHealth(s, "/api/v1/server/healthz", "", "gitignore-cli/1.0")

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json, got %q", ct)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for healthy status, got %d\nbody:\n%s", rec.Code, rec.Body.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\nbody:\n%s", err, rec.Body.String())
	}

	required := []string{
		"project", "status", "version", "go_version", "build",
		"uptime", "mode", "timestamp", "features", "checks", "stats",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Errorf("JSON missing mandated top-level field %q", key)
		}
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot decode into HealthResponse: %v", err)
	}
	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %q (checks: %+v)", resp.Status, resp.Checks)
	}
	if resp.Checks.Database != "ok" {
		t.Errorf("expected database ok, got %q", resp.Checks.Database)
	}
	if resp.GoVersion == "" {
		t.Errorf("go_version must be populated")
	}
}

// TestHealthzTrailingNewline verifies the JSON body ends with a single newline
// (AI.md PART 13 JSON format).
func TestHealthzTrailingNewline(t *testing.T) {
	s := newTestHealthServer(t)
	rec := doHealth(s, "/api/v1/server/healthz", "", "gitignore-cli/1.0")
	body := rec.Body.String()
	if !strings.HasSuffix(body, "}\n") {
		t.Errorf("JSON body must end with a trailing newline")
	}
}

// TestHealthzContentNegotiation verifies handleHealthz picks the format from the
// Accept header and client type (AI.md PART 14 content negotiation).
func TestHealthzContentNegotiation(t *testing.T) {
	s := newTestHealthServer(t)
	cases := []struct {
		name      string
		path      string
		accept    string
		userAgent string
		wantCT    string
		needle    string
	}{
		{"browser html", "/server/healthz", "text/html", "Mozilla/5.0", "text/html", "<!DOCTYPE html>"},
		{"explicit plain", "/server/healthz", "text/plain", "", "text/plain", "project.name:"},
		{"curl plain", "/server/healthz", "", "curl/8.0.1", "text/plain", "project.name:"},
		{"api json our-cli", "/api/v1/server/healthz", "", "gitignore-cli/1.0", "application/json", `"status"`},
		{"api text accept", "/api/v1/server/healthz", "text/plain", "", "text/plain", "project.name:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doHealth(s, c.path, c.accept, c.userAgent)
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.wantCT) {
				t.Errorf("expected Content-Type %q, got %q", c.wantCT, ct)
			}
			if !strings.Contains(rec.Body.String(), c.needle) {
				t.Errorf("body missing %q\ngot:\n%s", c.needle, rec.Body.String())
			}
		})
	}
}

// TestHealthzTextSections verifies the plain-text rendering uses numbered section
// headers and dot-notation keys in canonical order (AI.md PART 13 plain text).
func TestHealthzTextSections(t *testing.T) {
	s := newTestHealthServer(t)
	rec := doHealth(s, "/server/healthz", "text/plain", "")
	body := rec.Body.String()
	for _, needle := range []string{
		"# 1. Project", "project.name:", "checks.database:", "stats.requests_total:",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("text body missing %q", needle)
		}
	}
}
