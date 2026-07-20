// Package cmd implements gitignore-cli's command dispatch, help/version
// text, and interactive mode. Flag parsing lives in src/client/main.go
// (mirroring src/main.go's stdlib flag.Parse style); this package is
// invoked once flags are resolved.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/output"
)

// BinaryName is set once at startup to filepath.Base(os.Args[0]) so help/
// version/error text always shows the actual invoked name, even if the
// binary has been renamed (see AI.md PART 32 "Binary Naming Rules").
var BinaryName = "gitignore-cli"

// knownCommands maps a leading positional argument to a command handler
// name. Anything that doesn't match one of these is treated as a bare
// template-name list for the combine smart-detection path.
var knownCommands = map[string]bool{
	"list": true, "search": true, "categories": true, "category": true,
	"stats": true, "get": true, "template": true, "combine": true, "help": true,
}

// Dispatch routes positional args (post-flag-parsing) to the matching
// command, or — if the first arg isn't a known command word — treats all
// args as template names for combine (IDEA.md: "gitignore-cli Go Node >
// .gitignore").
func Dispatch(c *api.Client, p *output.Printer, format string, args []string) int {
	if len(args) == 0 {
		return Interactive(c, p, format)
	}

	first := strings.ToLower(args[0])
	rest := args[1:]

	if !knownCommands[first] {
		return CmdCombine(c, p, format, args)
	}

	switch first {
	case "list":
		return CmdList(c, p, format)
	case "search":
		return CmdSearch(c, p, format, strings.Join(rest, " "))
	case "categories":
		return CmdCategories(c, p, format)
	case "category":
		if len(rest) == 0 {
			p.Error("category requires a name")
			return output.ExitUsage
		}
		return CmdCategory(c, p, format, rest[0])
	case "stats":
		return CmdStats(c, p, format)
	case "get", "template":
		if len(rest) == 0 {
			p.Error("%s requires a template name", first)
			return output.ExitUsage
		}
		return CmdGetTemplate(c, p, format, rest[0])
	case "combine":
		return CmdCombine(c, p, format, rest)
	case "help":
		PrintHelp("dev")
		return output.ExitSuccess
	}

	// Unreachable: every knownCommands key is handled above.
	return output.ExitUsage
}

// Interactive is a plain-text fallback menu loop over stdin/stdout. The
// real default interactive experience is the bubbletea TUI in
// src/client/tui, launched by main.go whenever stdout is an interactive
// terminal and no command args were given (AI.md PART 32 "Automatic Mode
// Detection"). Interactive exists only as a last-resort fallback for
// callers that invoke Dispatch directly with no args outside that
// TUI-eligible path.
func Interactive(c *api.Client, p *output.Printer, format string) int {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println(p.Bold(fmt.Sprintf("%s — interactive mode", BinaryName)))
	fmt.Println("Type a command, or 'help' / 'quit'.")
	fmt.Println()

	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return output.ExitSuccess
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "quit", "exit", "q":
			return output.ExitSuccess
		case "help", "?":
			printInteractiveHelp()
			continue
		}
		args := strings.Fields(line)
		Dispatch(c, p, format, args)
		fmt.Println()
	}
}

func printInteractiveHelp() {
	fmt.Println("Commands:")
	fmt.Println("  list                 List all template names")
	fmt.Println("  search TERM          Search templates")
	fmt.Println("  categories           List categories")
	fmt.Println("  category NAME        List templates in a category")
	fmt.Println("  get NAME             Print a single template")
	fmt.Println("  combine NAME NAME.. Merge templates (or just: NAME NAME..)")
	fmt.Println("  stats                Show server template statistics")
	fmt.Println("  quit                 Exit interactive mode")
}

// PrintHelp prints --help output using the actual invoked binary name.
func PrintHelp(version string) {
	fmt.Printf("%s %s - CLI for gitignore\n\n", BinaryName, version)
	fmt.Println("Usage:")
	fmt.Printf("  %s [command] [args] [flags]\n", BinaryName)
	fmt.Println("  # interactive mode (no args)")
	fmt.Printf("  %s\n\n", BinaryName)
	fmt.Println("Commands:")
	fmt.Println("  list                          List all template names")
	fmt.Println("  search QUERY                  Search templates by name/tag")
	fmt.Println("  categories                    List categories")
	fmt.Println("  category NAME                 List templates in a category")
	fmt.Println("  get NAME | template NAME       Print a single template")
	fmt.Println("  combine NAME...                Merge templates (default when args are bare names)")
	fmt.Println("  stats                         Show server template statistics")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("-h, --help                             - Show help")
	fmt.Println("-v, --version                           - Show version")
	fmt.Println("--shell completions [SHELL]            - Print shell completions (auto-detect if SHELL omitted)")
	fmt.Println("--shell init [SHELL]                   - Print shell init command (auto-detect if SHELL omitted)")
	fmt.Println("--shell help                           - Show shell integration help")
	fmt.Println()
	fmt.Println("--server URL                           - Server URL (default: from cli.yml)")
	fmt.Println("--config NAME                          - Config profile name (default: cli)")
	fmt.Println("--output {text|json|table}              - Output format (default: text)")
	fmt.Println("--debug                                - Debug output")
	fmt.Println("--color {auto|yes|no}                  - Color output (default: auto)")
	fmt.Println("--lang CODE                            - Language for output (default: auto)")
	fmt.Println()
	fmt.Println("Shells: bash, zsh, fish, sh, dash, ksh, powershell, pwsh")
	fmt.Println()
	fmt.Println("Run without arguments for interactive mode.")
	fmt.Println("Examples:")
	fmt.Printf("  %s Go Node > .gitignore\n", BinaryName)
	fmt.Printf("  %s search python\n", BinaryName)
	fmt.Printf("  %s get Go --output json\n", BinaryName)
}

// PrintVersion prints --version output. Extended server info is appended
// when reachable (best-effort; a failed probe is silent).
func PrintVersion(version, commit, buildDate string, c *api.Client) {
	fmt.Printf("%s %s (%s) built %s\n", BinaryName, version, commit, buildDate)
	if c == nil {
		return
	}
	if err := c.Healthz(); err == nil {
		fmt.Printf("\nServer: %s\n", c.BaseURL)
	}
}
