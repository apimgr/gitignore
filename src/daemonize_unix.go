//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// daemonize detaches the server from the controlling terminal (AI.md PART 8
// "--daemon"). The parent re-executes the binary with the _DAEMON_CHILD marker
// and Setsid so the child leads a new session with stdio connected to the null
// device, then reports the child PID and returns isParent=true so the caller
// exits 0. In the already-detached child it returns isParent=false to continue
// normal startup.
func daemonize() (isParent bool, err error) {
	if os.Getenv("_DAEMON_CHILD") == "1" {
		return false, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("failed to resolve executable: %w", err)
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "_DAEMON_CHILD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("failed to start daemon: %w", err)
	}
	fmt.Printf("Daemon started with PID %d\n", cmd.Process.Pid)
	return true, nil
}
