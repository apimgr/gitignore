// Package httputil provides HTTP client-type detection and content-negotiation
// helpers shared across the server (AI.md PART 14 "Content Negotiation").
//
// Detection is driven entirely by the request's User-Agent and Accept headers.
// Three client classes are distinguished:
//
//   - our client   ({project_name}-cli) — INTERACTIVE, receives JSON
//   - text browser (lynx, w3m, links)   — INTERACTIVE, receives no-JS HTML
//   - HTTP tool    (curl, wget, httpie) — NON-INTERACTIVE, receives plain text
package httputil

import (
	"net/http"
	"strings"
)

// projectName is the binary/client name used to detect our own CLI client via
// its User-Agent prefix "{project_name}-cli/".
const projectName = "gitignore"

// IsOurCliClient reports whether the request comes from our own client binary
// ({project_name}-cli). Our client is INTERACTIVE and renders its own TUI/GUI,
// so it receives JSON rather than pre-formatted text.
func IsOurCliClient(r *http.Request) bool {
	ua := r.Header.Get("User-Agent")
	return strings.HasPrefix(ua, projectName+"-cli/")
}

// IsTextBrowser reports whether the request comes from a text-mode browser
// (lynx, w3m, links, elinks, browsh, carbonyl, netsurf). Text browsers are
// INTERACTIVE but do not support JavaScript, so they receive a no-JS HTML
// alternative rather than pre-formatted text.
func IsTextBrowser(r *http.Request) bool {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	textBrowsers := []string{
		"lynx/",
		"w3m/",
		"links ",
		"links/",
		"elinks/",
		"browsh/",
		"carbonyl/",
		"netsurf",
	}
	for _, browser := range textBrowsers {
		if strings.Contains(ua, browser) {
			return true
		}
	}
	return false
}

// IsHttpTool reports whether the request comes from a non-interactive HTTP tool
// (curl, wget, httpie, and library user-agents). An empty User-Agent is treated
// as an HTTP tool because interactive browsers always send one.
func IsHttpTool(r *http.Request) bool {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	httpTools := []string{
		"curl/", "wget/", "httpie/",
		"libcurl/", "python-requests/",
		"go-http-client/", "axios/", "node-fetch/",
	}
	for _, tool := range httpTools {
		if strings.Contains(ua, tool) {
			return true
		}
	}
	if ua == "" {
		return true
	}
	return false
}

// IsNonInteractiveClient reports whether the client needs pre-formatted text
// output. Only HTTP tools are non-interactive; our client and text browsers are
// interactive and handle their own rendering.
func IsNonInteractiveClient(r *http.Request) bool {
	if IsOurCliClient(r) {
		return false
	}
	if IsTextBrowser(r) {
		return false
	}
	if IsHttpTool(r) {
		return true
	}
	return false
}

// GetAPIResponseFormat determines the response format for /api/** routes.
// API routes return raw data as plain text (no HTML conversion) and default to
// JSON. Priority: .txt extension, then Accept: text/plain, then non-interactive
// client detection, then JSON.
func GetAPIResponseFormat(r *http.Request) string {
	if strings.HasSuffix(r.URL.Path, ".txt") {
		return "text"
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/plain") {
		return "text"
	}
	if IsNonInteractiveClient(r) {
		return "text"
	}
	return "json"
}

// DetectResponseFormat returns the negotiated content type ("text/html",
// "text/plain", or "application/json") for a request. API routes (/api/) follow
// the API priority order; frontend routes honor an explicit Accept header first,
// then fall back to User-Agent smart detection, then default to HTML.
func DetectResponseFormat(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if GetAPIResponseFormat(r) == "text" {
			return "text/plain"
		}
		return "application/json"
	}

	accept := r.Header.Get("Accept")
	switch {
	case strings.Contains(accept, "text/html"):
		return "text/html"
	case strings.Contains(accept, "text/plain"):
		return "text/plain"
	}

	if IsNonInteractiveClient(r) {
		return "text/plain"
	}

	return "text/html"
}
