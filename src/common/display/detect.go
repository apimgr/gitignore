// Package display auto-detects which UI mode a binary should run in
// (headless, CLI, TUI, or GUI) from the process environment — TTY status,
// SSH/Mosh remoting, TERM value, and (on platforms that support it) native
// display presence. See AI.md PART 32 "Automatic Mode Detection": binaries
// never expose a --tui/--cli/--gui flag; mode is always inferred.
package display

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// DisplayMode is the resolved UI mode for the current process.
type DisplayMode int

// Display modes, in escalating richness order.
const (
	DisplayModeHeadless DisplayMode = iota
	DisplayModeCLI
	DisplayModeTUI
	DisplayModeGUI
)

// DisplayEnv captures everything DetectDisplayEnv observed about the
// current process environment.
type DisplayEnv struct {
	Mode         DisplayMode
	HasDisplay   bool
	DisplayType  string
	IsTerminal   bool
	IsSSH        bool
	IsMosh       bool
	IsScreen     bool
	TerminalType string
	Cols         int
	Rows         int
}

// DetectDisplayEnv inspects the current process environment and returns a
// fully populated DisplayEnv, including the auto-detected Mode.
func DetectDisplayEnv() DisplayEnv {
	e := DisplayEnv{
		TerminalType: os.Getenv("TERM"),
	}

	e.IsTerminal = term.IsTerminal(int(os.Stdout.Fd()))
	if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		e.Cols, e.Rows = cols, rows
	}

	e.IsSSH = os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != ""
	e.IsMosh = os.Getenv("MOSH_CONNECTION") != "" || strings.Contains(os.Getenv("TERM"), "mosh")
	e.IsScreen = os.Getenv("STY") != "" || os.Getenv("TMUX") != ""

	e.HasDisplay, e.DisplayType = detectPlatformDisplay()

	e.Mode = e.autoDetectDisplayMode()
	return e
}

// autoDetectDisplayMode implements AI.md PART 32's mode-selection rules:
// remote sessions (SSH/Mosh) always prefer TUI over GUI even with a
// forwarded display; a dumb/unset terminal or non-interactive stdout never
// gets TUI or GUI; a native display with no remoting allows GUI.
func (e *DisplayEnv) autoDetectDisplayMode() DisplayMode {
	if !e.IsTerminal || e.IsDumbTerminal() {
		return DisplayModeCLI
	}
	if e.IsSSH || e.IsMosh {
		return DisplayModeTUI
	}
	if e.HasDisplay {
		return DisplayModeGUI
	}
	return DisplayModeTUI
}

// IsDumbTerminal reports whether TERM is unset or "dumb" — always forces
// plain/CLI output, never TUI or GUI.
func (e *DisplayEnv) IsDumbTerminal() bool {
	return e.TerminalType == "" || e.TerminalType == "dumb"
}

// IsAutoDetectDisplayModeGUI reports whether Mode resolved to GUI.
func (e DisplayEnv) IsAutoDetectDisplayModeGUI() bool { return e.Mode == DisplayModeGUI }

// IsAutoDetectDisplayModeTUI reports whether Mode resolved to TUI.
func (e DisplayEnv) IsAutoDetectDisplayModeTUI() bool { return e.Mode == DisplayModeTUI }

// IsAutoDetectDisplayModeCLI reports whether Mode resolved to CLI.
func (e DisplayEnv) IsAutoDetectDisplayModeCLI() bool { return e.Mode == DisplayModeCLI }

// IsAutoDetectDisplayModeHeadless reports whether Mode resolved to Headless.
func (e DisplayEnv) IsAutoDetectDisplayModeHeadless() bool { return e.Mode == DisplayModeHeadless }
