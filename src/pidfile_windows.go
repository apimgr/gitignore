//go:build windows

package main

import "os"

// pidFilePerm mirrors the Unix helper; Windows ignores POSIX mode bits.
func pidFilePerm() os.FileMode { return 0644 }

// pidProcessAlive is unused on Windows because PID files are disabled there
// (GetPIDFile returns an empty path), but the symbol must exist to compile.
func pidProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc != nil
}
