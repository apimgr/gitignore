package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/geoip"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/scheduler"
)

// buildScheduler constructs the scheduler from config and registers every
// built-in task (AI.md PART 18). Backup handlers are injected here because
// backup logic lives in package main.
func buildScheduler(cfg *config.Config, configDir, dataDir, logsDir string, gm *geoip.Manager) (*scheduler.Scheduler, error) {
	catchUp := time.Hour
	if cfg.Server.Schedule.CatchUpWindow != "" {
		if d, err := time.ParseDuration(cfg.Server.Schedule.CatchUpWindow); err == nil && d > 0 {
			catchUp = d
		}
	}

	s := scheduler.New(scheduler.Config{
		Timezone:      cfg.Server.Schedule.Timezone,
		CatchUpWindow: catchUp,
		// scheduler_error operator email (AI.md PART 17). notifier is nil on
		// CLI-only paths, so this safely no-ops there.
		OnError: func(id, name string, err error, nextRun time.Time) {
			emitSchedulerError(id, name, err.Error(), fmtTime(nextRun))
		},
	})

	retention := cfg.Server.Maintenance.Cleanup.LogRetentionDays
	if retention <= 0 {
		retention = 7
	}

	deps := scheduler.Deps{
		LogsDir:          logsDir,
		LogRetentionDays: retention,
		SSLEnabled:       cfg.Server.SSL.Enabled,
		BackupEnabled:    true,
		BackupDaily:      backupTaskHandler(cfg, configDir, dataDir, "daily"),
		BackupHourly:     backupTaskHandler(cfg, configDir, dataDir, "hourly"),
		UpdateCheck:      updateCheckHandler(cfg, dataDir),
		TorInstalled:     torBinaryInstalled(cfg),
	}

	if gm != nil {
		deps.GeoIPUpdate = geoipUpdateHandler(gm)
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
		emitBackupComplete(name, backupFileSize(dest))
		return nil
	}
}

// backupFileSize returns a human-readable size for the finished backup file, or
// "unknown" when it cannot be stat'd.
func backupFileSize(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	return humanizeBytes(fi.Size())
}

// humanizeBytes formats a byte count with a binary unit suffix.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// handleSchedulerCommand dispatches the `scheduler` CLI subcommands
// (AI.md PART 18 "CLI Commands"). It initializes the database, loads persisted
// state, and operates without starting the background loop.
func handleSchedulerCommand(args []string, cfg *config.Config, configDir, dataDir, logsDir string, color bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: gitignore scheduler {list|show|run|enable|disable|history} [id]")
		os.Exit(exUsage)
	}

	if err := db.Init(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer db.Close()

	gm := newGeoIP(cfg, dataDir)
	sched, err := buildScheduler(cfg, configDir, dataDir, logsDir, gm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build scheduler: %v\n", err)
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
		schedulerList(sched)
	case "show":
		if id == "" {
			fmt.Fprintln(os.Stderr, "Usage: gitignore scheduler show <id>")
			os.Exit(exUsage)
		}
		schedulerShow(sched, id)
	case "run":
		if id == "" {
			fmt.Fprintln(os.Stderr, "Usage: gitignore scheduler run <id>")
			os.Exit(exUsage)
		}
		fmt.Printf("Running task %q...\n", id)
		if err := sched.RunNow(context.Background(), id); err != nil {
			fmt.Fprintf(os.Stderr, "Task failed: %v\n", err)
			os.Exit(exOSErr)
		}
		fmt.Println("Done.")
	case "enable", "disable":
		if id == "" {
			fmt.Fprintf(os.Stderr, "Usage: gitignore scheduler %s <id>\n", sub)
			os.Exit(exUsage)
		}
		if _, ok := sched.Task(id); !ok {
			fmt.Fprintf(os.Stderr, "Unknown task: %s\n", id)
			os.Exit(exUsage)
		}
		if err := sched.SetEnabled(id, sub == "enable"); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to update task: %v\n", err)
			os.Exit(exIOErr)
		}
		fmt.Printf("Task %q %sd.\n", id, sub)
	case "history":
		if id == "" {
			fmt.Fprintln(os.Stderr, "Usage: gitignore scheduler history <id>")
			os.Exit(exUsage)
		}
		schedulerHistory(sched, id)
	default:
		fmt.Fprintf(os.Stderr, "Unknown scheduler command: %s\n", sub)
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

func schedulerList(s *scheduler.Scheduler) {
	tasks := s.Tasks()
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "  \tTASK\tSCHEDULE\tLAST RUN\tNEXT RUN\tSTATUS")
	for _, t := range tasks {
		state := "enabled"
		if !t.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			schedulerStatusGlyph(t), t.Name, t.Schedule,
			fmtTime(t.LastRun), fmtTime(t.NextRun), state)
	}
	w.Flush()
	fmt.Println("\nLegend: ✓ Success  ● Running  ✗ Failed  ○ Pending  ◐ Skipped")
}

func schedulerShow(s *scheduler.Scheduler, id string) {
	t, ok := s.Task(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown task: %s\n", id)
		os.Exit(exUsage)
	}
	state := "Enabled"
	if !t.Enabled {
		state = "Disabled"
	}
	fmt.Printf("TASK: %s (%s)\n", t.Name, t.ID)
	fmt.Printf("  Status:     %s %s\n", schedulerStatusGlyph(t), state)
	fmt.Printf("  Schedule:   %s\n", t.Schedule)
	fmt.Printf("  Last Run:   %s\n", fmtTime(t.LastRun))
	fmt.Printf("  Next Run:   %s\n", fmtTime(t.NextRun))
	fmt.Printf("  Last State: %s\n", nonEmpty(t.LastStatus, "pending"))
	if t.LastError != "" {
		fmt.Printf("  Last Error: %s\n", t.LastError)
	}
	fmt.Printf("  Run Count:  %d successful, %d failed\n", t.RunCount, t.FailCount)
	fmt.Printf("  Skippable:  %v\n", t.Skippable)
}

// schedulerHistory prints the most recent execution summary. The mandated
// persistent schema (AI.md PART 18 "Scheduler State") stores only the latest
// run plus cumulative counts, not a per-execution log, so history reflects the
// last recorded run.
func schedulerHistory(s *scheduler.Scheduler, id string) {
	t, ok := s.Task(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown task: %s\n", id)
		os.Exit(exUsage)
	}
	fmt.Printf("HISTORY: %s (%s)\n", t.Name, t.ID)
	if t.LastRun.IsZero() {
		fmt.Println("  No recorded executions yet.")
		return
	}
	fmt.Printf("  %s  %-8s  %s\n", fmtTime(t.LastRun),
		nonEmpty(t.LastStatus, "pending"), t.LastError)
	fmt.Printf("  Totals: %d successful, %d failed\n", t.RunCount, t.FailCount)
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
