// Package tui implements gitignore-cli's default interactive mode: a
// bubbletea TUI that reuses src/client/api and src/client/config directly
// (see AI.md PART 32 "TUI Requirements" — no duplicated API logic).
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/apimgr/gitignore/src/common/theme"
)

// Styles is the set of lipgloss styles the TUI renders with, derived from
// a shared theme.ThemePalette rather than a second hardcoded color table.
type Styles struct {
	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Status     lipgloss.Style
	Error      lipgloss.Style
	Success    lipgloss.Style
	Help       lipgloss.Style
	Border     lipgloss.Style
	Selected   lipgloss.Style
	Muted      lipgloss.Style
	InputLabel lipgloss.Style
}

// StylesFromThemePalette derives a full Styles set from p.
func StylesFromThemePalette(p theme.ThemePalette) Styles {
	return Styles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(p.Primary)).
			Padding(0, 1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Secondary)),
		Status: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Info)),
		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(p.Error)),
		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Success)),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)),
		Border: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(p.Border)),
		Selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(p.Accent)),
		Muted: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)),
		InputLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(p.Primary)),
	}
}

// defaultStyles picks dark or light based on the terminal background
// heuristic used elsewhere in the project (dark by default per AI.md
// "dark mode default").
func defaultStyles() Styles {
	return StylesFromThemePalette(theme.ThemePaletteDark)
}
