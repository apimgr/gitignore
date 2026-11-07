package output

import (
	"strings"
	"testing"
)

func TestResolveColor(t *testing.T) {
	t.Run("explicit yes", func(t *testing.T) {
		if !ResolveColor("yes") {
			t.Error("ResolveColor(yes) = false, want true")
		}
	})
	t.Run("explicit no", func(t *testing.T) {
		if ResolveColor("no") {
			t.Error("ResolveColor(no) = true, want false")
		}
	})
	t.Run("NO_COLOR set overrides auto", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if ResolveColor("auto") {
			t.Error("ResolveColor(auto) with NO_COLOR set = true, want false")
		}
	})
	t.Run("case insensitive and trimmed", func(t *testing.T) {
		if !ResolveColor("  YES  ") {
			t.Error("ResolveColor('  YES  ') = false, want true")
		}
	})
}

func TestPrinterColorize(t *testing.T) {
	t.Run("color enabled wraps in ANSI codes", func(t *testing.T) {
		p := New(true)
		got := p.Green("ok")
		if !strings.HasPrefix(got, "\x1b[32m") || !strings.HasSuffix(got, "\x1b[0m") {
			t.Errorf("Green(ok) = %q, want ANSI-wrapped", got)
		}
		if !strings.Contains(got, "ok") {
			t.Errorf("Green(ok) = %q, missing original text", got)
		}
	})

	t.Run("color disabled returns plain text", func(t *testing.T) {
		p := New(false)
		if got := p.Red("fail"); got != "fail" {
			t.Errorf("Red(fail) with color disabled = %q, want plain %q", got, "fail")
		}
		if got := p.Bold("b"); got != "b" {
			t.Errorf("Bold(b) with color disabled = %q, want plain %q", got, "b")
		}
		if got := p.Yellow("y"); got != "y" {
			t.Errorf("Yellow(y) with color disabled = %q, want plain %q", got, "y")
		}
		if got := p.Cyan("c"); got != "c" {
			t.Errorf("Cyan(c) with color disabled = %q, want plain %q", got, "c")
		}
	})
}

func TestFormatTable(t *testing.T) {
	out := FormatTable([]string{"Name", "Size"}, [][]string{
		{"go.gitignore", "12"},
		{"node", "3"},
	})

	for _, want := range []string{"Name", "Size", "go.gitignore", "node", "┌", "┐", "└", "┘", "├", "┤"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTable output missing %q:\n%s", want, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Errorf("expected 6 lines (top, header, sep, 2 data rows, bottom), got %d:\n%s", len(lines), out)
	}
}

func TestFormatTableEmptyRows(t *testing.T) {
	out := FormatTable([]string{"A"}, nil)
	if !strings.Contains(out, "A") {
		t.Errorf("expected header even with no rows:\n%s", out)
	}
}

func TestFormatTableRowShorterThanHeaders(t *testing.T) {
	// A row with fewer cells than headers must not panic and should render
	// the missing cell as blank.
	out := FormatTable([]string{"A", "B"}, [][]string{{"only-a"}})
	if !strings.Contains(out, "only-a") {
		t.Errorf("expected only-a in output:\n%s", out)
	}
}
