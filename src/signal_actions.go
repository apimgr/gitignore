package main

import (
	"log"
	"runtime"
)

// sigAction is the action the main loop performs in response to a received OS
// signal. The signal-to-action mapping is platform specific (see
// signal_unix.go / signal_windows.go) but the actions themselves are shared
// (AI.md PART 8 "Signal Handling & Graceful Shutdown").
type sigAction int

const (
	// sigActionShutdown triggers an orderly graceful shutdown. Mapped from
	// SIGTERM, SIGINT, SIGQUIT and SIGRTMIN+3 on Unix and os.Interrupt on
	// Windows.
	sigActionShutdown sigAction = iota
	// sigActionReopenLogs reopens log files for external rotation. Mapped
	// from SIGUSR1 (Unix only).
	sigActionReopenLogs
	// sigActionStatusDump writes a runtime status snapshot to the log.
	// Mapped from SIGUSR2 (Unix only).
	sigActionStatusDump
)

// reopenLogs handles the SIGUSR1 "reopen logs" request (AI.md PART 8). The
// server writes diagnostic output to stderr, which a supervisor (systemd,
// Docker, logrotate with copytruncate) rotates externally, so there is no
// process-owned file handle to reopen. The acknowledgment is logged so an
// operator driving a rotation can confirm the signal was delivered.
func reopenLogs() {
	log.Println("SIGUSR1: log reopen acknowledged (stderr is rotated by the supervisor)")
}

// dumpStatus handles the SIGUSR2 "status dump" request (AI.md PART 8) by
// writing a runtime snapshot to the log: goroutine count and memory usage.
func dumpStatus() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("SIGUSR2 status dump: goroutines=%d alloc=%dKiB sys=%dKiB numgc=%d",
		runtime.NumGoroutine(), m.Alloc/1024, m.Sys/1024, m.NumGC)
}
