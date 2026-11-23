//go:build !gui || (!linux && !darwin && !windows)

package gui

import (
	"errors"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/config"
	"github.com/apimgr/gitignore/src/common/i18n"
)

// IsGUIAvailable reports whether the native GUI was compiled for this target.
func IsGUIAvailable() bool { return false }

func clientLanguage(client *api.Client) string {
	if client != nil && client.Lang != "" {
		return client.Lang
	}
	return i18n.DefaultLang
}

// LaunchGUI reports that this binary was built without GUI support.
func LaunchGUI(client *api.Client, _ *config.Config, _ string) error {
	return errors.New(i18n.Translate(clientLanguage(client), "cli.client.gui.unavailable"))
}
