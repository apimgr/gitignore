package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/gitignore/src/client/config"
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

func TestResolveClientTokenPriorityAndTokenFile(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Token: "configured-token"}}
	t.Setenv("GITIGNORE_TOKEN", "environment-token")
	if got, err := resolveClientToken("flag-token", cfg); err != nil || got != "flag-token" {
		t.Fatalf("flag token = %q, %v", got, err)
	}
	if got, err := resolveClientToken("", cfg); err != nil || got != "environment-token" {
		t.Fatalf("environment token = %q, %v", got, err)
	}
	t.Setenv("GITIGNORE_TOKEN", "")
	if got, err := resolveClientToken("", cfg); err != nil || got != "configured-token" {
		t.Fatalf("configured token = %q, %v", got, err)
	}
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Auth.TokenFile = tokenPath
	if got, err := resolveClientToken("", cfg); err != nil || got != "file-token" {
		t.Fatalf("token-file value = %q, %v", got, err)
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
