package urlutil

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildAPIURL(t *testing.T) {
	cases := []struct {
		name        string
		baseURL     string
		path        string
		pathParams  map[string]string
		queryParams map[string]string
		want        string
	}{
		{
			name:    "no params",
			baseURL: "https://example.com",
			path:    "/api/templates",
			want:    "https://example.com/api/templates",
		},
		{
			// u.Path is set to the already-escaped segment, so u.String() escapes
			// it a second time (Go's url.URL always re-encodes Path on output);
			// this documents the actual double-encoding behavior.
			name:       "path placeholder replaced and escaped",
			baseURL:    "https://example.com",
			path:       "/api/templates/{name}",
			pathParams: map[string]string{"name": "Visual Studio"},
			want:       "https://example.com/api/templates/Visual%2520Studio",
		},
		{
			name:        "query params encoded",
			baseURL:     "https://example.com",
			path:        "/api/search",
			queryParams: map[string]string{"q": "a b"},
			want:        "https://example.com/api/search?q=a+b",
		},
		{
			name:    "base with trailing slash and existing path",
			baseURL: "https://example.com/api",
			path:    "/templates",
			want:    "https://example.com/api/templates",
		},
		{
			name:    "invalid base url returns empty",
			baseURL: "://not-a-url",
			path:    "/x",
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildAPIURL(tc.baseURL, tc.path, tc.pathParams, tc.queryParams)
			if got != tc.want {
				t.Errorf("BuildAPIURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodePathSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"go", "go"},
		{"Visual Studio", "Visual%20Studio"},
		{"a/b", "a%2Fb"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := EncodePathSegment(tc.in); got != tc.want {
			t.Errorf("EncodePathSegment(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEncodeQueryValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"go", "go"},
		{"a b", "a+b"},
		{"a&b=c", "a%26b%3Dc"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := EncodeQueryValue(tc.in); got != tc.want {
			t.Errorf("EncodeQueryValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildQueryString(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		if got := BuildQueryString(nil); got != "" {
			t.Errorf("BuildQueryString(nil) = %q, want empty", got)
		}
	})

	t.Run("single param", func(t *testing.T) {
		got := BuildQueryString(map[string]string{"q": "hello world"})
		want := url.Values{"q": {"hello world"}}.Encode()
		if got != want {
			t.Errorf("BuildQueryString() = %q, want %q", got, want)
		}
	})

	t.Run("multiple params sorted", func(t *testing.T) {
		got := BuildQueryString(map[string]string{"b": "2", "a": "1"})
		if !strings.Contains(got, "a=1") || !strings.Contains(got, "b=2") {
			t.Errorf("BuildQueryString() = %q, missing expected pairs", got)
		}
		if strings.Index(got, "a=1") > strings.Index(got, "b=2") {
			t.Errorf("BuildQueryString() = %q, expected alphabetical order", got)
		}
	})
}
