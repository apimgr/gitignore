package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/blocklist"
	displayformat "github.com/apimgr/gitignore/src/common/format"
	"github.com/apimgr/gitignore/src/common/httputil"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/cve"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/template"
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

	Uptime    string      `json:"uptime"`
	Mode      string      `json:"mode"`
	Timestamp time.Time   `json:"timestamp"`
	Cluster   ClusterInfo `json:"cluster"`

	Features FeaturesInfo `json:"features"`
	Checks   ChecksInfo   `json:"checks"`
	Stats    StatsInfo    `json:"stats"`
}

// ClusterInfo reports the cluster mode, which is disabled for this single-instance application.
type ClusterInfo struct {
	Enabled bool `json:"enabled"`
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
	I2P   I2PInfo `json:"i2p"`
	GeoIP bool    `json:"geoip"`
}

// TorInfo reports the Tor hidden-service state (AI.md PART 31).
type TorInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
}

// I2PInfo reports the public-safe state of the optional eepsite.
type I2PInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
	Provider string `json:"provider"`
}

// ChecksInfo reports component health as "ok"/"error" only — never details
// (AI.md PART 13 "Database/cache checks MUST be vague").
type ChecksInfo struct {
	Database         string            `json:"database"`
	Cache            string            `json:"cache"`
	Disk             string            `json:"disk"`
	Scheduler        string            `json:"scheduler"`
	Tor              string            `json:"tor,omitempty"`
	I2P              string            `json:"i2p,omitempty"`
	Datasets         map[string]string `json:"-"`
	DatasetOrder     []string          `json:"-"`
	CriticalDatasets map[string]bool   `json:"-"`
}

func (c ChecksInfo) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	write := func(key, value string) error {
		if !first {
			out.WriteByte(',')
		}
		first = false
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return err
		}
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return err
		}
		out.Write(keyJSON)
		out.WriteByte(':')
		out.Write(valueJSON)
		return nil
	}
	for _, item := range []struct{ key, value string }{{"database", c.Database}, {"cache", c.Cache}, {"disk", c.Disk}, {"scheduler", c.Scheduler}} {
		if err := write(item.key, item.value); err != nil {
			return nil, err
		}
	}
	if c.Tor != "" {
		if err := write("tor", c.Tor); err != nil {
			return nil, err
		}
	}
	if c.I2P != "" {
		if err := write("i2p", c.I2P); err != nil {
			return nil, err
		}
	}
	keys := append([]string(nil), c.DatasetOrder...)
	if len(keys) == 0 {
		for key := range c.Datasets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
	}
	for _, key := range keys {
		if err := write(key, c.Datasets[key]); err != nil {
			return nil, err
		}
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func (s *Server) registerHealthChecks() {
	if s.healthRegistry == nil {
		return
	}
	if s.geoip != nil && s.geoip.Enabled() {
		for _, id := range []string{"asn", "country", "city"} {
			key := "geoip_" + id
			name := key
			manager := s.geoip
			_ = s.healthRegistry.Register(name, false, func() string {
				return manager.HealthStatuses(time.Now().UTC())[name]
			})
		}
	}
	if s.blocklist != nil {
		ids := []string{blocklist.FireHOLID}
		if s.config != nil && s.currentConfig() != nil {
			for _, source := range s.currentConfig().Server.Security.Blocklists.ExtraSources {
				if source.Enabled == nil || *source.Enabled {
					ids = append(ids, source.ID)
				}
			}
		}
		for _, id := range ids {
			key := "blocklist_" + id
			sourceID := id
			matcher := s.blocklist
			_ = s.healthRegistry.Register(key, false, func() string {
				for _, source := range matcher.Status(time.Now().UTC()) {
					if source.ID == sourceID {
						return source.State
					}
				}
				return "error"
			})
		}
	}
	templates := (*template.Manager)(nil)
	if s.config != nil {
		templates = s.config.Templates
	}
	_ = s.healthRegistry.Register("data_templates", true, func() string {
		if templates == nil || templates.Count() == 0 {
			return "error"
		}
		return "ok"
	})
	if s.securityDir != "" {
		securityDir := s.securityDir
		_ = s.healthRegistry.Register("data_cve_nvd", false, func() string {
			return cve.HealthStatus(securityDir, "nvd", time.Now().UTC())
		})
		if s.config != nil && s.currentConfig() != nil && s.currentConfig().Data.Trivy.Enabled {
			_ = s.healthRegistry.Register("data_trivy_db", false, func() string {
				return cve.HealthStatus(securityDir, "trivy", time.Now().UTC())
			})
		}
	}
	s.refreshHealthCore()
	s.healthRegistry.RefreshNow()
}

// RefreshHealth refreshes cached dataset checks after an update or reload.
func (s *Server) RefreshHealth() error {
	s.refreshHealthCore()
	if s.healthRegistry != nil {
		s.healthRegistry.RefreshNow()
	}
	return nil
}

// refreshHealthCore performs the potentially blocking core probes outside the
// request path. The scheduler and data reload hooks refresh these cached values.
func (s *Server) refreshHealthCore() {
	checks := ChecksInfo{
		Database:  okError(db.Ping() == nil),
		Cache:     "ok",
		Disk:      okError(s.diskWritable()),
		Scheduler: okError(s.schedulerHealthy()),
	}
	s.healthCoreMu.Lock()
	s.healthCore = checks
	s.healthCoreRefreshed = time.Now().UTC()
	s.healthCoreMu.Unlock()
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
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.writeHealthText(w, resp)
		} else {
			s.writeHealthFrontendText(w, r, resp)
		}
	case "application/json":
		s.writeHealthJSON(w, r, resp)
	default:
		s.writeHealthHTML(w, r, resp)
	}
}

