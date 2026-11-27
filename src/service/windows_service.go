//go:build windows

package service

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sys/windows/svc"
)

// RunAsWindowsService detects an SCM launch and runs the supplied server
// lifecycle under the Windows Service Control Manager. It returns handled
// false when this process was launched interactively.
func RunAsWindowsService(run func(context.Context, func()) error) (handled bool, err error) {
	isService, err := IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("detect Windows service session: %w", err)
	}
	if !isService {
		return false, nil
	}
	if run == nil {
		return true, fmt.Errorf("Windows service lifecycle callback is nil")
	}
	if err := svc.Run(appName, &windowsServiceHandler{run: run}); err != nil {
		return true, fmt.Errorf("run Windows service: %w", err)
	}
	return true, nil
}

// IsWindowsService reports whether the current process was launched by SCM.
func IsWindowsService() (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("detect Windows service session: %w", err)
	}
	return isService, nil
}

type windowsServiceHandler struct {
	run func(context.Context, func()) error
}

func (h *windowsServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, updates chan<- svc.Status) (bool, uint32) {
	updates <- svc.Status{State: svc.StartPending, WaitHint: 30000, CheckPoint: 1}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan struct{})
	var readyOnce sync.Once
	go func() {
		done <- h.run(ctx, func() { readyOnce.Do(func() { close(ready) }) })
	}()
	started := false

	for {
		select {
		case <-ready:
			if !started {
				started = true
				updates <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
			}
		case err := <-done:
			updates <- stopPendingStatus(err)
			cancel()
			return false, serviceExitCode(err)
		case request, ok := <-requests:
			if !ok {
				cancel()
				err := <-done
				updates <- stopPendingStatus(err)
				return false, serviceExitCode(err)
			}
			switch request.Cmd {
			case svc.Interrogate:
				if started {
					updates <- request.CurrentStatus
				} else {
					updates <- svc.Status{State: svc.StartPending, WaitHint: 30000, CheckPoint: 1}
				}
			case svc.Stop, svc.Shutdown:
				updates <- svc.Status{State: svc.StopPending, WaitHint: 30000, CheckPoint: 1}
				cancel()
				err := <-done
				updates <- stopPendingStatus(err)
				return false, serviceExitCode(err)
			}
		}
	}
}

func stopPendingStatus(err error) svc.Status {
	return svc.Status{State: svc.StopPending, Win32ExitCode: serviceExitCode(err)}
}

func serviceExitCode(err error) uint32 {
	if err != nil {
		return 1
	}
	return 0
}
