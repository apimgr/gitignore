package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/scheduler"
	"github.com/apimgr/gitignore/src/service"
	"github.com/apimgr/gitignore/src/updater"
)

// updateRepo is the GitHub repository self-update queries for releases (AI.md
// PART 22). It matches the module path org/name.
const updateRepo = "apimgr/gitignore"

// newUpdater builds an updater.Updater from the running version and configured
// channel (AI.md PART 22).
func newUpdater(cfg *config.ServerConfig) *updater.Updater {
	return updater.New(updater.Config{
		Repo:           updateRepo,
		BinaryName:     projectName,
		CurrentVersion: Version,
		Branch:         cfg.Server.Update.Branch,
		BuildEpoch:     buildEpoch(),
	})
}

func updateStatusLines(currentVersion, branch string) [2]string {
	return [2]string{
		cliText("cli.update.current_version", "version", currentVersion),
		cliText("cli.update.branch", "branch", branch),
	}
}

// runUpdateCheck performs `--update check`: it queries the configured channel
// and prints whether an update is available, without touching the binary. It
// requires no privileges. Exit code 0 whether or not an update exists (AI.md
// PART 22 exit codes); a query error exits 1.
func runUpdateCheck(cfg *config.ServerConfig) {
	u := newUpdater(cfg)
	for _, line := range updateStatusLines(Version, cfg.Server.Update.Branch) {
		fmt.Println(line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rel, err := u.Check(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.update.check_failed", "error", err))
		requestExit(1)
	}
	if rel == nil {
		fmt.Println(cliText("cli.update.up_to_date"))
		return
	}
	fmt.Println(cliText("cli.update.available", "version", rel.TagName))
	fmt.Println(cliText("cli.update.install_hint", "binary", filepath.Base(os.Args[0])))
}

// runUpdateInstall performs `--update yes` (the default): it checks the
// configured channel, and when a newer release exists, downloads it, verifies
// the SHA256, and atomically replaces the running binary. The operator restarts
// the service to load the new binary. Exit 0 on success or when already current;
// exit 1 on any failure (AI.md PART 22).
func runUpdateInstall(cfg *config.ServerConfig) {
	u := newUpdater(cfg)
	for _, line := range updateStatusLines(Version, cfg.Server.Update.Branch) {
		fmt.Println(line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	rel, err := u.Check(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.update.check_failed", "error", err))
		requestExit(1)
	}
	if rel == nil {
		fmt.Println(cliText("cli.update.up_to_date"))
		return
	}
	fmt.Println(cliText("cli.update.downloading", "version", rel.TagName))
	if err := u.Install(ctx, rel); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.update.install_failed", "error", err))
		requestExit(1)
	}
	emitUpdateInstalled(Version, rel.TagName)
	fmt.Println(cliText("cli.update.installed", "version", rel.TagName))
}

// updateCheckHandler is the scheduler's update_check task (AI.md PART 18/22). It
// runs the equivalent of `--update check` against the configured channel,
// filtered by defer_days. On finding a newer eligible release it fires the
// update_available operator notification once per version. When auto_install is
// enabled by a process that started elevated it stages the release and requests
// a supervisor restart; the root startup path verifies and applies it. The task
// never injects update notices into public responses — this is an operator
// concern only.
func updateCheckHandler(cfg *config.ServerConfig, dataDir, logsDir string, runtimeDeps []scheduler.Deps) scheduler.HandlerFunc {
	return func(ctx context.Context) error {
		u := newUpdater(cfg)
		rel, err := u.CheckDeferred(ctx, cfg.Server.Update.DeferDays)
		if err != nil {
			return err
		}
		if rel == nil {
			return nil
		}
		if stagedUpdateVersion(dataDir) == rel.TagName {
			return nil
		}
		if lastFailedUpdateVersion(dataDir) == rel.TagName {
			return nil
		}

		// Fire the operator notification once per newly seen version.
		if lastNotifiedUpdate(dataDir) != rel.TagName {
			log.Printf("update_check: update available: %s (current %s)", rel.TagName, Version)
			emitUpdateAvailable(Version, rel.TagName)
			if err := recordNotifiedUpdate(dataDir, rel.TagName); err != nil {
				log.Printf("update_check: failed to record notified version: %v", err)
			}
		}

		if !cfg.Server.Update.AutoInstall {
			return nil
		}

		var runtime scheduler.Deps
		if len(runtimeDeps) > 0 {
			runtime = runtimeDeps[0]
		}
		if !runtime.StartedElevated {
			log.Printf("update_check: install skipped for %s; process did not start elevated", rel.TagName)
			recordUpdateAudit(logsDir, "update.install_skipped", rel.TagName, "not_elevated")
			return nil
		}

		log.Printf("update_check: staging %s", rel.TagName)
		if err := u.Stage(ctx, rel, filepath.Join(dataDir, "update")); err != nil {
			_ = os.RemoveAll(filepath.Join(dataDir, "update"))
			recordFailedUpdateVersion(dataDir, rel.TagName)
			recordUpdateAudit(logsDir, "update.failed", rel.TagName, err.Error())
			emitUpdateFailed(rel.TagName, err.Error())
			return err
		}
		_ = os.Remove(failedUpdateVersionPath(dataDir))
		recordUpdateAudit(logsDir, "update.staged", rel.TagName, "success")
		emitUpdateStaged(rel.TagName)
		manager := service.DetectServiceManager()
		if manager == service.ServiceOpenRC || manager == service.ServiceSysVinit {
			log.Printf("update_check: %s staged; restart the service to apply it", rel.TagName)
			return nil
		}
		if runtime.RestartForUpdate == nil {
			return fmt.Errorf("update %s staged but server restart is unavailable", rel.TagName)
		}
		log.Printf("update_check: %s staged; requesting graceful service restart", rel.TagName)
		return runtime.RestartForUpdate(ctx)
	}
}

func applyStagedUpdate(cfg *config.ServerConfig, dataDir, logsDir string, startedElevated bool) error {
	if !startedElevated {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	updateDir := filepath.Join(dataDir, "update")
	versionData, _ := os.ReadFile(filepath.Join(updateDir, "version"))
	version := strings.TrimSpace(string(versionData))
	applied, err := newUpdater(cfg).ApplyStaged(ctx, updateDir)
	if err != nil {
		log.Printf("update startup: staged update rejected: %v", err)
		recordFailedUpdateVersion(dataDir, version)
		recordUpdateAudit(logsDir, "update.failed", version, err.Error())
		emitUpdateFailed(version, err.Error())
		return nil
	}
	if !applied {
		return nil
	}
	_ = os.Remove(failedUpdateVersionPath(dataDir))
	recordUpdateAudit(logsDir, "update.installed", version, "success")
	emitUpdateInstalled(Version, version)
	if err := updater.RestartSelf(); err != nil {
		log.Printf("update startup: failed to re-execute installed release: %v", err)
		recordFailedUpdateVersion(dataDir, version)
		recordUpdateAudit(logsDir, "update.failed", version, "restart_failed")
		emitUpdateFailed(version, "restart_failed")
	}
	return nil
}

func failedUpdateVersionPath(dataDir string) string {
	return filepath.Join(dataDir, "update_failed_version")
}

func stagedUpdateVersion(dataDir string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, "update", "version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func lastFailedUpdateVersion(dataDir string) string {
	data, err := os.ReadFile(failedUpdateVersionPath(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func recordFailedUpdateVersion(dataDir, version string) {
	if version != "" {
		_ = os.WriteFile(failedUpdateVersionPath(dataDir), []byte(version+"\n"), 0o600)
	}
}

func recordUpdateAudit(logsDir, event, version, reason string) {
	result := "success"
	severity := "info"
	if event == "update.failed" {
		result = "failure"
		severity = "error"
	} else if event == "update.install_skipped" {
		result = "skipped"
	}
	_ = audit.Append(logsDir, audit.Entry{
		Event: event, Category: "system", Severity: severity,
		Actor:   audit.Actor{Type: "system"},
		Details: map[string]any{"version": version, "reason": reason}, Result: result,
	})
}

// updateNotifyStatePath is the file recording the last version the update_check
// task notified about, so the update_available event fires once per version
// rather than on every task run (AI.md PART 22).
func updateNotifyStatePath(dataDir string) string {
	return filepath.Join(dataDir, "update_last_notified")
}

// lastNotifiedUpdate returns the last version the update_check task notified
// about, or "" when none has been recorded.
func lastNotifiedUpdate(dataDir string) string {
	data, err := os.ReadFile(updateNotifyStatePath(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// recordNotifiedUpdate persists tag as the last notified version.
func recordNotifiedUpdate(dataDir, tag string) error {
	path := updateNotifyStatePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tag+"\n"), 0o644)
}
