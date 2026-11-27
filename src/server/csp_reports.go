package server

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxCSPReportBytes = 64 << 10

type cspReport struct {
	Type   string         `json:"type"`
	Legacy *cspReportBody `json:"csp-report"`
	Body   *cspReportBody `json:"body"`
}

type cspReportBody struct {
	BlockedURI         string `json:"blocked-uri"`
	ViolatedDirective  string `json:"violated-directive"`
	EffectiveDirective string `json:"effective-directive"`
}

type reportRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]reportRateBucket
	lastGC  time.Time
}

type reportRateBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

func newReportRateLimiter() *reportRateLimiter {
	return &reportRateLimiter{buckets: make(map[string]reportRateBucket), lastGC: time.Now()}
}

func (rl *reportRateLimiter) allow(ip string, perMinute, burst int) bool {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 10
	}

	now := time.Now()
	if ip == "" {
		ip = "unknown"
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if now.Sub(rl.lastGC) >= time.Minute {
		for key, bucket := range rl.buckets {
			if now.Sub(bucket.lastSeen) >= time.Minute {
				delete(rl.buckets, key)
			}
		}
		rl.lastGC = now
	}

	bucket, ok := rl.buckets[ip]
	if !ok {
		bucket = reportRateBucket{tokens: float64(burst), updated: now}
	}
	refill := now.Sub(bucket.updated).Seconds() * float64(perMinute) / 60
	bucket.tokens += refill
	if bucket.tokens > float64(burst) {
		bucket.tokens = float64(burst)
	}
	bucket.updated = now
	bucket.lastSeen = now
	if bucket.tokens < 1 {
		rl.buckets[ip] = bucket
		return false
	}
	bucket.tokens--
	rl.buckets[ip] = bucket
	return true
}

func (s *Server) allowBrowserReport(r *http.Request) bool {
	perMinute, burst := 60, 10
	if cfg := s.currentConfig(); cfg != nil {
		if cfg.Web.Reports.RateLimitPerMinute > 0 {
			perMinute = cfg.Web.Reports.RateLimitPerMinute
		}
		if cfg.Web.Reports.RateLimitPerIPBurst > 0 {
			burst = cfg.Web.Reports.RateLimitPerIPBurst
		}
	}
	if s.reportLimiter == nil {
		return true
	}
	return s.reportLimiter.allow(s.clientIP(r), perMinute, burst)
}

func (s *Server) handleCSPReport(w http.ResponseWriter, r *http.Request) {
	s.acceptBrowserReport(w, r)
	if !s.allowBrowserReport(r) {
		return
	}
	if s.allowCSPReportSample() {
		s.logCSPReports(r, readReportBody(w, r))
	} else if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxCSPReportBytes+1))
	}
}

func (s *Server) handleDefaultReport(w http.ResponseWriter, r *http.Request) {
	s.acceptBrowserReport(w, r)
	if !s.allowBrowserReport(r) {
		return
	}
	for _, report := range parseCSPReports(readReportBody(w, r)) {
		if report.Legacy != nil || report.Type == "csp-violation" {
			if s.allowCSPReportSample() {
				s.logCSPReport(r, report)
			}
		}
	}
}

func (s *Server) allowCSPReportSample() bool {
	rate := 1.0
	if cfg := s.currentConfig(); cfg != nil {
		rate = cfg.Web.CSP.ReportsSampleRate
		if !cfg.Web.CSP.ReportsEnabled {
			return false
		}
	}
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	return rand.Float64() < rate
}

func (s *Server) handleNELReport(w http.ResponseWriter, r *http.Request) {
	s.acceptBrowserReport(w, r)
	if !s.allowBrowserReport(r) {
		return
	}
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxCSPReportBytes+1))
	}
}

func (s *Server) acceptBrowserReport(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func readReportBody(w http.ResponseWriter, r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCSPReportBytes))
	if err != nil {
		return nil
	}
	return body
}

func parseCSPReports(body []byte) []cspReport {
	if len(body) == 0 {
		return nil
	}
	var reports []cspReport
	if json.Unmarshal(body, &reports) == nil {
		return reports
	}
	var report cspReport
	if json.Unmarshal(body, &report) == nil {
		return []cspReport{report}
	}
	return nil
}

func (s *Server) logCSPReports(r *http.Request, body []byte) {
	for _, report := range parseCSPReports(body) {
		if report.Legacy == nil && report.Type != "" && report.Type != "csp-violation" {
			continue
		}
		s.logCSPReport(r, report)
	}
}

func (s *Server) logCSPReport(r *http.Request, report cspReport) {
	entry := report.Body
	if report.Legacy != nil {
		entry = report.Legacy
	}
	if entry == nil {
		return
	}
	directive := entry.EffectiveDirective
	if directive == "" {
		directive = entry.ViolatedDirective
	}
	s.logCSPViolation(r, directive, entry.BlockedURI)
}

func (s *Server) logCSPViolation(r *http.Request, directive, blockedURI string) {
	if s.config == nil || s.config.Paths == nil {
		return
	}
	logDir := s.config.Paths.GetLogsDir()
	if os.MkdirAll(logDir, 0750) != nil {
		return
	}
	path := filepath.Join(logDir, "security.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_ = file.Chmod(0600)
	_, _ = fmt.Fprintf(file, "security.csp_violation ip=%q blocked_uri=%q violated_directive=%q\n",
		reportLogField(s.clientIP(r), 200), reportLogField(sanitizeCSPReportURI(blockedURI), 256), reportLogField(directive, 200))
}

func reportLogField(value string, maxRunes int) string {
	value = safeSecurityLogField(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}

func sanitizeCSPReportURI(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return "[redacted]"
	}
	if strings.EqualFold(parsed.Scheme, "file") {
		return "[redacted]"
	}
	if strings.EqualFold(parsed.Scheme, "data") {
		return "data:"
	}
	if host := parsed.Hostname(); host != "" {
		if address, err := netip.ParseAddr(host); err == nil && (address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast()) {
			return "[redacted]"
		}
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return reportLogField(parsed.String(), 256)
}
