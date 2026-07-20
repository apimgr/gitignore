// Package output provides colorized/plain terminal output helpers for the
// CLI, following the same auto/yes/no + NO_COLOR resolution as the server
// (see src/main.go resolveColor) and the sysexits-style CLI exit codes
// defined in AI.md PART 32 "Error Handling".
package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// CLI-specific exit codes (AI.md PART 32 "Error Handling"). These are
// distinct from the server binary's sysexits(3) codes — the client spec
// defines its own smaller table and this binary follows it.
const (
	ExitSuccess    = 0
	ExitGeneral    = 1
	ExitConfig     = 2
	ExitConnection = 3
	ExitAuth       = 4
	ExitNotFound   = 5
	ExitUsage      = 64
)

// ResolveColor mirrors src/main.go's resolveColor: "yes"/"no" force the
// setting, otherwise NO_COLOR wins, otherwise auto-detect via isatty.
func ResolveColor(flagValue string) bool {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "yes":
		return true
	case "no":
		return false
	default:
		if _, set := os.LookupEnv("NO_COLOR"); set {
			return false
		}
		return isatty.IsTerminal(os.Stdout.Fd())
	}
}

// Printer wraps stdout/stderr writes with optional ANSI color.
type Printer struct {
	Color bool
}

func New(color bool) *Printer { return &Printer{Color: color} }

func (p *Printer) colorize(code, s string) string {
	if !p.Color {
		return s
	}
	return code + s + "\x1b[0m"
}

// Bold renders s bold when color is enabled.
func (p *Printer) Bold(s string) string { return p.colorize("\x1b[1m", s) }

// Green renders s green (success) when color is enabled.
func (p *Printer) Green(s string) string { return p.colorize("\x1b[32m", s) }

// Red renders s red (error) when color is enabled.
func (p *Printer) Red(s string) string { return p.colorize("\x1b[31m", s) }

// Yellow renders s yellow (warning) when color is enabled.
func (p *Printer) Yellow(s string) string { return p.colorize("\x1b[33m", s) }

// Cyan renders s cyan (headers) when color is enabled.
func (p *Printer) Cyan(s string) string { return p.colorize("\x1b[36m", s) }

// Error prints a formatted error to stderr as "Error: {msg}".
func (p *Printer) Error(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, p.Red("Error: ")+fmt.Sprintf(format, args...))
}

// Warn prints a formatted warning to stderr.
func (p *Printer) Warn(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, p.Yellow("Warning: ")+fmt.Sprintf(format, args...))
}

// FormatTable renders rows as a simple aligned, box-drawn table.
func FormatTable(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder
	writeSep := func(left, mid, right string) {
		b.WriteString(left)
		for i, w := range widths {
			b.WriteString(strings.Repeat("─", w+2))
			if i < len(widths)-1 {
				b.WriteString(mid)
			}
		}
		b.WriteString(right + "\n")
	}
	writeRow := func(cells []string) {
		b.WriteString("│")
		for i, w := range widths {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			b.WriteString(" " + cell + strings.Repeat(" ", w-len(cell)) + " │")
		}
		b.WriteString("\n")
	}

	writeSep("┌", "┬", "┐")
	writeRow(headers)
	writeSep("├", "┼", "┤")
	for _, row := range rows {
		writeRow(row)
	}
	writeSep("└", "┴", "┘")
	return b.String()
}
