//go:build !linux && !windows

package i2p

import (
	"os/exec"
	"syscall"
)

func setParentDeathSignal(command *exec.Cmd) {}

func requestRouterStop(command *exec.Cmd) error {
	return command.Process.Signal(syscall.SIGTERM)
}
