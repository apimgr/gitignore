//go:build !windows

package service

import "context"

// IsWindowsService reports false when built outside Windows.
func IsWindowsService() (bool, error) { return false, nil }

// RunAsWindowsService is a no-op when the binary is not built for Windows.
func RunAsWindowsService(func(context.Context, func()) error) (bool, error) {
	return false, nil
}
