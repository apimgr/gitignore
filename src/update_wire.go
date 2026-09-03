package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/scheduler"
	"github.com/apimgr/gitignore/src/updater"
)

// updateRepo is the GitHub repository self-update queries for releases (AI.md
// PART 22). It matches the module path org/name.
const updateRepo = "apimgr/gitignore"

// newUpdater builds an updater.Updater from the running version and configured
// channel (AI.md PART 22).
func newUpdater(cfg *config.Config) *updater.Updater {
	return updater.New(updater.Config{
		Repo:           updateRepo,
		BinaryName:     projectName,
		CurrentVersion: Version,
		Branch:         cfg.Server.Update.Branch,
	})
}

// runUpdateCheck performs `--update check`: it queries the configured channel
// and prints whether an update is available, without touching the binary. It
// requires no privileges. Exit code 0 whether or not an update exists (AI.md
// PART 22 exit codes); a query error exits 1.
func runUpdateCheck(cfg *config.Config) {
	u := newUpdater(cfg)
	fmt.Printf("Current version: %s\n", Version)
	fmt.Printf("Update branch:   %s\n", cfg.Server.Update.Branch)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rel, err := u.Check(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Update check failed: %v\n", err)
		os.Exit(1)
	}
	if rel == nil {
		fmt.Println("You are running the latest version.")
		return
	}
	fmt.Printf("Update available: %s\n", rel.TagName)
	fmt.Printf("Run '%s --update yes' to install.\n", projectName)
}

// runUpdateInstall performs `--update yes` (the default): it checks the
// configured channel, and when a newer release exists, downloads it, verifies
// the SHA256, and atomically replaces the running binary. The operator restarts
// the service to load the new binary. Exit 0 on success or when already current;
// exit 1 on any failure (AI.md PART 22).
func runUpdateInstall(cfg *config.Config) {
	u := newUpdater(cfg)
	fmt.Printf("Current version: %s\n", Version)
	fmt.Printf("Update branch:   %s\n", cfg.Server.Update.Branch)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	rel, err := u.Check(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Update check failed: %v\n", err)
		os.Exit(1)
	}
	if rel == nil {
		fmt.Println("You are running the latest version.")
		return
	}
	fmt.Printf("Downloading %s...\n", rel.TagName)
	if err := u.Install(ctx, rel); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		os.Exit(1)
	}
	emitUpdateInstalled(Version, rel.TagName)
	fmt.Printf("Updated to %s. Restart the service to load the new binary.\n", rel.TagName)
}

// updateCheckHandler is the scheduler's update_check task (AI.md PART 18/22). It
// runs the equivalent of `--update check` against the configured channel,
// filtered by defer_days. On finding a newer eligible release it fires the
// update_available operator notification once per version. When auto_install is
// enabled it additionally installs the release and re-execs the server so the
// new binary takes over. The task never injects update notices into public
// responses — this is an operator concern only.
func updateCheckHandler(cfg *config.Config, dataDir string) scheduler.HandlerFunc {
	return func(ctx context.Context) error {
		u := newUpdater(cfg)
		rel, err := u.CheckDeferred(ctx, cfg.Server.Update.DeferDays)
		if err != nil {
			return err
		}
		if rel == nil {
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

		log.Printf("update_check: auto-installing %s", rel.TagName)
		if err := u.Install(ctx, rel); err != nil {
			return err
		}
		emitUpdateInstalled(Version, rel.TagName)
		log.Printf("update_check: installed %s, re-executing", rel.TagName)
		// Re-exec so the running server loads the new binary (AI.md PART 22
		// "Restart service or re-exec"). Only returns on error.
		return updater.RestartSelf()
	}
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
