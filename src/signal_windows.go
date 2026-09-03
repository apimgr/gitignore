//go:build windows

package main

import (
	"os"
	"os/signal"
)

// notifyShutdownSignals subscribes ch to the only signal Windows delivers to a
// console process, os.Interrupt (Ctrl+C / Ctrl+Break). Windows has no SIGHUP,
// SIGQUIT, SIGUSR1, SIGUSR2 or SIGRTMIN+3; a Windows Service receives
// SERVICE_CONTROL_STOP through the service manager instead (AI.md PART 8).
func notifyShutdownSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}

// classifySignal always requests graceful shutdown on Windows: the only
// subscribed signal is os.Interrupt (AI.md PART 8).
func classifySignal(sig os.Signal) sigAction {
	return sigActionShutdown
}
