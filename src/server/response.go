package server

import (
	"encoding/json"
	"net/http"

	"github.com/apimgr/gitignore/src/common/i18n"
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
	"VALIDATION_FAILED":  http.StatusBadRequest,
	"UNAUTHORIZED":       http.StatusUnauthorized,
	"TOKEN_EXPIRED":      http.StatusUnauthorized,
	"TOKEN_INVALID":      http.StatusUnauthorized,
	"FORBIDDEN":          http.StatusForbidden,
	"ACCOUNT_LOCKED":     http.StatusForbidden,
	"CSRF_FAILED":        http.StatusForbidden,
	"NOT_FOUND":          http.StatusNotFound,
	"METHOD_NOT_ALLOWED": http.StatusMethodNotAllowed,
	"CONFLICT":           http.StatusConflict,
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
	setCacheHeaders(w, "error")
	w.WriteHeader(mapAPIErrorCodeToHTTPStatus(code))
	_ = json.NewEncoder(w).Encode(APIResponse{OK: false, Error: code, Message: message})
}

// apiErrorI18nKey maps a stable API error code to its translation key so error
// messages can be localized to the request language (AI.md PART 30).
var apiErrorI18nKey = map[string]string{
	"BAD_REQUEST":        "errors.bad_request",
	"VALIDATION_FAILED":  "errors.validation_failed",
	"FORBIDDEN":          "errors.forbidden",
	"ACCOUNT_LOCKED":     "errors.account_locked",
	"NOT_FOUND":          "errors.not_found",
	"METHOD_NOT_ALLOWED": "errors.method_not_allowed",
	"CONFLICT":           "errors.conflict",
	"RATE_LIMITED":       "errors.rate_limited",
	"SERVER_ERROR":       "errors.server_error",
	"MAINTENANCE":        "errors.maintenance_mode",
}

// sendAPIResponseErrorLocalized writes an error envelope whose Message is
// translated to the request language when a translation key exists for the
// code; otherwise it falls back to the supplied message (AI.md PART 30).
func sendAPIResponseErrorLocalized(w http.ResponseWriter, r *http.Request, code, fallback string) {
	message := fallback
	if key, ok := apiErrorI18nKey[code]; ok {
		message = i18n.T(r, key)
	}
	sendAPIResponseError(w, code, message)
}

// setCacheHeaders applies the spec's Cache-Control policy per response class
// (AI.md PART 9 HTTP Cache Headers): static assets are immutable and long-lived,
// API responses are briefly cacheable, HTML and error pages are never stored,
// and authenticated responses are private and never stored.
func setCacheHeaders(w http.ResponseWriter, kind string) {
	switch kind {
	case "static":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case "api":
		w.Header().Set("Cache-Control", "public, max-age=60")
	case "authenticated":
		w.Header().Set("Cache-Control", "private, no-store")
	case "html", "error":
		w.Header().Set("Cache-Control", "no-store")
	default:
		w.Header().Set("Cache-Control", "no-store")
	}
}
