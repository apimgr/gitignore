package server

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the unified JSON envelope for all versioned API responses
// (AI.md PART 9 / PART 14). Success responses set OK=true and populate Data;
// error responses set OK=false and populate Error (a stable machine code) and
// Message (a human-readable string).
type APIResponse struct {
	OK      bool        `json:"ok"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
}

// apiErrorStatus maps a stable API error code to its HTTP status (AI.md PART 9).
var apiErrorStatus = map[string]int{
	"BAD_REQUEST":        http.StatusBadRequest,
	"UNAUTHORIZED":       http.StatusUnauthorized,
	"FORBIDDEN":          http.StatusForbidden,
	"NOT_FOUND":          http.StatusNotFound,
	"METHOD_NOT_ALLOWED": http.StatusMethodNotAllowed,
	"CONFLICT":           http.StatusConflict,
	"VALIDATION":         http.StatusUnprocessableEntity,
	"RATE_LIMITED":       http.StatusTooManyRequests,
	"SERVER_ERROR":       http.StatusInternalServerError,
	"NOT_IMPLEMENTED":    http.StatusNotImplemented,
	"MAINTENANCE":        http.StatusServiceUnavailable,
}

// mapAPIErrorCodeToHTTPStatus resolves a stable error code to an HTTP status,
// defaulting to 500 for unknown codes.
func mapAPIErrorCodeToHTTPStatus(code string) int {
	if s, ok := apiErrorStatus[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// sendAPIResponseOK writes a success envelope with the given data.
func sendAPIResponseOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	setCacheHeaders(w, "api")
	_ = json.NewEncoder(w).Encode(APIResponse{OK: true, Data: data})
}

// sendAPIResponseError writes an error envelope, deriving the HTTP status from
// the stable error code.
func sendAPIResponseError(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(mapAPIErrorCodeToHTTPStatus(code))
	_ = json.NewEncoder(w).Encode(APIResponse{OK: false, Error: code, Message: message})
}

// setCacheHeaders applies the spec's Cache-Control policy per response class
// (AI.md PART 9): static assets are immutable and long-lived, API responses are
// briefly cacheable, HTML is never stored, and authenticated responses are
// private and never stored.
func setCacheHeaders(w http.ResponseWriter, kind string) {
	switch kind {
	case "static":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case "api":
		w.Header().Set("Cache-Control", "public, max-age=60")
	case "authenticated":
		w.Header().Set("Cache-Control", "private, no-store")
	default:
		w.Header().Set("Cache-Control", "no-store")
	}
}
