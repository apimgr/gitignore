//go:build gui && linux

package gui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func isSystemDark() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err != nil {
		return true
	}
	return strings.Contains(strings.ToLower(string(output)), "dark")
}
