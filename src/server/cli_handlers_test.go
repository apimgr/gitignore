package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCLIScriptShDefaultsAndOverride(t *testing.T) {
	s := newTestCompatServer(t)

	t.Run("default templates", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/sh", nil)
		rec := httptest.NewRecorder()
		s.handleCLIScriptSh(rec, req)
		if !strings.Contains(rec.Body.String(), "linux,macos,windows") {
			t.Errorf("expected default templates in script: %s", rec.Body.String())
		}
	})

	t.Run("custom defaults query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/sh?defaults=go,node", nil)
		rec := httptest.NewRecorder()
		s.handleCLIScriptSh(rec, req)
		if !strings.Contains(rec.Body.String(), "go,node") {
			t.Errorf("expected custom defaults in script: %s", rec.Body.String())
		}
	})
}

func TestHandleCLIScriptPs(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/ps", nil)
	rec := httptest.NewRecorder()
	s.handleCLIScriptPs(rec, req)
	if !strings.Contains(rec.Body.String(), "windows,visualstudio,vscode") {
		t.Errorf("expected default templates in script: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "gitignore.ps1") {
		t.Errorf("Content-Disposition = %q", got)
	}
}

func TestHandleCLICompletionBash(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/completion/bash", nil)
	rec := httptest.NewRecorder()
	s.handleCLICompletionBash(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "_gitignore_completions") || !strings.Contains(body, "Go") {
		t.Errorf("expected bash completion with template names: %s", body)
	}
}

func TestHandleCLICompletionZsh(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/completion/zsh", nil)
	rec := httptest.NewRecorder()
	s.handleCLICompletionZsh(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "#compdef gitignore") || !strings.Contains(body, "'Go'") {
		t.Errorf("expected zsh completion with template names: %s", body)
	}
}

func TestHandleCLICompletionFish(t *testing.T) {
	s := newTestCompatServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/completion/fish", nil)
	rec := httptest.NewRecorder()
	s.handleCLICompletionFish(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "__fish_use_subcommand") || !strings.Contains(body, "Go") {
		t.Errorf("expected fish completion with template names: %s", body)
	}
}
