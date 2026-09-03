// Package theme is the single source of truth for this project's color
// palette, shared across the web CSS, TUI, CLI, and GUI front ends (see
// AI.md PART 16 "Theming"). Front ends derive their own styling primitives
// (lipgloss styles, CSS custom properties, ANSI codes) FROM ThemePalette —
// they never hardcode a second, parallel color set.
package theme

// ThemePalette is a complete set of semantic colors for one theme variant
// (dark or light). Hex values follow a Tokyo-Night-style palette.
type ThemePalette struct {
	Background string `json:"background"`
	Foreground string `json:"foreground"`
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Accent     string `json:"accent"`
	Success    string `json:"success"`
	Warning    string `json:"warning"`
	Error      string `json:"error"`
	Info       string `json:"info"`
	Surface    string `json:"surface"`
	SurfaceAlt string `json:"surface_alt"`
	Border     string `json:"border"`
	Muted      string `json:"muted"`
}

// ThemePaletteDark is the default dark theme palette.
var ThemePaletteDark = ThemePalette{
	Background: "#1a1b26",
	Foreground: "#c0caf5",
	Primary:    "#7aa2f7",
	Secondary:  "#bb9af7",
	Accent:     "#7dcfff",
	Success:    "#9ece6a",
	Warning:    "#e0af68",
	Error:      "#f7768e",
	Info:       "#2ac3de",
	Surface:    "#24283b",
	SurfaceAlt: "#414868",
	Border:     "#414868",
	Muted:      "#565f89",
}

// ThemePaletteLight is the default light theme palette.
var ThemePaletteLight = ThemePalette{
	Background: "#d5d6db",
	Foreground: "#343b58",
	Primary:    "#34548a",
	Secondary:  "#5a4a78",
	Accent:     "#0f4b6e",
	Success:    "#485e30",
	Warning:    "#8f5e15",
	Error:      "#8c4351",
	Info:       "#166775",
	Surface:    "#e6e7ed",
	SurfaceAlt: "#c4c6cd",
	Border:     "#9699a3",
	Muted:      "#6c6e75",
}
