//go:build !windows

package display

import "os"

// detectPlatformDisplay reports whether a native display server is present
// on Unix-like platforms (Wayland or X11).
func detectPlatformDisplay() (bool, string) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true, "wayland"
	}
	if os.Getenv("DISPLAY") != "" {
		return true, "x11"
	}
	return false, ""
}
