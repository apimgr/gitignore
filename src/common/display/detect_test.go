package display

import "testing"

func TestIsDumbTerminal(t *testing.T) {
	cases := []struct {
		term string
		want bool
	}{
		{"", true},
		{"dumb", true},
		{"xterm-256color", false},
		{"screen", false},
	}
	for _, tc := range cases {
		e := DisplayEnv{TerminalType: tc.term}
		if got := e.IsDumbTerminal(); got != tc.want {
			t.Errorf("IsDumbTerminal(TERM=%q) = %v, want %v", tc.term, got, tc.want)
		}
	}
}

func TestAutoDetectDisplayModeNonInteractive(t *testing.T) {
	e := DisplayEnv{IsTerminal: false, TerminalType: "xterm"}
	if got := e.autoDetectDisplayMode(); got != DisplayModeCLI {
		t.Errorf("non-interactive stdout = %v, want CLI", got)
	}
}

func TestAutoDetectDisplayModeDumbTerminal(t *testing.T) {
	e := DisplayEnv{IsTerminal: true, TerminalType: "dumb"}
	if got := e.autoDetectDisplayMode(); got != DisplayModeCLI {
		t.Errorf("dumb terminal = %v, want CLI", got)
	}
}

func TestAutoDetectDisplayModeSSHPrefersTUIEvenWithDisplay(t *testing.T) {
	e := DisplayEnv{IsTerminal: true, TerminalType: "xterm", IsSSH: true, HasDisplay: true}
	if got := e.autoDetectDisplayMode(); got != DisplayModeTUI {
		t.Errorf("SSH with display = %v, want TUI (remote always prefers TUI)", got)
	}
}

func TestAutoDetectDisplayModeMoshPrefersTUI(t *testing.T) {
	e := DisplayEnv{IsTerminal: true, TerminalType: "xterm", IsMosh: true, HasDisplay: true}
	if got := e.autoDetectDisplayMode(); got != DisplayModeTUI {
		t.Errorf("Mosh with display = %v, want TUI", got)
	}
}

func TestAutoDetectDisplayModeLocalWithDisplayIsGUI(t *testing.T) {
	e := DisplayEnv{IsTerminal: true, TerminalType: "xterm", HasDisplay: true}
	if got := e.autoDetectDisplayMode(); got != DisplayModeGUI {
		t.Errorf("local terminal with display = %v, want GUI", got)
	}
}

func TestAutoDetectDisplayModeLocalNoDisplayIsTUI(t *testing.T) {
	e := DisplayEnv{IsTerminal: true, TerminalType: "xterm", HasDisplay: false}
	if got := e.autoDetectDisplayMode(); got != DisplayModeTUI {
		t.Errorf("local terminal without display = %v, want TUI", got)
	}
}

func TestDisplayModePredicates(t *testing.T) {
	cases := []struct {
		mode                    DisplayMode
		gui, tui, cli, headless bool
	}{
		{DisplayModeHeadless, false, false, false, true},
		{DisplayModeCLI, false, false, true, false},
		{DisplayModeTUI, false, true, false, false},
		{DisplayModeGUI, true, false, false, false},
	}
	for _, tc := range cases {
		e := DisplayEnv{Mode: tc.mode}
		if got := e.IsAutoDetectDisplayModeGUI(); got != tc.gui {
			t.Errorf("mode %v: IsAutoDetectDisplayModeGUI() = %v, want %v", tc.mode, got, tc.gui)
		}
		if got := e.IsAutoDetectDisplayModeTUI(); got != tc.tui {
			t.Errorf("mode %v: IsAutoDetectDisplayModeTUI() = %v, want %v", tc.mode, got, tc.tui)
		}
		if got := e.IsAutoDetectDisplayModeCLI(); got != tc.cli {
			t.Errorf("mode %v: IsAutoDetectDisplayModeCLI() = %v, want %v", tc.mode, got, tc.cli)
		}
		if got := e.IsAutoDetectDisplayModeHeadless(); got != tc.headless {
			t.Errorf("mode %v: IsAutoDetectDisplayModeHeadless() = %v, want %v", tc.mode, got, tc.headless)
		}
	}
}

func TestDetectDisplayEnvRunsWithoutPanicking(t *testing.T) {
	// In the test environment stdout is not a terminal, so this should
	// resolve to a stable, non-interactive mode without error.
	e := DetectDisplayEnv()
	if e.IsTerminal {
		t.Skip("stdout unexpectedly a terminal in this environment; skipping mode assertion")
	}
	if e.Mode != DisplayModeCLI {
		t.Errorf("non-terminal stdout Mode = %v, want CLI", e.Mode)
	}
}
