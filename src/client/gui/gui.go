//go:build gui

// Package gui would host a native (GTK4/Cocoa/Win32) desktop UI for
// gitignore-cli, per AI.md PART 32 "GUI Requirements" — explicitly NOT
// Electron, NOT a web view, and NOT a wrapped copy of the TUI.
//
// This build environment has no cgo toolchain or display server available
// (headless Docker Go toolchain image), so a real native GUI cannot be
// built or verified here. This file is a stub so the src/client/gui
// package/directory exists per the spec's module layout; it deliberately
// does not attempt a native implementation. A future contributor with
// access to a cgo + GTK4/Cocoa/Win32 toolchain should replace this file.
package gui

import "fmt"

// IsGUIAvailable always reports false in this build — no native GUI
// implementation exists yet.
func IsGUIAvailable() bool { return false }

// LaunchGUI is unimplemented; callers must fall back to the TUI or CLI.
func LaunchGUI(serverURL string) error {
	return fmt.Errorf("native GUI is not implemented in this build (requires cgo + GTK4/Cocoa/Win32, unavailable in this toolchain)")
}
