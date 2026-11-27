// Package terminal provides responsive size-breakpoint detection shared by
// CLI table rendering and TUI layout (see AI.md PART 7 "Responsive
// Breakpoints").
package terminal

import (
	"os"

	"golang.org/x/term"
)

// SizeMode buckets terminal dimensions into a responsive breakpoint.
type SizeMode int

// Size breakpoints, narrowest to widest.
const (
	SizeModeMicro SizeMode = iota
	SizeModeMinimal
	SizeModeCompact
	SizeModeStandard
	SizeModeWide
	SizeModeUltrawide
	SizeModeMassive
	SizeMicro     = SizeModeMicro
	SizeMinimal   = SizeModeMinimal
	SizeCompact   = SizeModeCompact
	SizeStandard  = SizeModeStandard
	SizeWide      = SizeModeWide
	SizeUltrawide = SizeModeUltrawide
	SizeMassive   = SizeModeMassive
)

// Column-width breakpoint thresholds.
const (
	breakpointMinimal   = 40
	breakpointCompact   = 60
	breakpointStandard  = 80
	breakpointWide      = 120
	breakpointUltrawide = 200
	breakpointMassive   = 400
)

// TerminalSize contains the terminal dimensions and their responsive mode.
type TerminalSize struct {
	Cols int
	Rows int
	Mode SizeMode
}

// GetTerminalSize reports stdout dimensions, using a standard terminal size
// when stdout is not attached to a terminal.
func GetTerminalSize() TerminalSize {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 {
		cols = 80
	}
	if err != nil || rows <= 0 {
		rows = 24
	}
	return TerminalSize{Cols: cols, Rows: rows, Mode: ModeForSize(cols, rows)}
}

// ModeForSize maps terminal columns and rows to the AI.md responsive mode.
func ModeForSize(cols, rows int) SizeMode {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	switch {
	case cols < breakpointMinimal || rows < 10:
		return SizeMicro
	case cols < breakpointCompact || rows < 16:
		return SizeMinimal
	case cols < breakpointStandard || rows < 24:
		return SizeCompact
	case cols < breakpointWide || rows < 40:
		return SizeStandard
	case cols < breakpointUltrawide || rows < 60:
		return SizeWide
	case cols < breakpointMassive || rows < 80:
		return SizeUltrawide
	default:
		return SizeMassive
	}
}

// ModeForWidth classifies a width using the standard 24-row fallback.
func ModeForWidth(cols int) SizeMode {
	return ModeForSize(cols, 24)
}

// String renders the breakpoint name.
func (m SizeMode) String() string {
	switch m {
	case SizeMicro:
		return "micro"
	case SizeMinimal:
		return "minimal"
	case SizeCompact:
		return "compact"
	case SizeStandard:
		return "standard"
	case SizeWide:
		return "wide"
	case SizeUltrawide:
		return "ultrawide"
	case SizeMassive:
		return "massive"
	default:
		return "unknown"
	}
}

func (m SizeMode) ShowASCIIArt() bool { return m >= SizeModeStandard }
func (m SizeMode) ShowBorders() bool  { return m >= SizeModeCompact }
func (m SizeMode) ShowSidebar() bool  { return m >= SizeModeWide }
func (m SizeMode) ShowIcons() bool    { return m >= SizeModeMinimal }
