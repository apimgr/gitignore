package server

import (
	"encoding/json"
	"net/http"
	"strings"

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
	Details interface{} `json:"details,omitempty"`
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
	_ = encodeJSON(w, APIResponse{OK: true, Data: data})
}

// sendAPIResponseError writes an error envelope, deriving the HTTP status from
// the stable error code.
func sendAPIResponseError(w http.ResponseWriter, code, message string) {
	sendAPIResponseErrorDetails(w, code, message, map[string]interface{}{})
}

func sendAPIResponseErrorDetails(w http.ResponseWriter, code, message string, details interface{}) {
	w.Header().Set("Content-Type", "application/json")
	setCacheHeaders(w, "error")
	w.WriteHeader(mapAPIErrorCodeToHTTPStatus(code))
	_ = encodeJSON(w, APIResponse{OK: false, Error: code, Message: message, Details: details})
}

func encodeJSON(w http.ResponseWriter, value interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// apiErrorI18nKey maps a stable API error code to its translation key so error
// messages can be localized to the request language (AI.md PART 30).
var apiErrorI18nKey = map[string]string{
	"BAD_REQUEST":        "errors.bad_request",
	"VALIDATION_FAILED":  "errors.validation_error",
	"UNAUTHORIZED":       "errors.unauthorized",
	"TOKEN_EXPIRED":      "errors.token_expired",
	"TOKEN_INVALID":      "errors.token_invalid",
	"FORBIDDEN":          "errors.forbidden",
	"ACCOUNT_LOCKED":     "errors.account_locked",
	"CSRF_FAILED":        "errors.csrf_failed",
	"NOT_FOUND":          "errors.not_found",
	"METHOD_NOT_ALLOWED": "errors.method_not_allowed",
	"CONFLICT":           "errors.conflict",
	"RATE_LIMITED":       "errors.rate_limited",
	"SERVER_ERROR":       "errors.server_error",
	"NOT_IMPLEMENTED":    "errors.not_implemented",
	"MAINTENANCE":        "errors.maintenance_mode",
	"scheduler_error":    "errors.scheduler_error",
}

// sendAPIResponseErrorLocalized writes an error envelope with a request-language
// message derived from the stable error code (AI.md PART 30).
func sendAPIResponseErrorLocalized(w http.ResponseWriter, r *http.Request, code string) {
	key, ok := apiErrorI18nKey[code]
	if !ok {
		key = "errors.server_error"
	}
	sendAPIResponseError(w, code, i18n.T(r, key))
}

// setCacheHeaders applies the spec's Cache-Control policy per response class
// (AI.md PART 9 HTTP Cache Headers): static assets are immutable and long-lived,
// API responses are briefly cacheable, HTML and error pages are never stored,
// and authenticated responses are private and never stored.
func setCacheHeaders(w http.ResponseWriter, kind string) {
	switch kind {
	case "static":
		w.Header().Set("Cache-Control", "no-cache")
	case "api":
		w.Header().Set("Cache-Control", "public, max-age=60")
	case "authenticated":
		w.Header().Set("Cache-Control", "private, no-store")
	case "html", "error":
		w.Header().Set("Cache-Control", "no-store")
	case "sw":
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Cache-Control", "no-store")
	}
}

func setVersionedCacheHeaders(w http.ResponseWriter, r *http.Request, stamp string) {
	w.Header().Set("ETag", `"`+stamp+`"`)
	if r != nil && r.URL.Query().Get("v") == stamp {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

func setRevalidatedCacheHeaders(w http.ResponseWriter, stamp string) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", `"`+stamp+`"`)
}

func versionPurge(w http.ResponseWriter, r *http.Request, stamp string, status int) {
	if cookie, err := r.Cookie("gitignore_build"); err == nil && cookie.Value != stamp && status >= http.StatusOK && status < http.StatusMultipleChoices && isTopLevelDocument(r) {
		w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "gitignore_build", Value: stamp, Path: "/", MaxAge: 31536000,
		Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func isTopLevelDocument(r *http.Request) bool {
	if r == nil || r.Method == http.MethodHead {
		return false
	}
	destination := strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest"))
	mode := strings.TrimSpace(r.Header.Get("Sec-Fetch-Mode"))
	return destination == "" && mode == "" || destination == "document" && mode == "navigate"
}
