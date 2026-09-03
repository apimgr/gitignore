//go:build linux

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// sigRTMIN3 is SIGRTMIN+3 (signal 37 on Linux). systemd sends it for a clean
// stop and it is the container STOPSIGNAL, so it must trigger graceful
// shutdown (AI.md PART 8, PART 26). It is Linux specific: on darwin/freebsd
// signal 37 is out of range, so it is only registered here.
const sigRTMIN3 = syscall.Signal(37)

// notifyPlatformSignals adds the Linux-only SIGRTMIN+3 to the subscription.
func notifyPlatformSignals(ch chan<- os.Signal) {
	signal.Notify(ch, sigRTMIN3)
}
