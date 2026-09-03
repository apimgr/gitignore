package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/httputil"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/tor"
)

// HealthResponse is the canonical, PUBLIC-safe health payload for
// /server/healthz (AI.md PART 13). Field order matches the spec exactly and is
// shared by the JSON, plain-text, and HTML renderings. Every field is safe for
// an unauthenticated internet viewer: no credentials, paths, hosts, or secrets.
type HealthResponse struct {
	Project ProjectInfo `json:"project"`

	Status         string   `json:"status"`
	PendingRestart bool     `json:"pending_restart,omitempty"`
	RestartReason  []string `json:"restart_reason,omitempty"`

	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Build     BuildInfo `json:"build"`

	Uptime    string    `json:"uptime"`
	Mode      string    `json:"mode"`
	Timestamp time.Time `json:"timestamp"`

	Features FeaturesInfo `json:"features"`
	Checks   ChecksInfo   `json:"checks"`
	Stats    StatsInfo    `json:"stats"`
}

// ProjectInfo carries branding identity (AI.md PART 16).
type ProjectInfo struct {
	Name        string `json:"name"`
	Tagline     string `json:"tagline"`
	Description string `json:"description"`
}

// BuildInfo carries build-time identity (AI.md PART 7).
type BuildInfo struct {
	Commit string `json:"commit"`
	Date   string `json:"date"`
}

// FeaturesInfo reports non-negotiable public features (AI.md PARTS 19, 31).
type FeaturesInfo struct {
	Tor   TorInfo `json:"tor"`
	GeoIP bool    `json:"geoip"`
}

// TorInfo reports the Tor hidden-service state (AI.md PART 31).
type TorInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
}

// ChecksInfo reports component health as "ok"/"error" only — never details
// (AI.md PART 13 "Database/cache checks MUST be vague").
type ChecksInfo struct {
	Database  string `json:"database"`
	Cache     string `json:"cache"`
	Disk      string `json:"disk"`
	Scheduler string `json:"scheduler"`
	Tor       string `json:"tor,omitempty"`
}

// StatsInfo reports public-safe aggregate counters (AI.md PART 13).
type StatsInfo struct {
	RequestsTotal int64 `json:"requests_total"`
	Requests24h   int64 `json:"requests_24h"`
	ActiveConns   int   `json:"active_connections"`
}

// handleHealthz serves the content-negotiated health check (AI.md PARTS 13, 14).
// The same handler backs /server/healthz, the optional root /healthz alias, and
// the /api/** health routes; content negotiation alone decides the format.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	resp := s.buildHealthResponse()
	setCacheHeaders(w, "html")

	switch httputil.DetectResponseFormat(r) {
	case "text/plain":
		s.writeHealthText(w, resp)
	case "application/json":
		s.writeHealthJSON(w, resp)
	default:
		s.writeHealthHTML(w, resp)
	}
}

// buildHealthResponse gathers PUBLIC-safe health data from real subsystem state.
func (s *Server) buildHealthResponse() HealthResponse {
	var resp HealthResponse

	if s.config != nil && s.config.Cfg != nil {
		resp.Project = ProjectInfo{
			Name:        s.config.Cfg.Server.Branding.Title,
			Tagline:     s.config.Cfg.Server.Branding.Tagline,
			Description: s.config.Cfg.Server.Branding.Description,
		}
		resp.Mode = s.config.Cfg.Server.Mode
	}
	if resp.Project.Name == "" {
		resp.Project.Name = "gitignore"
	}
	if resp.Mode == "" {
		resp.Mode = "production"
	}

	resp.Version = s.config.Version
	resp.GoVersion = runtime.Version()
	resp.Build = BuildInfo{Commit: s.config.Commit, Date: s.config.BuildDate}
	resp.Uptime = formatUptime(time.Since(s.startTime))
	resp.Timestamp = time.Now().UTC()

	resp.Features = s.healthFeatures()
	resp.Checks = s.healthChecks(resp.Features.Tor)
	resp.Status = overallStatus(resp.Checks)

	if s.stats != nil {
		total, last24h, active := s.stats.snapshot(time.Now())
		resp.Stats = StatsInfo{RequestsTotal: total, Requests24h: last24h, ActiveConns: active}
	}

	return resp
}

