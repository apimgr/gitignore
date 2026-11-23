package main

import (
	"testing"

	"github.com/apimgr/gitignore/src/common/display"
	"github.com/apimgr/gitignore/src/common/i18n"
)

// isTUIEligible must return true exactly for the TUI/GUI display modes
// detected by common/display, and false for anything else (piped, dumb
// terminal, non-tty).
func TestIsTUIEligibleMatchesDisplayMode(t *testing.T) {
	env := display.DetectDisplayEnv()
	want := env.Mode == display.DisplayModeTUI || env.Mode == display.DisplayModeGUI
	if got := isTUIEligible(); got != want {
		t.Errorf("isTUIEligible() = %v, want %v (detected mode=%v)", got, want, env.Mode)
	}
}

func TestClientLanguageSelectionAndFallback(t *testing.T) {
	previousLanguage := clientLanguage
	t.Cleanup(func() { setClientLanguage(previousLanguage, "") })

	setClientLanguage("fr", "")
	if clientLanguage != "fr" {
		t.Fatalf("clientLanguage = %q, want fr", clientLanguage)
	}
	if got, want := clientText("health.status.healthy"), i18n.Translate("fr", "health.status.healthy"); got != want {
		t.Fatalf("French CLI text = %q, want translated health label", got)
	}

	setClientLanguage("unsupported", "")
	if clientLanguage != i18n.DefaultLang {
		t.Fatalf("unsupported language resolved to %q, want %q", clientLanguage, i18n.DefaultLang)
	}
	if got := clientText("health.status.healthy"); got != "Healthy" {
		t.Fatalf("fallback CLI text = %q, want English health label", got)
	}
}
