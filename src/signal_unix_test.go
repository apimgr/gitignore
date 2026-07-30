//go:build !windows

package main

import (
	"syscall"
	"testing"
)

// TestClassifySignal verifies the Unix signal-to-action mapping (AI.md PART 8
// signal table). SIGUSR1 reopens logs, SIGUSR2 dumps status, every other
// subscribed signal triggers graceful shutdown.
func TestClassifySignal(t *testing.T) {
	cases := []struct {
		name string
		sig  syscall.Signal
		want sigAction
	}{
		{"SIGTERM", syscall.SIGTERM, sigActionShutdown},
		{"SIGINT", syscall.SIGINT, sigActionShutdown},
		{"SIGQUIT", syscall.SIGQUIT, sigActionShutdown},
		{"SIGUSR1", syscall.SIGUSR1, sigActionReopenLogs},
		{"SIGUSR2", syscall.SIGUSR2, sigActionStatusDump},
	}
	for _, c := range cases {
		if got := classifySignal(c.sig); got != c.want {
			t.Errorf("classifySignal(%s) = %d, want %d", c.name, got, c.want)
		}
	}
}
