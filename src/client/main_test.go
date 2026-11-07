package main

import (
	"testing"

	"github.com/apimgr/gitignore/src/common/display"
)

// isTUIEligible must return true exactly for the TUI/GUI display modes
// detected by common/display, and false for anything else (piped, dumb
// terminal, non-tty).
func TestIsTUIEligibleMatchesDisplayMode(t *testing.T) {
	env := display.DetectDisplayEnv()
	want := env.Mode == display.DisplayModeTUI || env.Mode == display.DisplayModeGUI
	if got := isTUIEligible(); got != want {
		t.Errorf("isTUIEligible() = %v, want %v (detected mode=%v)", got, want, env.Mode)
	}
}
