//go:build linux

package main

import "testing"

// TestClassifySignalRTMIN3 verifies SIGRTMIN+3 (the container/systemd
// STOPSIGNAL) triggers graceful shutdown on Linux (AI.md PART 8, PART 26).
func TestClassifySignalRTMIN3(t *testing.T) {
	if got := classifySignal(sigRTMIN3); got != sigActionShutdown {
		t.Errorf("classifySignal(SIGRTMIN+3) = %d, want %d", got, sigActionShutdown)
	}
}
