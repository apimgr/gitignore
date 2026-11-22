//go:build linux

package i2p

import (
	"os/exec"
	"syscall"
)

func setParentDeathSignal(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

func requestRouterStop(command *exec.Cmd) error {
	return command.Process.Signal(syscall.SIGTERM)
}
