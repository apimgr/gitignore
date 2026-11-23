package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	displayformat "github.com/apimgr/gitignore/src/common/format"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/geoip"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/scheduler"
)

// buildScheduler constructs the scheduler from config and registers every
// built-in task (AI.md PART 18). Backup handlers are injected here because
// backup logic lives in package main.
func buildScheduler(cfg *config.Config, configDir, dataDir, logsDir string, gm *geoip.Manager, runtimeDeps ...scheduler.Deps) (*scheduler.Scheduler, error) {
	catchUp := time.Hour
	if cfg.Server.Schedule.CatchUpWindow != "" {
		if d, err := time.ParseDuration(cfg.Server.Schedule.CatchUpWindow); err == nil && d > 0 {
			catchUp = d
		}
	}
	var taskState func(string, bool, string, time.Duration, time.Time)
	if len(runtimeDeps) > 0 {
		taskState = runtimeDeps[0].TaskState
	}

	s := scheduler.New(scheduler.Config{
		Timezone:      cfg.Server.Schedule.Timezone,
		CatchUpWindow: catchUp,
		// scheduler_error operator email (AI.md PART 17). notifier is nil on
		// CLI-only paths, so this safely no-ops there.
		OnError: func(id, name string, err error, nextRun time.Time) {
			emitSchedulerError(id, name, err.Error(), fmtTime(nextRun))
		},
		OnState: taskState,
	})

	retention := cfg.Server.Maintenance.Cleanup.LogRetentionDays
	if retention <= 0 {
		retention = 7
	}

	deps := scheduler.Deps{
		LogsDir:               logsDir,
		ConfigDir:             configDir,
		LogRetentionDays:      retention,
		SecurityRetentionDays: cfg.Server.Security.Reports.RetentionDays,
		SSLEnabled:            cfg.Server.SSL.Enabled,
		BackupEnabled:         true,
		BackupDaily:           backupTaskHandler(cfg, configDir, dataDir, "daily"),
		BackupHourly:          backupTaskHandler(cfg, configDir, dataDir, "hourly"),
		UpdateCheck:           updateCheckHandler(cfg, dataDir),
		TorInstalled:          torBinaryInstalled(cfg),
		I2PEnabled:            cfg.Server.I2P.Enabled,
	}
	if len(cfg.Web.Security.Keyservers) > 0 {
		securityDir := filepath.Join(configDir, "security")
		deps.PGPKeyserverPublish = func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, err := config.Load(filepath.Join(configDir, "server.yml"))
			if err != nil {
				return err
			}
			if !current.Web.Security.PublishPGPKey {
				return nil
			}
			if _, err := os.Stat(filepath.Join(securityDir, pgpPublicFilename)); os.IsNotExist(err) {
				return nil
			}
			return publishProjectPGPContext(ctx, current, securityDir)
		}
	}
	if len(runtimeDeps) > 0 {
		runtime := runtimeDeps[0]
		deps.BlocklistUpdate = runtime.BlocklistUpdate
		deps.CVEUpdate = runtime.CVEUpdate
		deps.DiskCheck = runtime.DiskCheck
		deps.DiskCheckInterval = runtime.DiskCheckInterval
		deps.TorHealth = runtime.TorHealth
		deps.I2PHealth = runtime.I2PHealth
		deps.OwnerIdlePurgeDays = runtime.OwnerIdlePurgeDays
		deps.PurgeIdleOwners = runtime.PurgeIdleOwners
		deps.HealthRefresh = runtime.HealthRefresh
	}

	if gm != nil {
		deps.GeoIPUpdate = func(ctx context.Context) error {
			updateErr := geoipUpdateHandler(gm)(ctx)
			if deps.HealthRefresh != nil {
				refreshErr := deps.HealthRefresh(ctx)
				if updateErr == nil {
					return refreshErr
				}
			}
			return updateErr
		}
	}

	if err := scheduler.RegisterBuiltins(s, deps); err != nil {
		return nil, err
	}
	return s, nil
}

