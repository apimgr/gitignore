//go:build !windows

package display

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// detectPlatformDisplay reports whether a native display server is present
// on Unix-like platforms (Wayland or X11).
func detectPlatformDisplay() (bool, string) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true, "wayland"
	}
	if os.Getenv("DISPLAY") != "" {
		return true, "x11"
	}
	if runtime.GOOS == "darwin" {
		remote := os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""
		if !remote && os.Getenv("__CFBundleIdentifier") != "" {
			return true, "macos"
		}
		if !remote {
			if output, err := exec.Command("launchctl", "managername").Output(); err == nil && strings.Contains(string(output), "Aqua") {
				return true, "macos"
			}
		}
	}
	return false, ""
}
