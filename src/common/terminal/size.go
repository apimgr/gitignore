// Package terminal provides responsive size-breakpoint detection shared by
// CLI table rendering and TUI layout (see AI.md PART 7 "Responsive
// Breakpoints").
package terminal

// SizeMode buckets a terminal's column width into a responsive breakpoint.
type SizeMode int

// Size breakpoints, narrowest to widest.
const (
	SizeMicro SizeMode = iota
	SizeMinimal
	SizeCompact
	SizeStandard
	SizeWide
	SizeUltrawide
	SizeMassive
)

// Column-width breakpoint thresholds.
const (
	breakpointMinimal   = 40
	breakpointCompact   = 60
	breakpointStandard  = 80
	breakpointWide      = 120
	breakpointUltrawide = 160
)

// ModeForWidth maps a terminal column width to its SizeMode breakpoint.
func ModeForWidth(cols int) SizeMode {
	switch {
	case cols < breakpointMinimal:
		return SizeMicro
	case cols < breakpointCompact:
		return SizeMinimal
	case cols < breakpointStandard:
		return SizeCompact
	case cols < breakpointWide:
		return SizeStandard
	case cols < breakpointUltrawide:
		return SizeWide
	case cols < breakpointUltrawide*2:
		return SizeUltrawide
	default:
		return SizeMassive
	}
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
