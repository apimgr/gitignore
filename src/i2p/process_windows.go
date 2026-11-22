//go:build windows

package i2p

import (
	"os"
	"os/exec"
)

func setParentDeathSignal(command *exec.Cmd) {}

func requestRouterStop(command *exec.Cmd) error {
	return command.Process.Signal(os.Interrupt)
}
