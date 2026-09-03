//go:build windows

package display

// detectPlatformDisplay reports whether a native display session is
// present on Windows. A console-attached process with an active session is
// always treated as having a display since Windows has no headless X11/
// Wayland analog to distinguish.
func detectPlatformDisplay() (bool, string) {
	return true, "windows"
}