// backupTaskHandler returns a HandlerFunc that runs a full backup into the
// backup directory, named by kind (daily/hourly).
func backupTaskHandler(cfg *config.Config, configDir, dataDir, kind string) scheduler.HandlerFunc {
	return func(ctx context.Context) error {
		backupDir := apppath.GetBackupDir()
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return err
		}
		name := fmt.Sprintf("%s-%s.tar.gz", projectName, kind)
		dest := filepath.Join(backupDir, name)
		if err := runBackup(cfg, configDir, dataDir, dest); err != nil {
			// Dedicated failure notification; suppresses scheduler_error for
			// this execution (AI.md PART 17 suppression rules).
			emitBackupFailed(name, err.Error())
			return err
		}
		emitBackupComplete(name, backupFileSize(dest, cfg.Server.I18n.DefaultLanguage))
		return nil
	}
}

// backupFileSize returns a human-readable size for the finished backup file, or
// "unknown" when it cannot be stat'd.
func backupFileSize(path, lang string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	return displayformat.Size(fi.Size(), lang)
}

func schedulerText(lang, key string, values ...interface{}) string {
	return i18n.TranslateFormat(lang, "cli.scheduler."+key, values...)
}

func schedulerTaskName(lang string, task scheduler.TaskInfo) string {
	key := "cli.scheduler.task." + task.ID
	name := i18n.Translate(lang, key)
	if name == key {
		return task.Name
	}
	return name
}

func schedulerStatusText(lang, status string) string {
	key := map[string]string{
		scheduler.StatusSuccess: "status_success",
		scheduler.StatusFailed:  "status_failed",
		"running":               "status_running",
		"pending":               "status_pending",
		scheduler.StatusSkipped: "status_skipped",
	}[status]
	if key == "" {
		return status
	}
	return schedulerText(lang, key)
}

// humanizeBytes formats a byte count with a binary unit suffix.
func humanizeBytes(n int64) string {
	return displayformat.Size(n, "en")
}

// handleSchedulerCommand dispatches the `scheduler` CLI subcommands
// (AI.md PART 18 "CLI Commands"). It initializes the database, loads persisted
// state, and operates without starting the background loop.
func handleSchedulerCommand(args []string, cfg *config.Config, configDir, dataDir, logsDir string, color bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "usage"))
		os.Exit(exUsage)
	}

	if err := db.Init(dataDir); err != nil {
		fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "database_init_failed", "error", err))
		os.Exit(exUnavailable)
	}
	defer func() { _ = db.Close() }()

	gm := newGeoIP(cfg, dataDir)
	sched, err := buildScheduler(cfg, configDir, dataDir, logsDir, gm)
	if err != nil {
		fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "build_failed", "error", err))
		os.Exit(exConfig)
	}
	sched.LoadPersisted()

	sub := args[0]
	var id string
	if len(args) > 1 {
		id = args[1]
	}

	switch sub {
	case "list":
		schedulerList(sched, outputLanguage)
	case "show":
		if id == "" {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "usage_show"))
			os.Exit(exUsage)
		}
		schedulerShow(sched, id, outputLanguage)
	case "run":
		if id == "" {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "usage_run"))
			os.Exit(exUsage)
		}
		fmt.Println(schedulerText(outputLanguage, "running_task", "id", id))
		if err := sched.RunNow(context.Background(), id); err != nil {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "task_failed", "error", err))
			os.Exit(exOSErr)
		}
		fmt.Println(schedulerText(outputLanguage, "done"))
	case "enable", "disable":
		if id == "" {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "usage_action", "action", sub))
			os.Exit(exUsage)
		}
		if _, ok := sched.Task(id); !ok {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "unknown_task", "id", id))
			os.Exit(exUsage)
		}
		if err := sched.SetEnabled(id, sub == "enable"); err != nil {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "task_update_failed", "error", err))
			os.Exit(exIOErr)
		}
		key := "task_enabled"
		if sub == "disable" {
			key = "task_disabled"
		}
		fmt.Println(schedulerText(outputLanguage, key, "id", id))
	case "history":
		if id == "" {
			fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "usage_history"))
			os.Exit(exUsage)
		}
		schedulerHistory(sched, id, outputLanguage)
	default:
		fmt.Fprintln(os.Stderr, schedulerText(outputLanguage, "unknown_command", "command", sub))
		os.Exit(exUsage)
	}
}

