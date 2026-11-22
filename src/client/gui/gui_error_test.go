//go:build gui && (linux || darwin || windows)

package gui

import (
	"errors"
	"testing"

	"github.com/apimgr/gitignore/src/client/api"
)

func TestErrorMessageUsesLocalizedAPIMessage(t *testing.T) {
	err := &api.APIError{Status: 404, Message: "Template was not found"}
	if got := errorMessage(err); got != "Template was not found" {
		t.Fatalf("errorMessage() = %q, want API message", got)
	}
}

func TestErrorMessagePreservesLocalError(t *testing.T) {
	err := errors.New("invalid server URL")
	if got := errorMessage(err); got != err.Error() {
		t.Fatalf("errorMessage() = %q, want %q", got, err.Error())
	}
}
