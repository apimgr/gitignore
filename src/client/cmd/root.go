// Package cmd implements gitignore-cli's command dispatch, help/version
// text, and interactive mode. Flag parsing lives in src/client/main.go
// (mirroring src/main.go's stdlib flag.Parse style); this package is
// invoked once flags are resolved.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/output"
	"github.com/apimgr/gitignore/src/common/i18n"
)

// BinaryName is set once at startup to filepath.Base(os.Args[0]) so help/
// version/error text always shows the actual invoked name, even if the
// binary has been renamed (see AI.md PART 32 "Binary Naming Rules").
var BinaryName = "gitignore-cli"

var language = i18n.DefaultLang

func SetLanguage(lang string) {
	language = lang
}

func text(key string, values ...interface{}) string {
	return i18n.TranslateFormat(language, key, values...)
}

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
			p.ErrorText(text("cli.client.command.category_name_required"))
			return output.ExitUsage
		}
		return CmdCategory(c, p, format, rest[0])
	case "stats":
		return CmdStats(c, p, format)
	case "get", "template":
		if len(rest) == 0 {
			p.ErrorText(text("cli.client.command.template_name_required", "command", first))
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
	fmt.Println(p.Bold(text("cli.client.interactive.title", "binary", BinaryName)))
	fmt.Println(text("cli.client.interactive.prompt"))
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
	fmt.Println(text("cli.client.interactive.help"))
}

// PrintHelp prints --help output using the actual invoked binary name.
func PrintHelp(version string) {
	fmt.Println(text("cli.client.help", "binary", BinaryName, "version", version))
}

// PrintVersion prints --version output. Extended server info is appended
// when reachable (best-effort; a failed probe is silent).
func PrintVersion(version, commit, buildDate string, c *api.Client) {
	fmt.Println(text("cli.client.version_line", "binary", BinaryName, "version", version, "commit", commit))
	fmt.Println(text("cli.client.built_line", "build_date", buildDate))
	fmt.Println(text("cli.client.go_line", "go_version", runtime.Version()))
	fmt.Println(text("cli.client.os_arch_line", "goos", runtime.GOOS, "goarch", runtime.GOARCH))
	if c == nil {
		return
	}
	if err := c.Healthz(); err == nil {
		fmt.Printf("\n%s\n", text("cli.client.server_label", "server", c.BaseURL))
	}
}
