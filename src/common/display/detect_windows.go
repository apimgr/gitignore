//go:build windows

package display

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// detectPlatformDisplay reports whether a native display session is
// present on Windows. A console-attached process with an active session is
// always treated as having a display since Windows has no headless X11/
// Wayland analog to distinguish.
func detectPlatformDisplay() (bool, string) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil || sessionID == 0 {
		return false, "none"
	}
	if strings.Contains(strings.ToLower(os.Getenv("SESSIONNAME")), "rdp") {
		return true, "windows-rdp"
	}
	return true, "windows"
}
