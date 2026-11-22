//go:build gui && windows

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
	output, err := exec.CommandContext(ctx, "reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "/v", "AppsUseLightTheme").Output()
	if err != nil {
		return true
	}
	return strings.Contains(strings.ToLower(string(output)), "0x0")
}
