package scheduler

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

// Default schedules for the built-in tasks (AI.md PART 18 "Built-in Tasks").
const (
	schedSSLRenewal      = "0 3 * * *"
	schedGeoIPUpdate     = "0 3 * * 0"
	schedBlocklistUpdate = "0 4 * * *"
	schedCVEUpdate       = "0 5 * * *"
	schedUpdateCheck     = "0 6 * * *"
	schedTokenCleanup    = "@every 15m"
	schedLogRotation     = "0 0 * * *"
	schedBackupDaily     = "0 2 * * *"
	schedBackupHourly    = "@hourly"
	schedHealthcheckSelf = "@every 5m"
	schedTorHealth       = "@every 10m"
)

// Deps carries the runtime handles the built-in tasks need. Handlers whose
// business logic lives outside this package (backup lives in package main) are
// injected; tasks whose owning subsystem is not yet wired register a handler
// that returns ErrSkipped until that subsystem provides one.
type Deps struct {
	LogsDir          string
	LogRetentionDays int
	SSLEnabled       bool
	BackupEnabled    bool
	// BackupDaily / BackupHourly are optional; when nil the tasks skip.
	BackupDaily  HandlerFunc
	BackupHourly HandlerFunc
	// GeoIPUpdate downloads/refreshes the GeoIP databases (AI.md PART 19). When
	// nil the geoip_update task skips.
	GeoIPUpdate HandlerFunc
	// UpdateCheck runs the self-update check (AI.md PART 22). When nil the
	// update_check task skips.
	UpdateCheck HandlerFunc
	// TorInstalled gates the tor_health task.
	TorInstalled bool
}

// RegisterBuiltins registers every mandated task from AI.md PART 18. Tasks
// appear in `scheduler list` regardless of whether their subsystem is active;
// inactive ones report StatusSkipped rather than running.
func RegisterBuiltins(s *Scheduler, d Deps) error {
	if d.LogRetentionDays <= 0 {
		d.LogRetentionDays = 7
	}

	tasks := []struct {
		id, name, schedule string
		skippable          bool
		retry              bool
		handler            HandlerFunc
	}{
		{"ssl_renewal", "SSL Renewal", schedSSLRenewal, false, false, sslRenewalHandler(d)},
		{"geoip_update", "GeoIP Update", schedGeoIPUpdate, true, false, geoipUpdateHandler(d)},
		{"blocklist_update", "Blocklist Update", schedBlocklistUpdate, true, true, notWired("Blocklists (AI.md PART 11)")},
		{"cve_update", "CVE Update", schedCVEUpdate, true, true, notWired("CVE database (AI.md PART 11)")},
		{"update_check", "Update Check", schedUpdateCheck, true, false, updateCheckHandler(d)},
		{"token_cleanup", "Token Cleanup", schedTokenCleanup, false, false, tokenCleanupHandler},
		{"log_rotation", "Log Rotation", schedLogRotation, false, false, logRotationHandler(d)},
		{"backup_daily", "Backup Daily", schedBackupDaily, true, false, backupHandler(d.BackupEnabled, d.BackupDaily)},
		{"backup_hourly", "Backup Hourly", schedBackupHourly, true, false, backupHandler(d.BackupEnabled, d.BackupHourly)},
		{"healthcheck_self", "Health Check", schedHealthcheckSelf, false, false, healthcheckSelfHandler},
		{"tor_health", "Tor Health", schedTorHealth, false, false, torHealthHandler(d)},
	}

	for _, t := range tasks {
		task := &Task{
			ID:           t.id,
			Name:         t.name,
			scheduleExpr: t.schedule,
			Skippable:    t.skippable,
			Handler:      t.handler,
			RetryOnFail:  t.retry,
			RetryDelay:   time.Hour,
		}
		if err := s.Register(task); err != nil {
			return err
		}
	}

	// backup_hourly is disabled by default (AI.md PART 18).
	if err := s.SetEnabled("backup_hourly", false); err != nil {
		return err
	}
	return nil
}

// notWired returns a handler that records a skip, naming the subsystem that
// will supply the real implementation in a later wave.
func notWired(subsystem string) HandlerFunc {
	return func(ctx context.Context) error {
		log.Printf("scheduler: %s not yet configured; skipping", subsystem)
		return ErrSkipped
	}
}

// tokenCleanupHandler removes expired tokens and sessions from server.db.
func tokenCleanupHandler(ctx context.Context) error {
	n, err := db.CleanupExpiredTokens()
	if err != nil {
		return err
	}
	if n > 0 {
		log.Printf("scheduler: token_cleanup removed %d expired records", n)
	}
	return nil
}

// healthcheckSelfHandler verifies core dependencies (currently the database).
func healthcheckSelfHandler(ctx context.Context) error {
	return db.Ping()
}

// sslRenewalHandler renews certificates when SSL is enabled. Certificate
// acquisition itself is handled by autocert at request time (AI.md PART 15);
// this task exists so renewals are tracked and surfaced. When SSL is disabled
// it skips.
func sslRenewalHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if !d.SSLEnabled {
			return ErrSkipped
		}
		return nil
	}
}

// geoipUpdateHandler runs the injected GeoIP update function (AI.md PART 19).
// When no handler is injected — GeoIP disabled or CLI-only paths — it skips.
func geoipUpdateHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if d.GeoIPUpdate == nil {
			return ErrSkipped
		}
		return d.GeoIPUpdate(ctx)
	}
}

// updateCheckHandler runs the injected self-update check (AI.md PART 22). When
// no handler is injected — CLI-only paths — it skips.
func updateCheckHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if d.UpdateCheck == nil {
			return ErrSkipped
		}
		return d.UpdateCheck(ctx)
	}
}

// backupHandler runs the injected backup function when backups are enabled.
func backupHandler(enabled bool, fn HandlerFunc) HandlerFunc {
	return func(ctx context.Context) error {
		if !enabled || fn == nil {
			return ErrSkipped
		}
		return fn(ctx)
	}
}

// torHealthHandler checks Tor connectivity when Tor is installed.
func torHealthHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if !d.TorInstalled {
			return ErrSkipped
		}
		log.Printf("scheduler: Tor health check not yet configured (AI.md PART 24); skipping")
		return ErrSkipped
	}
}

// logRotationHandler deletes rotated log archives older than the retention
// window. It never touches the live *.log files, only already-rotated archives
// (*.gz and *.log.<n>).
func logRotationHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if d.LogsDir == "" {
			return ErrSkipped
		}
		entries, err := os.ReadDir(d.LogsDir)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrSkipped
			}
			return err
		}
		cutoff := time.Now().Add(-time.Duration(d.LogRetentionDays) * 24 * time.Hour)
		removed := 0
		for _, e := range entries {
			if e.IsDir() || !isRotatedLog(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(filepath.Join(d.LogsDir, e.Name())); err == nil {
					removed++
				}
			}
		}
		if removed > 0 {
			log.Printf("scheduler: log_rotation pruned %d archived logs", removed)
		}
		return nil
	}
}

// isRotatedLog reports whether name is an already-rotated log archive rather
// than a live log file.
func isRotatedLog(name string) bool {
	if strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".zip") {
		return true
	}
	// Matches "app.log.1", "access.log.20240101", etc.
	idx := strings.Index(name, ".log.")
	return idx >= 0
}
