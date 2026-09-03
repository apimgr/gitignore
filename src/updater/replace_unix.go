//go:build !windows

package updater

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// replaceBinary replaces the running binary (Unix). Unix permits renaming over
// a running executable: the old image stays mapped in the live process while
// the new file takes over for any subsequent start. The rename is atomic within
// a filesystem, so there is no partially written binary to roll back from; on
// failure the original is left untouched. Permissions are preserved from the
// original binary.
func replaceBinary(currentPath, newBinaryPath string) error {
	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("failed to stat current binary: %w", err)
	}

	// os.Rename can fail with EXDEV when temp and target are on different
	// filesystems. Fall back to a same-directory copy-then-rename so the
	// replacement is still atomic on the target filesystem, and roll back to
	// the original on any failure.
	if err := os.Rename(newBinaryPath, currentPath); err != nil {
		if err := crossDeviceReplace(currentPath, newBinaryPath, info.Mode()); err != nil {
			return err
		}
		return nil
	}

	if err := os.Chmod(currentPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to restore permissions: %w", err)
	}
	return nil
}

// crossDeviceReplace handles the EXDEV case: copy the new binary next to the
// target, then atomically rename it over the target. The backup of the original
// is restored if the final rename fails.
func crossDeviceReplace(currentPath, newBinaryPath string, mode os.FileMode) error {
	stagePath := currentPath + ".new"
	if err := copyFile(newBinaryPath, stagePath, mode); err != nil {
		return fmt.Errorf("failed to stage new binary: %w", err)
	}

	backupPath := currentPath + ".bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		_ = os.Remove(stagePath)
		return fmt.Errorf("failed to back up current binary: %w", err)
	}

	if err := os.Rename(stagePath, currentPath); err != nil {
		// Roll back to the original binary.
		_ = os.Rename(backupPath, currentPath)
		_ = os.Remove(stagePath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

// copyFile copies src to dst, truncating dst, and applies mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// RestartSelf re-executes the current process image with the original argv and
// environment (Unix). syscall.Exec replaces the process in place, so the newly
// installed binary takes over immediately. It only returns on error.
func RestartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = resolveExe(exe)
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// resolveExe resolves exe against PATH so syscall.Exec receives an absolute
// path even if os.Args[0] was a bare name.
func resolveExe(exe string) (string, error) {
	if _, err := os.Stat(exe); err == nil {
		return exe, nil
	}
	return exec.LookPath(exe)
}
