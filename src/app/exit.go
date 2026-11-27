package app

import "fmt"

// ExitError reports a requested process exit to the thin executable entry point.
type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("application requested exit with status %d", e.Code)
}

func requestExit(code int) {
	panic(ExitError{Code: code})
}
