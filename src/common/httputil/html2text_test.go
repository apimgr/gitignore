package httputil

import (
	"strings"
	"testing"
)

func TestHTML2TextConverter(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		width      int
		wantSubstr []string
		notSubstr  []string
	}{
		{
			name:       "heading and paragraph",
			source:     "<html><body><h1>Title</h1><p>Hello world</p></body></html>",
			width:      80,
			wantSubstr: []string{"TITLE", "Hello world"},
		},
		{
			name:       "unordered list",
			source:     "<ul><li>one</li><li>two</li></ul>",
			width:      80,
			wantSubstr: []string{"• one", "• two"},
		},
		{
			name:       "ordered list numbering",
			source:     "<ol><li>first</li><li>second</li></ol>",
			width:      80,
			wantSubstr: []string{"1. first", "2. second"},
		},
		{
			name:       "link renders href",
			source:     `<a href="https://example.com">example</a>`,
			width:      80,
			wantSubstr: []string{"example [https://example.com]"},
		},
		{
			name:       "strong and em markers",
			source:     "<strong>bold</strong> <em>italic</em>",
			width:      80,
			wantSubstr: []string{"*bold*", "_italic_"},
		},
		{
			name:       "script and style skipped",
			source:     "<script>alert(1)</script><style>body{}</style><p>visible</p>",
			width:      80,
			wantSubstr: []string{"visible"},
			notSubstr:  []string{"alert(1)", "body{}"},
		},
		{
			name:       "form controls skipped",
			source:     `<form><input type="text"><button>Go</button></form><p>content</p>`,
			width:      80,
			wantSubstr: []string{"content"},
			notSubstr:  []string{"Go"},
		},
		{
			name:       "table renders grid",
			source:     "<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>",
			width:      80,
			wantSubstr: []string{"A", "B", "1", "2", "│"},
		},
		{
			name:       "zero width falls back to 80",
			source:     "<p>hello</p>",
			width:      0,
			wantSubstr: []string{"hello"},
		},
		{
			name:       "malformed html falls back to strip tags",
			source:     "<<<not really html>>>plain text",
			width:      80,
			wantSubstr: []string{"plain text"},
		},
		{
			name:       "empty input",
			source:     "",
			width:      80,
			wantSubstr: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HTML2TextConverter(tc.source, tc.width)
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("output does not end with newline: %q", got)
			}
			for _, want := range tc.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("output %q does not contain %q", got, want)
				}
			}
			for _, notWant := range tc.notSubstr {
				if strings.Contains(got, notWant) {
					t.Errorf("output %q unexpectedly contains %q", got, notWant)
				}
			}
		})
	}
}

func TestWordWrap(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{"empty text", "", 10, ""},
		{"zero width returns unwrapped", "hello world", 0, "hello world"},
		{"single short word", "hi", 10, "hi"},
		{"wraps at boundary", "one two three", 7, "one two\nthree"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wordWrap(tc.text, tc.width); got != tc.want {
				t.Errorf("wordWrap(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

func TestCenterText(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{"shorter than width", "hi", 6, "  hi"},
		{"equal to width", "hello", 5, "hello"},
		{"longer than width unchanged", "hello world", 5, "hello world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := centerText(tc.text, tc.width); got != tc.want {
				t.Errorf("centerText(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

func TestCollapseSpaces(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a   b\n\tc", "a b c"},
		{"", ""},
		{"single", "single"},
	}
	for _, tc := range cases {
		if got := collapseSpaces(tc.in); got != tc.want {
			t.Errorf("collapseSpaces(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<p>hello</p>", "hello"},
		{"no tags here", "no tags here"},
		{"<b>a</b> <i>b</i>", "a b"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := stripTags(tc.in); got != tc.want {
			t.Errorf("stripTags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