// schedulerStatusGlyph maps a task status to its legend glyph (AI.md PART 18).
func schedulerStatusGlyph(info scheduler.TaskInfo) string {
	if info.Running {
		return "●"
	}
	switch info.LastStatus {
	case scheduler.StatusSuccess:
		return "✓"
	case scheduler.StatusFailed:
		return "✗"
	case scheduler.StatusSkipped:
		return "◐"
	default:
		return "○"
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func schedulerList(s *scheduler.Scheduler, lang string) {
	tasks := s.Tasks()
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "  \t%s\t%s\t%s\t%s\t%s\n",
		schedulerText(lang, "column_task"), schedulerText(lang, "column_schedule"),
		schedulerText(lang, "column_last_run"), schedulerText(lang, "column_next_run"),
		schedulerText(lang, "column_status"))
	for _, t := range tasks {
		state := schedulerText(lang, "enabled")
		if !t.Enabled {
			state = schedulerText(lang, "disabled")
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			schedulerStatusGlyph(t), schedulerTaskName(lang, t), t.Schedule,
			fmtTime(t.LastRun), fmtTime(t.NextRun), state)
	}
	_ = w.Flush()
	fmt.Printf("\n%s: ✓ %s  ● %s  ✗ %s  ○ %s  ◐ %s\n",
		schedulerText(lang, "legend"), schedulerText(lang, "status_success"),
		schedulerText(lang, "status_running"), schedulerText(lang, "status_failed"),
		schedulerText(lang, "status_pending"), schedulerText(lang, "status_skipped"))
}

func schedulerShow(s *scheduler.Scheduler, id, lang string) {
	t, ok := s.Task(id)
	if !ok {
		fmt.Fprintln(os.Stderr, schedulerText(lang, "unknown_task", "id", id))
		os.Exit(exUsage)
	}
	state := schedulerText(lang, "enabled")
	if !t.Enabled {
		state = schedulerText(lang, "disabled")
	}
	fmt.Printf("%s: %s (%s)\n", schedulerText(lang, "task_heading"), schedulerTaskName(lang, t), t.ID)
	fmt.Printf("  %s: %s %s\n", schedulerText(lang, "field_status"), schedulerStatusGlyph(t), state)
	fmt.Printf("  %s: %s\n", schedulerText(lang, "field_schedule"), t.Schedule)
	fmt.Printf("  %s: %s\n", schedulerText(lang, "field_last_run"), fmtTime(t.LastRun))
	fmt.Printf("  %s: %s\n", schedulerText(lang, "field_next_run"), fmtTime(t.NextRun))
	fmt.Printf("  %s: %s\n", schedulerText(lang, "field_last_state"), schedulerStatusText(lang, nonEmpty(t.LastStatus, "pending")))
	if t.LastError != "" {
		fmt.Printf("  %s: %s\n", schedulerText(lang, "field_last_error"), t.LastError)
	}
	fmt.Printf("  %s: %s\n", schedulerText(lang, "field_run_count"), schedulerText(lang, "run_counts", "success_count", displayformat.Count(t.RunCount, lang), "fail_count", displayformat.Count(t.FailCount, lang)))
	skippable := schedulerText(lang, "no")
	if t.Skippable {
		skippable = schedulerText(lang, "yes")
	}
	fmt.Printf("  %s: %s\n", schedulerText(lang, "field_skippable"), skippable)
}

// schedulerHistory prints the most recent execution summary. The mandated
// persistent schema (AI.md PART 18 "Scheduler State") stores only the latest
// run plus cumulative counts, not a per-execution log, so history reflects the
// last recorded run.
func schedulerHistory(s *scheduler.Scheduler, id, lang string) {
	t, ok := s.Task(id)
	if !ok {
		fmt.Fprintln(os.Stderr, schedulerText(lang, "unknown_task", "id", id))
		os.Exit(exUsage)
	}
	fmt.Printf("%s: %s (%s)\n", schedulerText(lang, "history_heading"), schedulerTaskName(lang, t), t.ID)
	if t.LastRun.IsZero() {
		fmt.Println("  " + schedulerText(lang, "no_executions"))
		return
	}
	fmt.Printf("  %s  %-8s  %s\n", fmtTime(t.LastRun),
		schedulerStatusText(lang, nonEmpty(t.LastStatus, "pending")), t.LastError)
	fmt.Printf("  %s: %s\n", schedulerText(lang, "totals"), schedulerText(lang, "run_counts", "success_count", displayformat.Count(t.RunCount, lang), "fail_count", displayformat.Count(t.FailCount, lang)))
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
