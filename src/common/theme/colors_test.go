package theme

import "testing"

func TestSemanticPalettes(t *testing.T) {
	for name, palette := range map[string]ThemePalette{"dark": ThemePaletteDark, "light": ThemePaletteLight} {
		if palette.Background == "" || palette.Foreground == "" || palette.Primary == "" || palette.Secondary == "" || palette.Accent == "" || palette.Success == "" || palette.Warning == "" || palette.Error == "" || palette.Info == "" || palette.Surface == "" || palette.SurfaceAlt == "" || palette.Border == "" || palette.Muted == "" {
			t.Errorf("%s palette is missing a semantic color: %#v", name, palette)
		}
	}
	if ThemePaletteDark.Background == ThemePaletteLight.Background {
		t.Fatal("dark and light backgrounds are identical")
	}
}
