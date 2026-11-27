package cmd

import (
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	t.Run("unset defaults to bash", func(t *testing.T) {
		t.Setenv("SHELL", "")
		if got := DetectShell(); got != "bash" {
			t.Errorf("DetectShell() = %q, want bash", got)
		}
	})
	t.Run("extracts basename", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/bin/zsh")
		if got := DetectShell(); got != "zsh" {
			t.Errorf("DetectShell() = %q, want zsh", got)
		}
	})
}

func TestJoinWords(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"one"}, "one"},
		{[]string{"a", "b", "c"}, "a b c"},
	}
	for _, tc := range cases {
		if got := joinWords(tc.in); got != tc.want {
			t.Errorf("joinWords(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGenerateCompletionsContainAllCommandsAndBinaryName(t *testing.T) {
	generators := map[string]func(string) string{
		"bash":       generateBashCompletions,
		"zsh":        generateZshCompletions,
		"fish":       generateFishCompletions,
		"powershell": generatePowershellCompletions,
	}
	for name, gen := range generators {
		t.Run(name, func(t *testing.T) {
			out := gen("gitignore-cli")
			if !strings.Contains(out, "gitignore-cli") {
				t.Errorf("%s completions missing binary name:\n%s", name, out)
			}
			for _, cmd := range commandWords {
				if !strings.Contains(out, cmd) {
					t.Errorf("%s completions missing command %q:\n%s", name, cmd, out)
				}
			}
		})
	}
	if !strings.Contains(generateBashCompletions("gitignore-cli"), "--update") || !strings.Contains(generateFishCompletions("gitignore-cli"), "-l update") {
		t.Fatal("client update flag is missing from Bash or Fish completions")
	}
}

func TestHandleShellNoArgsReturnsUsage(t *testing.T) {
	if got := HandleShell(nil); got != 2 {
		t.Errorf("HandleShell(nil) = %d, want 2", got)
	}
}

func TestHandleShellUnknownSubcommand(t *testing.T) {
	if got := HandleShell([]string{"bogus"}); got != 2 {
		t.Errorf("HandleShell(bogus) = %d, want 2", got)
	}
}

func TestHandleShellHelp(t *testing.T) {
	if got := HandleShell([]string{"help"}); got != 0 {
		t.Errorf("HandleShell(help) = %d, want 0", got)
	}
}

func TestPrintCompletionsKnownAndUnknownShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell", "pwsh"} {
		if got := printCompletions(shell, "gitignore-cli"); got != 0 {
			t.Errorf("printCompletions(%s) = %d, want 0", shell, got)
		}
	}
	for _, shell := range []string{"sh", "dash", "ksh"} {
		if got := printCompletions(shell, "gitignore-cli"); got != 2 {
			t.Errorf("printCompletions(%s) = %d, want 2", shell, got)
		}
	}
	if got := printCompletions("cobol", "gitignore-cli"); got != 2 {
		t.Errorf("printCompletions(cobol) = %d, want 2", got)
	}
}

func TestPrintInitKnownAndUnknownShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "sh", "dash", "ksh", "powershell", "pwsh"} {
		if got := printInit(shell, "gitignore-cli"); got != 0 {
			t.Errorf("printInit(%s) = %d, want 0", shell, got)
		}
	}
	if got := printInit("cobol", "gitignore-cli"); got != 2 {
		t.Errorf("printInit(cobol) = %d, want 2", got)
	}
}

func TestHandleShellCompletionsAndInitDispatch(t *testing.T) {
	if got := HandleShell([]string{"completions", "bash"}); got != 0 {
		t.Errorf("HandleShell(completions bash) = %d, want 0", got)
	}
	if got := HandleShell([]string{"init", "zsh"}); got != 0 {
		t.Errorf("HandleShell(init zsh) = %d, want 0", got)
	}
}
