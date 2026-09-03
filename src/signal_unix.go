//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyShutdownSignals subscribes ch to the Unix signals the server acts on
// (AI.md PART 8 signal table). SIGHUP is explicitly ignored — configuration
// reloads automatically via the file watcher, so a hangup must neither reload
// nor terminate the process. SIGRTMIN+3 is added per-platform (Linux only)
// through notifyPlatformSignals.
func notifyShutdownSignals(ch chan<- os.Signal) {
	signal.Notify(ch,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
	)
	notifyPlatformSignals(ch)
	signal.Ignore(syscall.SIGHUP)
}

// classifySignal maps a received Unix signal to the main-loop action per the
// AI.md PART 8 signal table: SIGUSR1 reopens logs, SIGUSR2 dumps status, and
// every other subscribed signal (SIGTERM, SIGINT, SIGQUIT, SIGRTMIN+3)
// triggers graceful shutdown.
func classifySignal(sig os.Signal) sigAction {
	switch sig {
	case syscall.SIGUSR1:
		return sigActionReopenLogs
	case syscall.SIGUSR2:
		return sigActionStatusDump
	default:
		return sigActionShutdown
	}
}
