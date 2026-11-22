//go:build gui && darwin

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
	output, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.Contains(strings.ToLower(string(output)), "dark")
}
