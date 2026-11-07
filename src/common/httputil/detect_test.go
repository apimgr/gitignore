package httputil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newReq builds a GET request with the given path, User-Agent, and Accept
// header for exercising the detection helpers below.
func newReq(t *testing.T, path, ua, accept string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if ua != "" {
		r.Header.Set("User-Agent", ua)
	}
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	return r
}

func TestIsOurCliClient(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		{"our client", "gitignore-cli/1.0.0", true},
		{"our client no version", "gitignore-cli/", true},
		{"empty", "", false},
		{"curl", "curl/8.0.0", false},
		{"prefix mismatch", "notgitignore-cli/1.0.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq(t, "/", tc.ua, "")
			if got := IsOurCliClient(r); got != tc.want {
				t.Errorf("IsOurCliClient(%q) = %v, want %v", tc.ua, got, tc.want)
			}
		})
	}
}

func TestIsTextBrowser(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		{"lynx", "Lynx/2.9.0", true},
		{"w3m", "w3m/0.5.3", true},
		{"links space", "Links (2.28; Linux)", true},
		{"links slash", "links/2.28", true},
		{"elinks", "ELinks/0.16", true},
		{"browsh", "browsh/1.6.5", true},
		{"carbonyl", "carbonyl/0.0.1", true},
		{"netsurf", "NetSurf/3.10", true},
		{"chrome", "Mozilla/5.0 Chrome/100", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq(t, "/", tc.ua, "")
			if got := IsTextBrowser(r); got != tc.want {
				t.Errorf("IsTextBrowser(%q) = %v, want %v", tc.ua, got, tc.want)
			}
		})
	}
}

func TestIsHttpTool(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		{"curl", "curl/8.0.0", true},
		{"wget", "Wget/1.21", true},
		{"httpie", "HTTPie/3.2.1", true},
		{"libcurl", "libcurl/7.68.0", true},
		{"python-requests", "python-requests/2.28.0", true},
		{"go-http-client", "Go-http-client/1.1", true},
		{"axios", "axios/1.4.0", true},
		{"node-fetch", "node-fetch/3.3.0", true},
		{"empty ua treated as tool", "", true},
		{"browser", "Mozilla/5.0 (Windows NT 10.0) Chrome/100 Safari/537.36", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq(t, "/", tc.ua, "")
			if got := IsHttpTool(r); got != tc.want {
				t.Errorf("IsHttpTool(%q) = %v, want %v", tc.ua, got, tc.want)
			}
		})
	}
}

func TestIsNonInteractiveClient(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		{"our cli wins over tool-shaped ua", "gitignore-cli/1.0.0", false},
		{"text browser interactive", "Lynx/2.9.0", false},
		{"http tool non-interactive", "curl/8.0.0", true},
		{"empty ua is non-interactive", "", true},
		{"regular browser interactive", "Mozilla/5.0 Chrome/100", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq(t, "/", tc.ua, "")
			if got := IsNonInteractiveClient(r); got != tc.want {
				t.Errorf("IsNonInteractiveClient(%q) = %v, want %v", tc.ua, got, tc.want)
			}
		})
	}
}

func TestGetAPIResponseFormat(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		ua     string
		accept string
		want   string
	}{
		{"txt extension wins", "/api/templates/go.txt", "gitignore-cli/1.0.0", "application/json", "text"},
		{"accept text/plain", "/api/templates/go", "", "text/plain", "text"},
		{"curl non-interactive", "/api/templates/go", "curl/8.0.0", "", "text"},
		{"default json", "/api/templates/go", "Mozilla/5.0 Chrome/100", "", "json"},
		{"our cli json", "/api/templates/go", "gitignore-cli/1.0.0", "", "json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq(t, tc.path, tc.ua, tc.accept)
			if got := GetAPIResponseFormat(r); got != tc.want {
				t.Errorf("GetAPIResponseFormat() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectResponseFormat(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		ua     string
		accept string
		want   string
	}{
		{"api text via txt suffix", "/api/templates/go.txt", "", "", "text/plain"},
		{"api json default", "/api/templates/go", "Mozilla/5.0", "", "application/json"},
		{"api text via curl", "/api/templates/go", "curl/8.0.0", "", "text/plain"},
		{"frontend accept html", "/templates/go", "curl/8.0.0", "text/html", "text/html"},
		{"frontend accept plain", "/templates/go", "Mozilla/5.0", "text/plain", "text/plain"},
		{"frontend curl no accept", "/templates/go", "curl/8.0.0", "", "text/plain"},
		{"frontend browser default html", "/templates/go", "Mozilla/5.0 Chrome/100", "", "text/html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq(t, tc.path, tc.ua, tc.accept)
			if got := DetectResponseFormat(r); got != tc.want {
				t.Errorf("DetectResponseFormat() = %q, want %q", got, tc.want)
			}
		})
	}
}
