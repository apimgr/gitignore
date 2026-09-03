//go:build !windows && !linux

package main

import "os"

// notifyPlatformSignals is a no-op on non-Linux Unix platforms (darwin,
// freebsd). SIGRTMIN+3 is a Linux realtime signal with no portable meaning
// elsewhere, so only the base Unix signals in notifyShutdownSignals apply
// (AI.md PART 8).
func notifyPlatformSignals(ch chan<- os.Signal) {}
