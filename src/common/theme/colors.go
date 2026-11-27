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
	Background: "#282a36",
	Foreground: "#f8f8f2",
	Primary:    "#bd93f9",
	Secondary:  "#50fa7b",
	Accent:     "#ff79c6",
	Success:    "#50fa7b",
	Warning:    "#ffb86c",
	Error:      "#ff5555",
	Info:       "#8be9fd",
	Surface:    "#2b2d3a",
	SurfaceAlt: "#21222c",
	Border:     "#44475a",
	Muted:      "#939fcc",
}

// ThemePaletteLight is the default light theme palette.
var ThemePaletteLight = ThemePalette{
	Background: "#ffffff",
	Foreground: "#1f2328",
	Primary:    "#0969da",
	Secondary:  "#1a7f37",
	Accent:     "#8250df",
	Success:    "#1a7f37",
	Warning:    "#9a6700",
	Error:      "#d1242f",
	Info:       "#0969da",
	Surface:    "#f6f8fa",
	SurfaceAlt: "#eff2f5",
	Border:     "#d1d9e0",
	Muted:      "#59636e",
}
