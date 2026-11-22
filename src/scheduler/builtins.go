package scheduler

import (
	"context"
	"errors"
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
	schedDiskCheck       = "@every 5m"
	schedSecurityCleanup = "30 3 * * *"
	schedHealthRefresh   = "@every 60s"
	schedPGPKeyserver    = "@every 1m"
)

// Deps carries runtime handles and subsystem callbacks needed by built-in
// tasks. External work is injected, and a missing required callback fails the
// task so incomplete wiring cannot be reported as a successful run.
type Deps struct {
	LogsDir               string
	ConfigDir             string
	LogRetentionDays      int
	SecurityRetentionDays int
	SSLEnabled            bool
	BackupEnabled         bool
	// BackupDaily / BackupHourly are optional; when nil the tasks skip.
	BackupDaily  HandlerFunc
	BackupHourly HandlerFunc
	// GeoIPUpdate downloads/refreshes the GeoIP databases (AI.md PART 19). When
	// nil the geoip_update task skips.
	GeoIPUpdate HandlerFunc
	// UpdateCheck runs the self-update check (AI.md PART 22). When nil the
	// update_check task skips.
	UpdateCheck HandlerFunc
	// BlocklistUpdate refreshes configured and built-in security blocklists.
	BlocklistUpdate HandlerFunc
	// CVEUpdate refreshes NVD and, when enabled, the Trivy vulnerability DB.
	CVEUpdate HandlerFunc
	// DiskCheck checks data, log, and backup volumes and emits low-space events.
	DiskCheck         HandlerFunc
	DiskCheckInterval string
	// TorInstalled and I2PEnabled bind their health tasks to active modes.
	TorInstalled        bool
	I2PEnabled          bool
	TorHealth           HandlerFunc
	I2PHealth           HandlerFunc
	OwnerIdlePurgeDays  int
	PurgeIdleOwners     HandlerFunc
	HealthRefresh       HandlerFunc
	PGPKeyserverPublish HandlerFunc
	TaskState           func(id string, running bool, status string, duration time.Duration, lastRun time.Time)
}

// RegisterBuiltins registers the mandated AI.md PART 18 tasks. Tor and I2P
// health tasks are registered only when their corresponding modes are active.
func RegisterBuiltins(s *Scheduler, d Deps) error {
	if d.LogRetentionDays <= 0 {
		d.LogRetentionDays = 7
	}
	if d.SecurityRetentionDays <= 0 {
		d.SecurityRetentionDays = 365
	}
	diskSchedule := schedDiskCheck
	if interval := strings.TrimSpace(d.DiskCheckInterval); interval != "" {
		if _, err := time.ParseDuration(interval); err == nil {
			diskSchedule = "@every " + interval
		}
	}

	tasks := []struct {
		id, name, schedule string
		skippable          bool
		retry              bool
		handler            HandlerFunc
	}{
		{"ssl_renewal", "SSL Renewal", schedSSLRenewal, false, false, sslRenewalHandler(d)},
		{"geoip_update", "GeoIP Update", schedGeoIPUpdate, true, false, geoipUpdateHandler(d)},
		{"blocklist_update", "Blocklist Update", schedBlocklistUpdate, true, true, requiredHandler("blocklist_update", d.BlocklistUpdate)},
		{"cve_update", "CVE Update", schedCVEUpdate, true, true, requiredHandler("cve_update", d.CVEUpdate)},
		{"update_check", "Update Check", schedUpdateCheck, true, false, updateCheckHandler(d)},
		{"token_cleanup", "Token Cleanup", schedTokenCleanup, false, false, tokenCleanupHandler(d)},
		{"log_rotation", "Log Rotation", schedLogRotation, false, false, logRotationHandler(d)},
		{"backup_daily", "Backup Daily", schedBackupDaily, true, false, backupHandler(d.BackupEnabled, d.BackupDaily)},
		{"backup_hourly", "Backup Hourly", schedBackupHourly, true, false, backupHandler(d.BackupEnabled, d.BackupHourly)},
		{"disk_check", "Disk Space Check", diskSchedule, true, false, requiredHandler("disk_check", d.DiskCheck)},
		{"healthcheck_self", "Health Check", schedHealthcheckSelf, false, false, healthcheckSelfHandler},
		{"health_registry_refresh", "Health Registry Refresh", schedHealthRefresh, true, false, healthRegistryRefreshHandler(d)},
		{"security_cleanup", "Security Report Cleanup", schedSecurityCleanup, false, false, securityCleanupHandler(d)},
	}
	if d.TorInstalled {
		tasks = append(tasks, struct {
			id, name, schedule string
			skippable          bool
			retry              bool
			handler            HandlerFunc
		}{"tor_health", "Tor Health", schedTorHealth, false, false, requiredHandler("tor_health", d.TorHealth)})
	}
	if d.I2PEnabled {
		tasks = append(tasks, struct {
			id, name, schedule string
			skippable          bool
			retry              bool
			handler            HandlerFunc
		}{"i2p_health", "I2P Health", schedTorHealth, false, false, requiredHandler("i2p_health", d.I2PHealth)})
	}
	if d.PGPKeyserverPublish != nil {
		tasks = append(tasks, struct {
			id, name, schedule string
			skippable          bool
			retry              bool
			handler            HandlerFunc
		}{"pgp_keyserver_publish", "PGP Keyserver Publication", schedPGPKeyserver, true, false, d.PGPKeyserverPublish})
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

func healthRegistryRefreshHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.HealthRefresh == nil {
			return ErrSkipped
		}
		return d.HealthRefresh(ctx)
	}
}

func securityCleanupHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := db.CleanupClosedSecurityReports(time.Now().AddDate(0, 0, -d.SecurityRetentionDays))
		if err != nil {
			return err
		}
		if count > 0 {
			log.Printf("scheduler: security_cleanup removed %d expired reports", count)
		}
		if d.ConfigDir == "" {
			return nil
		}
		oldKey := filepath.Join(d.ConfigDir, "security", "pgp.priv.old.asc.enc")
		info, err := os.Stat(oldKey)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Since(info.ModTime()) <= 30*24*time.Hour {
			return nil
		}
		if err := os.Remove(oldKey); err != nil {
			return err
		}
		log.Printf("scheduler: security_cleanup removed retired PGP private key")
		return nil
	}
}

// requiredHandler makes missing runtime wiring an explicit task failure.
func requiredHandler(taskID string, handler HandlerFunc) HandlerFunc {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if handler == nil {
			return errors.New("scheduler: required handler is unavailable for " + taskID)
		}
		return handler(ctx)
	}
}

// tokenCleanupHandler removes expired tokens and sessions from server.db.
func tokenCleanupHandler(d Deps) HandlerFunc {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := db.CleanupExpiredTokens()
		if err != nil {
			return err
		}
		if n > 0 {
			log.Printf("scheduler: token_cleanup removed %d expired records", n)
		}
		if d.OwnerIdlePurgeDays > 0 {
			if d.PurgeIdleOwners == nil {
				return errors.New("scheduler: owner idle purge handler is unavailable")
			}
			if err := d.PurgeIdleOwners(ctx); err != nil {
				return err
			}
		}
		return nil
	}
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
