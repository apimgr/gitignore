//go:build !windows

package main

import (
	"os"
	"syscall"
)

// pidFilePerm returns the PID file permission for the current privilege level
// (AI.md PART 8 "Directory Validation Rules"): 0644 for root, 0600 for user.
func pidFilePerm() os.FileMode {
	if os.Geteuid() == 0 {
		return 0644
	}
	return 0600
}

// pidProcessAlive reports whether a process with the given PID is currently
// running. Signal 0 performs error checking without delivering a signal;
// EPERM means the process exists but is owned by another user.
func pidProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}
