package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	apppath "github.com/apimgr/gitignore/src/path"
)

// writePIDFile writes the current process PID to path (AI.md PART 8 "--pid").
// Containers skip PID files entirely. Existing files are checked for a live
// process: a running instance is a fatal "already running" error, while a stale
// file is removed and superseded. The file contains the decimal PID as text.
func writePIDFile(path string) error {
	if path == "" || apppath.IsRunningInContainer() {
		return nil
	}

	if data, err := os.ReadFile(path); err == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil && pid > 0 {
			if pid != os.Getpid() && pidProcessAlive(pid) {
				return fmt.Errorf("already running (pid %d, %s)", pid, path)
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), pidFilePerm()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	return nil
}

// removePIDFile deletes the PID file on shutdown, but only when it still holds
// this process's PID so a superseding instance's file is never clobbered.
func removePIDFile(path string) {
	if path == "" {
		return
	}
	if data, err := os.ReadFile(path); err == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil && pid != os.Getpid() {
			return
		}
	}
	_ = os.Remove(path)
}
