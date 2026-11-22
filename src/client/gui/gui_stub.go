//go:build !gui || (!linux && !darwin && !windows)

package gui

import (
	"errors"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/config"
)

var errUnavailable = errors.New("GUI support is unavailable in this build or on this platform")

// IsGUIAvailable reports whether the native GUI was compiled for this target.
func IsGUIAvailable() bool { return false }

// LaunchGUI reports that this binary was built without GUI support.
func LaunchGUI(_ *api.Client, _ *config.Config, _ string) error { return errUnavailable }
