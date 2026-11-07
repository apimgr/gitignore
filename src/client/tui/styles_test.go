package tui

import (
	"testing"

	"github.com/apimgr/gitignore/src/common/theme"
)

func TestStylesFromThemePalette(t *testing.T) {
	styles := StylesFromThemePalette(theme.ThemePaletteDark)
	if styles.Title.GetForeground() == nil || styles.Error.GetForeground() == nil {
		t.Fatal("theme styles omitted semantic colors")
	}
	if defaultStyles().Title.GetForeground() == nil {
		t.Fatal("default styles are empty")
	}
}