// buildHealthResponse gathers PUBLIC-safe health data from real subsystem state.
func (s *Server) buildHealthResponse() HealthResponse {
	var resp HealthResponse

	if s.config != nil && s.currentConfig() != nil {
		resp.Project = ProjectInfo{
			Name:        s.currentConfig().Server.Branding.Title,
			Tagline:     s.currentConfig().Server.Branding.Tagline,
			Description: s.currentConfig().Server.Branding.Description,
		}
		resp.Mode = s.currentConfig().Server.Mode
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
	resp.Cluster = ClusterInfo{Enabled: false}

	resp.Features = s.healthFeatures()
	resp.Checks = s.healthChecks(resp.Features.Tor, resp.Features.I2P)
	resp.Status = overallStatus(resp.Checks)
	if s.config != nil && s.config.ConfigManager != nil && s.config.ConfigManager.PendingRestart() {
		resp.PendingRestart = true
		resp.RestartReason = s.config.ConfigManager.RestartSettings()
	}

	if s.stats != nil {
		total, last24h, active := s.stats.snapshot(time.Now())
		resp.Stats = StatsInfo{RequestsTotal: total, Requests24h: last24h, ActiveConns: active}
	}

	return resp
}

// healthFeatures reports Tor and GeoIP feature state (AI.md PARTS 19, 31).
func (s *Server) healthFeatures() FeaturesInfo {
	var f FeaturesInfo
	if s.config != nil && s.currentConfig() != nil {
		f.GeoIP = s.currentConfig().Server.GeoIP.Enabled
	}

	configuredBinary := ""
	dataDir := ""
	if s.config != nil {
		if s.currentConfig() != nil {
			configuredBinary = s.currentConfig().Server.Tor.Binary
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
	if s.config != nil && s.config.I2PStatus != nil {
		f.I2P = s.config.I2PStatus()
	} else {
		f.I2P = I2PInfo{Status: "disabled", Provider: "none"}
	}
	return f
}

// healthChecks probes each subsystem and returns "ok"/"error" only. Details are
// deliberately withheld — the endpoint is public (AI.md PART 13 security rules).
func (s *Server) healthChecks(torInfo TorInfo, i2pInfos ...I2PInfo) ChecksInfo {
	s.healthCoreMu.RLock()
	c := s.healthCore
	refreshed := s.healthCoreRefreshed
	s.healthCoreMu.RUnlock()
	if refreshed.IsZero() {
		s.refreshHealthCore()
		s.healthCoreMu.RLock()
		c = s.healthCore
		s.healthCoreMu.RUnlock()
	}
	if s.healthRegistry != nil {
		c.Datasets = make(map[string]string)
		c.CriticalDatasets = make(map[string]bool)
		for _, item := range s.healthRegistry.Snapshot() {
			c.DatasetOrder = append(c.DatasetOrder, item.Key)
			c.Datasets[item.Key] = item.State
			c.CriticalDatasets[item.Key] = item.Critical
		}
	}
	if torInfo.Enabled {
		c.Tor = "ok"
		if torInfo.Status == "unavailable" {
			c.Tor = "degraded"
		}
	}
	if len(i2pInfos) > 0 && i2pInfos[0].Enabled {
		if i2pInfos[0].Status == "unavailable" {
			c.I2P = "degraded"
		} else {
			c.I2P = "ok"
		}
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
func (s *Server) writeHealthJSON(w http.ResponseWriter, r *http.Request, resp HealthResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.Status == "unhealthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		http.Error(w, i18n.Translate(i18n.LangFromContext(r.Context()), "health.error.internal"), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

// writeHealthText emits the flattened dot-notation plain-text format with
// numbered section comments in canonical order (AI.md PART 13 plain text).
func (s *Server) writeHealthText(w http.ResponseWriter, resp HealthResponse) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if resp.Status == "unhealthy" {
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
	fmt.Fprintf(&b, "cluster.enabled: %t\n\n", resp.Cluster.Enabled)

	b.WriteString("# 5. Features - NON-NEGOTIABLE only (show actual status)\n")
	fmt.Fprintf(&b, "features.tor.enabled: %t\n", resp.Features.Tor.Enabled)
	fmt.Fprintf(&b, "features.tor.running: %t\n", resp.Features.Tor.Running)
	fmt.Fprintf(&b, "features.tor.status: %s\n", resp.Features.Tor.Status)
	fmt.Fprintf(&b, "features.tor.hostname: %s\n", resp.Features.Tor.Hostname)
	fmt.Fprintf(&b, "features.i2p.enabled: %t\n", resp.Features.I2P.Enabled)
	fmt.Fprintf(&b, "features.i2p.running: %t\n", resp.Features.I2P.Running)
	fmt.Fprintf(&b, "features.i2p.status: %s\n", resp.Features.I2P.Status)
	fmt.Fprintf(&b, "features.i2p.hostname: %s\n", resp.Features.I2P.Hostname)
	fmt.Fprintf(&b, "features.i2p.provider: %s\n", resp.Features.I2P.Provider)
	b.WriteString("features.i2p: " + resp.Features.I2P.Status)
	if resp.Features.I2P.Hostname != "" {
		b.WriteString(" " + resp.Features.I2P.Hostname)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "features.geoip: %t\n\n", resp.Features.GeoIP)

	b.WriteString("# 6. Checks\n")
	fmt.Fprintf(&b, "checks.database: %s\n", resp.Checks.Database)
	fmt.Fprintf(&b, "checks.cache: %s\n", resp.Checks.Cache)
	fmt.Fprintf(&b, "checks.disk: %s\n", resp.Checks.Disk)
	fmt.Fprintf(&b, "checks.scheduler: %s\n", resp.Checks.Scheduler)
	if resp.Checks.Tor != "" {
		fmt.Fprintf(&b, "checks.tor: %s\n", resp.Checks.Tor)
	}
	if resp.Checks.I2P != "" {
		fmt.Fprintf(&b, "checks.i2p: %s\n", resp.Checks.I2P)
	}
	datasetKeys := append([]string(nil), resp.Checks.DatasetOrder...)
	if len(datasetKeys) == 0 {
		for key := range resp.Checks.Datasets {
			datasetKeys = append(datasetKeys, key)
		}
		sort.Strings(datasetKeys)
	}
	for _, key := range datasetKeys {
		fmt.Fprintf(&b, "checks.%s: %s\n", key, resp.Checks.Datasets[key])
	}
	b.WriteString("\n")

	b.WriteString("# 7. Stats\n")
	fmt.Fprintf(&b, "stats.requests_total: %d\n", resp.Stats.RequestsTotal)
	fmt.Fprintf(&b, "stats.requests_24h: %d\n", resp.Stats.Requests24h)
	fmt.Fprintf(&b, "stats.active_connections: %d\n", resp.Stats.ActiveConns)

	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) writeHealthFrontendText(w http.ResponseWriter, r *http.Request, resp HealthResponse) {
	lang := i18n.LangFromContext(r.Context())
	t := func(key string) string { return i18n.Translate(lang, "health."+key) }
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if resp.Status == "unhealthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	var b strings.Builder
	title := resp.Project.Name
	if resp.Project.Tagline != "" {
		title += " - " + resp.Project.Tagline
	}
	fmt.Fprintf(&b, "%s\n\n", title)
	fmt.Fprintf(&b, "%s:    %s\n", t("field.status"), t("status."+resp.Status))
	fmt.Fprintf(&b, "%s:   %s\n", t("field.version"), resp.Version)
	fmt.Fprintf(&b, "%s:     %s (%s)\n", t("field.build"), resp.Build.Commit, humanTimestampString(lang, resp.Build.Date))
	fmt.Fprintf(&b, "%s:        %s\n", t("field.go_version"), resp.GoVersion)
	fmt.Fprintf(&b, "%s:    %s\n", t("field.uptime"), displayformat.Duration(time.Since(s.startTime), lang))
	fmt.Fprintf(&b, "%s:      %s\n", t("field.mode"), t("mode."+resp.Mode))
	fmt.Fprintf(&b, "%s:    %s\n", t("field.checked"), humanTimestamp(lang, resp.Timestamp))
	fmt.Fprintf(&b, "%s:   %s\n\n", t("field.cluster"), translatedBool(lang, resp.Cluster.Enabled))

	fmt.Fprintf(&b, "%s\n", t("section.features"))
	fmt.Fprintf(&b, "  %s:     %s", t("feature.tor"), t("feature_status."+resp.Features.Tor.Status))
	if resp.Features.Tor.Hostname != "" {
		fmt.Fprintf(&b, " - %s", resp.Features.Tor.Hostname)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "  %s:     %s", t("feature.i2p"), t("feature_status."+resp.Features.I2P.Status))
	if resp.Features.I2P.Hostname != "" {
		fmt.Fprintf(&b, " - %s", resp.Features.I2P.Hostname)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "  %s:   %s\n\n", t("feature.geoip"), translatedBool(lang, resp.Features.GeoIP))

	fmt.Fprintf(&b, "%s\n", t("section.checks"))
	fmt.Fprintf(&b, "  %-19s %s\n", t("check.database"), translatedCheckState(lang, resp.Checks.Database))
	fmt.Fprintf(&b, "  %-19s %s\n", t("check.cache"), translatedCheckState(lang, resp.Checks.Cache))
	fmt.Fprintf(&b, "  %-19s %s\n", t("check.disk"), translatedCheckState(lang, resp.Checks.Disk))
	fmt.Fprintf(&b, "  %-19s %s\n", t("check.scheduler"), translatedCheckState(lang, resp.Checks.Scheduler))
	if resp.Checks.Tor != "" {
		fmt.Fprintf(&b, "  %-19s %s\n", t("feature.tor"), translatedCheckState(lang, resp.Checks.Tor))
	}
	if resp.Checks.I2P != "" {
		fmt.Fprintf(&b, "  %-19s %s\n", t("feature.i2p"), translatedCheckState(lang, resp.Checks.I2P))
	}
	for _, key := range resp.Checks.DatasetOrder {
		fmt.Fprintf(&b, "  %-19s %s\n", healthCheckLabel(lang, key)+":", translatedCheckState(lang, resp.Checks.Datasets[key]))
	}
	fmt.Fprintf(&b, "\n%s\n", t("section.stats"))
	fmt.Fprintf(&b, "  %s: %s\n", t("field.total_requests"), displayformat.Count(resp.Stats.RequestsTotal, lang))
	fmt.Fprintf(&b, "  %s: %s\n", t("field.requests_24h"), displayformat.Count(resp.Stats.Requests24h, lang))
	fmt.Fprintf(&b, "  %s: %s\n", t("field.active_connections"), displayformat.Count(int64(resp.Stats.ActiveConns), lang))
	_, _ = w.Write([]byte(b.String()))
}

func healthCheckLabel(lang, key string) string {
	if translated := i18n.Translate(lang, "health.check."+key); translated != "health.check."+key {
		return translated
	}
	for _, prefix := range []struct{ key, kind string }{{"geoip_", "geoip"}, {"blocklist_", "blocklist"}, {"data_", "data"}} {
		if strings.HasPrefix(key, prefix.key) {
			label := i18n.Translate(lang, "health.check_kind."+prefix.kind)
			return label + ": " + strings.ReplaceAll(strings.TrimPrefix(key, prefix.key), "_", " ")
		}
	}
	label := strings.ReplaceAll(key, "_", " ")
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	return label
}

func translatedBool(lang string, value bool) string {
	key := "common.disabled"
	if value {
		key = "common.enabled"
	}
	return i18n.Translate(lang, key)
}

func translatedCheckState(lang, state string) string {
	return i18n.Translate(lang, "health.check_state."+state)
}

func humanTimestamp(lang string, value time.Time) string {
	utc := value.UTC()
	month := i18n.Translate(lang, "health.month."+strings.ToLower(utc.Month().String()))
	return i18n.TranslateFormat(lang, "health.timestamp.format", map[string]string{
		"month": month,
		"day":   fmt.Sprintf("%d", utc.Day()),
		"year":  fmt.Sprintf("%d", utc.Year()),
		"time":  utc.Format("15:04:05"),
		"zone":  "UTC",
	})
}

func humanTimestampString(lang, value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return humanTimestamp(lang, parsed)
}

type healthPageView struct {
	HealthResponse
	Lang           string
	ModeLabel      string
	BannerClass    string
	StatusIcon     string
	StatusText     string
	TimestampISO   string
	TimestampHuman string
	BuildDateHuman string
	UptimeHuman    string
}

var healthTemplateFuncs = htmltemplate.FuncMap{
	"statusClass": func(value string) string {
		switch value {
		case "ok", "healthy", "running":
			return "status-ok"
		case "degraded", "starting", "restart_required":
			return "status-warning"
		default:
			return "status-error"
		}
	},
	"t":       func(lang, key string) string { return i18n.Translate(lang, key) },
	"label":   healthCheckLabel,
	"state":   translatedCheckState,
	"enabled": translatedBool,
	"number": func(lang string, value interface{}) string {
		switch count := value.(type) {
		case int:
			return displayformat.Count(int64(count), lang)
		case int64:
			return displayformat.Count(count, lang)
		default:
			return fmt.Sprint(value)
		}
	},
}

// writeHealthHTML renders the public HTML health page inside the shared site shell.
func (s *Server) writeHealthHTML(w http.ResponseWriter, r *http.Request, resp HealthResponse) {
	lang := i18n.LangFromContext(r.Context())
	view := &healthPageView{
		HealthResponse: resp,
		Lang:           lang,
		ModeLabel:      i18n.Translate(lang, "health.mode."+resp.Mode),
		BannerClass:    bannerClass(resp.Status),
		StatusIcon:     statusIcon(resp.Status),
		StatusText:     i18n.Translate(lang, "health.status."+resp.Status),
		TimestampISO:   resp.Timestamp.Format(time.RFC3339),
		TimestampHuman: humanTimestamp(lang, resp.Timestamp),
		BuildDateHuman: humanTimestampString(lang, resp.Build.Date),
		UptimeHuman:    displayformat.Duration(time.Since(s.startTime), lang),
	}
	status := http.StatusOK
	if resp.Status == "unhealthy" {
		status = http.StatusServiceUnavailable
	}
	documentTitle := resp.Project.Name + " - " + i18n.Translate(lang, "health.title")
	s.renderPageStatus(w, r, "healthz", status, PageData{
		Title:         resp.Project.Name,
		DocumentTitle: documentTitle,
		Description:   resp.Project.Description,
		Health:        view,
	})
}

// overallStatus derives the aggregate status from component checks. A failed
// database is fatal (unhealthy); any other failed check is degraded.
func overallStatus(c ChecksInfo) string {
	if c.Database == "error" || c.Disk == "error" {
		return "unhealthy"
	}
	if c.Cache == "error" || c.Scheduler == "error" || c.Tor == "degraded" || c.I2P == "degraded" {
		return "degraded"
	}
	for key, value := range c.Datasets {
		if value != "ok" && c.CriticalDatasets[key] {
			return "unhealthy"
		}
	}
	for _, value := range c.Datasets {
		if value != "ok" {
			return "degraded"
		}
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

func statusText(lang, status string) string {
	return i18n.Translate(lang, "health.status."+status)
}