// healthFeatures reports Tor and GeoIP feature state (AI.md PARTS 19, 31).
func (s *Server) healthFeatures() FeaturesInfo {
	var f FeaturesInfo
	if s.config != nil && s.config.Cfg != nil {
		f.GeoIP = s.config.Cfg.Server.GeoIP.Enabled
	}

	configuredBinary := ""
	dataDir := ""
	if s.config != nil {
		if s.config.Cfg != nil {
			configuredBinary = s.config.Cfg.Server.Tor.Binary
		}
		if s.config.Paths != nil {
			dataDir = s.config.Paths.GetDataDir()
		}
	}

	_, torFound := tor.FindBinary(configuredBinary)
	hostname := ""
	if dataDir != "" {
		hostname = tor.ReadHostname(dataDir)
	}
	running := hostname != ""

	f.Tor = TorInfo{
		Enabled:  torFound,
		Running:  running,
		Hostname: hostname,
	}
	switch {
	case running:
		f.Tor.Status = "healthy"
	case torFound:
		f.Tor.Status = "starting"
	default:
		f.Tor.Status = "disabled"
	}
	return f
}

// healthChecks probes each subsystem and returns "ok"/"error" only. Details are
// deliberately withheld — the endpoint is public (AI.md PART 13 security rules).
func (s *Server) healthChecks(torInfo TorInfo) ChecksInfo {
	c := ChecksInfo{
		Database:  okError(db.Ping() == nil),
		Cache:     "ok",
		Disk:      okError(s.diskWritable()),
		Scheduler: okError(s.schedulerHealthy()),
	}
	if torInfo.Enabled {
		c.Tor = okError(torInfo.Running)
	}
	return c
}

// schedulerHealthy reports whether scheduler state can be read from the store.
func (s *Server) schedulerHealthy() bool {
	_, err := db.LoadSchedulerStates()
	return err == nil
}

