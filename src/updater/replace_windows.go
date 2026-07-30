//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// replaceBinary replaces the running binary (Windows). Windows cannot delete or
// rename a running executable's target, but it can rename the running image
// itself. So: rename the running binary to .old, move the new binary into
// place, and best-effort remove the .old file (it succeeds once the old process
// exits; a subsequent update also clears any leftover). On failure the original
// is renamed back.
func replaceBinary(currentPath, newBinaryPath string) error {
	oldPath := currentPath + ".old"

	// Clear any leftover .old from a previous update (safe once the prior
	// process has exited).
	_ = os.Remove(oldPath)

	if err := os.Rename(currentPath, oldPath); err != nil {
		return fmt.Errorf("failed to rename current binary: %w", err)
	}

	if err := os.Rename(newBinaryPath, currentPath); err != nil {
		// Roll back to the original binary.
		_ = os.Rename(oldPath, currentPath)
		return fmt.Errorf("failed to move new binary: %w", err)
	}

	// Best-effort cleanup; the file is still locked while this process runs and
	// will be removed on the next update.
	_ = os.Remove(oldPath)
	return nil
}

// RestartSelf spawns a fresh instance with the original arguments and exits
// (Windows), which does not support exec() replacement.
func RestartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}
	// Give the new process a moment to initialize before the old one exits.
	time.Sleep(100 * time.Millisecond)
	os.Exit(0)
	return nil
}
