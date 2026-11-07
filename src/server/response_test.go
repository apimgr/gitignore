package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMapAPIErrorCodeToHTTPStatus(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{"BAD_REQUEST", http.StatusBadRequest},
		{"UNAUTHORIZED", http.StatusUnauthorized},
		{"FORBIDDEN", http.StatusForbidden},
		{"NOT_FOUND", http.StatusNotFound},
		{"RATE_LIMITED", http.StatusTooManyRequests},
		{"MAINTENANCE", http.StatusServiceUnavailable},
		{"SOME_UNKNOWN_CODE", http.StatusInternalServerError},
		{"", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			if got := mapAPIErrorCodeToHTTPStatus(tc.code); got != tc.want {
				t.Errorf("mapAPIErrorCodeToHTTPStatus(%q) = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}

func TestSendAPIResponseOK(t *testing.T) {
	rec := httptest.NewRecorder()
	sendAPIResponseOK(rec, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected default 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp.OK {
		t.Error("expected OK=true")
	}
	if resp.Error != "" {
		t.Errorf("expected empty Error, got %q", resp.Error)
	}
}

func TestSendAPIResponseError(t *testing.T) {
	rec := httptest.NewRecorder()
	sendAPIResponseError(rec, "NOT_FOUND", "template not found")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.OK {
		t.Error("expected OK=false")
	}
	if resp.Error != "NOT_FOUND" {
		t.Errorf("Error = %q, want NOT_FOUND", resp.Error)
	}
	if resp.Message != "template not found" {
		t.Errorf("Message = %q, want %q", resp.Message, "template not found")
	}
}

func TestSendAPIResponseErrorLocalizedFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// A code with no i18n key mapping falls back to the supplied message.
	sendAPIResponseErrorLocalized(rec, req, "TOKEN_EXPIRED", "custom fallback message")

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Message != "custom fallback message" {
		t.Errorf("Message = %q, want fallback %q", resp.Message, "custom fallback message")
	}
}

func TestSendAPIResponseErrorLocalizedTranslated(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	sendAPIResponseErrorLocalized(rec, req, "NOT_FOUND", "fallback")

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected a non-empty translated or fallback message")
	}
}

func TestSetCacheHeaders(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"static", "public, max-age=31536000, immutable"},
		{"api", "public, max-age=60"},
		{"authenticated", "private, no-store"},
		{"html", "no-store"},
		{"error", "no-store"},
		{"unknown-kind", "no-store"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			rec := httptest.NewRecorder()
			setCacheHeaders(rec, tc.kind)
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("setCacheHeaders(%q) Cache-Control = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}