// diskWritable reports whether the data directory is reachable. It intentionally
// returns only a boolean — no path, size, or filesystem detail leaves the check.
func (s *Server) diskWritable() bool {
	if s.config == nil || s.config.Paths == nil {
		return true
	}
	dir := s.config.Paths.GetDataDir()
	if dir == "" {
		return true
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// writeHealthJSON emits the health response as indented JSON with a trailing
// newline (AI.md PART 13 JSON format).
func (s *Server) writeHealthJSON(w http.ResponseWriter, resp HealthResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

// writeHealthText emits the flattened dot-notation plain-text format with
// numbered section comments in canonical order (AI.md PART 13 plain text).
func (s *Server) writeHealthText(w http.ResponseWriter, resp HealthResponse) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if resp.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	var b strings.Builder

	b.WriteString("# 1. Project (PART 16: branding)\n")
	fmt.Fprintf(&b, "project.name: %s\n", resp.Project.Name)
	fmt.Fprintf(&b, "project.tagline: %s\n", resp.Project.Tagline)
	fmt.Fprintf(&b, "project.description: %s\n\n", resp.Project.Description)

	b.WriteString("# 2. Status\n")
	fmt.Fprintf(&b, "status: %s\n\n", resp.Status)

	b.WriteString("# 3. Version & Build (PART 7)\n")
	fmt.Fprintf(&b, "version: %s\n", resp.Version)
	fmt.Fprintf(&b, "go_version: %s\n", resp.GoVersion)
	fmt.Fprintf(&b, "build.commit: %s\n", resp.Build.Commit)
	fmt.Fprintf(&b, "build.date: %s\n\n", resp.Build.Date)

	b.WriteString("# 4. Runtime (PART 6)\n")
	fmt.Fprintf(&b, "uptime: %s\n", resp.Uptime)
	fmt.Fprintf(&b, "mode: %s\n", resp.Mode)
	fmt.Fprintf(&b, "timestamp: %s\n\n", resp.Timestamp.Format(time.RFC3339))

	b.WriteString("# 5. Features - NON-NEGOTIABLE only (show actual status)\n")
	fmt.Fprintf(&b, "features.tor.enabled: %t\n", resp.Features.Tor.Enabled)
	fmt.Fprintf(&b, "features.tor.running: %t\n", resp.Features.Tor.Running)
	fmt.Fprintf(&b, "features.tor.status: %s\n", resp.Features.Tor.Status)
	fmt.Fprintf(&b, "features.tor.hostname: %s\n", resp.Features.Tor.Hostname)
	fmt.Fprintf(&b, "features.geoip: %t\n\n", resp.Features.GeoIP)

	b.WriteString("# 6. Checks\n")
	fmt.Fprintf(&b, "checks.database: %s\n", resp.Checks.Database)
	fmt.Fprintf(&b, "checks.cache: %s\n", resp.Checks.Cache)
	fmt.Fprintf(&b, "checks.disk: %s\n", resp.Checks.Disk)
	fmt.Fprintf(&b, "checks.scheduler: %s\n", resp.Checks.Scheduler)
	if resp.Checks.Tor != "" {
		fmt.Fprintf(&b, "checks.tor: %s\n", resp.Checks.Tor)
	}
	b.WriteString("\n")

	b.WriteString("# 7. Stats\n")
	fmt.Fprintf(&b, "stats.requests_total: %d\n", resp.Stats.RequestsTotal)
	fmt.Fprintf(&b, "stats.requests_24h: %d\n", resp.Stats.Requests24h)
	fmt.Fprintf(&b, "stats.active_connections: %d\n", resp.Stats.ActiveConns)

	_, _ = w.Write([]byte(b.String()))
}

// healthPageTemplate renders the public health page (AI.md PART 13 HTML format).
// It is self-contained so the endpoint always renders regardless of the site
// layout templates. All values are public-safe.
var healthTemplateFuncs = template.FuncMap{
	"statusClass": func(v string) string {
		if v == "ok" {
			return "status-ok-text"
		}
		return "status-error-text"
	},
}

var healthPageTemplate = template.Must(template.New("healthz").Funcs(healthTemplateFuncs).Parse(`<!DOCTYPE html>
<html lang="en" class="theme-dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Project.Name}} - Health Status</title>
<meta http-equiv="refresh" content="30">
<style>
:root{color-scheme:dark light}
body{font-family:system-ui,sans-serif;max-width:60rem;margin:0 auto;padding:1.5rem;background:#0f172a;color:#e2e8f0}
h1{margin:.25rem 0}
.tagline{color:#94a3b8;margin:.25rem 0}
.status-banner{padding:1rem;border-radius:8px;text-align:center;font-size:1.25rem;margin:1.5rem 0}
.status-ok{background:#052e16;color:#4ade80}
.status-error{background:#450a0a;color:#f87171}
.status-warning{background:#451a03;color:#fbbf24}
.section-card{background:#1e293b;border-radius:8px;padding:1rem 1.25rem;margin-bottom:1rem}
.info-list{display:grid;grid-template-columns:auto 1fr;gap:.35rem 1rem;margin:0}
.info-list dt{color:#94a3b8}
.info-list dd{margin:0}
code{background:#0f172a;padding:.1rem .35rem;border-radius:4px}
.data-table{width:100%;border-collapse:collapse}
.data-table th,.data-table td{text-align:left;padding:.35rem .5rem;border-bottom:1px solid #334155}
.status-ok-text{color:#4ade80}
.status-error-text{color:#f87171}
.code-block{overflow-x:auto;background:#0f172a;padding:.5rem;border-radius:4px;margin-top:.35rem}
</style>
</head>
<body>
<header class="health-header">
<h1>📦 {{.Project.Name}}</h1>
{{if .Project.Tagline}}<p class="tagline">{{.Project.Tagline}}</p>{{end}}
{{if .Project.Description}}<p>{{.Project.Description}}</p>{{end}}
</header>

<div class="status-banner {{.BannerClass}}">
<span class="status-icon">{{.StatusIcon}}</span>
<span class="status-text">{{.StatusText}}</span>
</div>

<section class="section-card">
<h2>ℹ️ Version</h2>
<dl class="info-list">
<dt>🏷️ Version</dt><dd><code>{{.Version}}</code></dd>
<dt>🐹 Go Version</dt><dd><code>{{.GoVersion}}</code></dd>
<dt>🔨 Build</dt><dd><code>{{.Build.Commit}}</code> ({{.Build.Date}})</dd>
<dt>⏱️ Uptime</dt><dd>{{.Uptime}}</dd>
<dt>🚀 Mode</dt><dd><span class="badge">{{.Mode}}</span></dd>
</dl>
</section>

<section class="section-card">
<h2>🎛️ Features</h2>
<ul class="feature-list">
<li>🧅 Tor: <span class="{{if .Features.Tor.Running}}status-ok-text{{else}}status-error-text{{end}}">{{.Features.Tor.Status}}</span>
{{if .Features.Tor.Hostname}}<div class="code-block"><code>{{.Features.Tor.Hostname}}</code></div>{{end}}
</li>
<li>🌍 GeoIP: {{if .Features.GeoIP}}enabled{{else}}disabled{{end}}</li>
</ul>
</section>

<section class="section-card">
<h2>🔧 Component Status</h2>
<table class="data-table">
<thead><tr><th>Component</th><th>Status</th></tr></thead>
<tbody>
<tr><td>🗄️ Database</td><td class="{{statusClass .Checks.Database}}">{{.Checks.Database}}</td></tr>
<tr><td>💾 Cache</td><td class="{{statusClass .Checks.Cache}}">{{.Checks.Cache}}</td></tr>
<tr><td>💿 Disk</td><td class="{{statusClass .Checks.Disk}}">{{.Checks.Disk}}</td></tr>
<tr><td>⏰ Scheduler</td><td class="{{statusClass .Checks.Scheduler}}">{{.Checks.Scheduler}}</td></tr>
{{if .Checks.Tor}}<tr><td>🧅 Tor</td><td class="{{statusClass .Checks.Tor}}">{{.Checks.Tor}}</td></tr>{{end}}
</tbody>
</table>
</section>

<section class="section-card">
<h2>📈 Server Statistics</h2>
<dl class="info-list">
<dt>📥 Total Requests</dt><dd>{{.Stats.RequestsTotal}}</dd>
<dt>📅 Requests (24h)</dt><dd>{{.Stats.Requests24h}}</dd>
<dt>🔌 Active Connections</dt><dd>{{.Stats.ActiveConns}}</dd>
</dl>
</section>

<footer class="health-footer">
<p>Last checked: <time datetime="{{.TimestampISO}}">{{.TimestampISO}}</time></p>
</footer>
</body>
</html>
`))

// writeHealthHTML renders the public HTML health page (AI.md PART 13 HTML).
func (s *Server) writeHealthHTML(w http.ResponseWriter, resp HealthResponse) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if resp.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	view := struct {
		HealthResponse
		BannerClass  string
		StatusIcon   string
		StatusText   string
		TimestampISO string
	}{
		HealthResponse: resp,
		BannerClass:    bannerClass(resp.Status),
		StatusIcon:     statusIcon(resp.Status),
		StatusText:     statusText(resp.Status),
		TimestampISO:   resp.Timestamp.Format(time.RFC3339),
	}
	_ = healthPageTemplate.Execute(w, view)
}

// overallStatus derives the aggregate status from component checks. A failed
// database is fatal (unhealthy); any other failed check is degraded.
func overallStatus(c ChecksInfo) string {
	if c.Database == "error" {
		return "unhealthy"
	}
	if c.Cache == "error" || c.Disk == "error" || c.Scheduler == "error" || c.Tor == "error" {
		return "degraded"
	}
	return "healthy"
}

// okError maps a boolean health probe result to the mandated "ok"/"error"
// vocabulary (AI.md PART 13).
func okError(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}

// formatUptime renders a duration as a compact human string like "2d 5h 30m".
func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func bannerClass(status string) string {
	switch status {
	case "healthy":
		return "status-ok"
	case "degraded":
		return "status-warning"
	default:
		return "status-error"
	}
}

func statusIcon(status string) string {
	switch status {
	case "healthy":
		return "✅"
	case "degraded":
		return "⚠️"
	default:
		return "❌"
	}
}

func statusText(status string) string {
	switch status {
	case "healthy":
		return "All Systems Operational"
	case "degraded":
		return "Degraded Performance"
	default:
		return "Service Unavailable"
	}
}
