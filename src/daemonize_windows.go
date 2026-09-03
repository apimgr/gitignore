//go:build windows

package main

import (
	"fmt"
	"os"
)

// daemonize is unsupported on Windows (AI.md PART 8 "--daemon"); the platform
// uses a Windows service instead. It warns and returns isParent=false so the
// process keeps running in the foreground.
func daemonize() (isParent bool, err error) {
	fmt.Fprintln(os.Stderr, "Warning: --daemon is not supported on Windows; use --service --install instead")
	return false, nil
}
