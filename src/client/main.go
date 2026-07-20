// Command gitignore-cli is the CLI client for the gitignore template server.
// It talks to the server's public, unauthenticated /api/v1/* endpoints
// (see IDEA.md: "No user accounts, registration, or login of any kind") to
// list/search/combine .gitignore templates from the shell, e.g.:
//
//	gitignore-cli Go Node > .gitignore
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/cmd"
	"github.com/apimgr/gitignore/src/client/config"
	"github.com/apimgr/gitignore/src/client/output"
	clipath "github.com/apimgr/gitignore/src/client/path"
	"github.com/apimgr/gitignore/src/client/tui"
	"github.com/apimgr/gitignore/src/common/display"
)

// Version information (set by build flags via -ldflags -X).
var (
	Version   = "dev"
	CommitID  = "unknown"
	BuildDate = "unknown"
)

func main() {
	cmd.BinaryName = filepath.Base(os.Args[0])
	api.ProjectName = "gitignore"
	api.Version = Version

	showHelp := flag.Bool("help", false, "Show help")
	flag.BoolVar(showHelp, "h", false, "Show help (shorthand)")

	showVersion := flag.Bool("version", false, "Show version")
	flag.BoolVar(showVersion, "v", false, "Show version (shorthand)")

	debugFlag := flag.Bool("debug", false, "Enable debug output")
	colorFlag := flag.String("color", "auto", "Color output: auto, yes, no")
	langFlag := flag.String("lang", "", "Language for output")
	serverFlag := flag.String("server", "", "Server URL")
	configFlag := flag.String("config", "", "Config profile name")
	outputFlag := flag.String("output", "", "Output format: text, json, table")
	shellFlag := flag.Bool("shell", false, "Shell integration: --shell {completions|init|help} [SHELL]")

	flag.Usage = func() { cmd.PrintHelp(Version) }
	flag.Parse()

	args := flag.Args()

	// --shell is handled before config/server resolution: it never talks
	// to the network and must work even with no server configured.
	if *shellFlag {
		os.Exit(cmd.HandleShell(args))
	}

	if *showHelp {
		cmd.PrintHelp(Version)
		os.Exit(output.ExitSuccess)
	}

	printer := output.New(output.ResolveColor(*colorFlag))

	if err := clipath.EnsureDirs(); err != nil {
		printer.Error("creating config/data directories: %v", err)
		os.Exit(output.ExitConfig)
	}

	cfg, cfgPath, err := config.Load(*configFlag)
	if err != nil {
		printer.Error("loading config: %v", err)
		os.Exit(output.ExitConfig)
	}

	// Flag-to-config persistence: --server/--color/--output only get
	// written back to cli.yml when the stored value is empty or invalid
	// (AI.md PART 32 "Flag-to-Config Save Rules").
	changed := false
	if result, save := config.SaveIfEmptyOrInvalid(cfg.Server.Primary, *serverFlag, config.IsValidServerURL); save {
		cfg.Server.Primary = result
		changed = true
	}
	if *colorFlag != "" && *colorFlag != "auto" {
		if result, save := config.SaveIfEmptyOrInvalid(cfg.Color, *colorFlag, func(s string) bool {
			return s == "yes" || s == "no" || s == "auto"
		}); save {
			cfg.Color = result
			changed = true
		}
	}
	if result, save := config.SaveIfEmptyOrInvalid(cfg.Output, *outputFlag, func(s string) bool {
		return s == "text" || s == "json" || s == "table"
	}); save {
		cfg.Output = result
		changed = true
	}
	if result, save := config.SaveIfEmptyOrInvalid(cfg.Lang, *langFlag, func(s string) bool { return s != "" }); save {
		cfg.Lang = result
		changed = true
	}
	if changed {
		if err := config.Save(cfgPath, cfg); err != nil {
			printer.Warn("could not persist config: %v", err)
		}
	}

	format := *outputFlag
	if format == "" {
		format = cfg.Output
	}
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" && format != "table" {
		printer.Error("invalid --output value %q: expected text, json, or table", format)
		os.Exit(output.ExitUsage)
	}

	if *debugFlag {
		fmt.Fprintf(os.Stderr, "debug: config=%s server-flag=%q output=%s color=%s\n",
			cfgPath, *serverFlag, format, *colorFlag)
	}

	serverURL, err := config.ResolveServer(*serverFlag, cfg)
	if err != nil {
		if *showVersion {
			// --version must still work without a configured server.
			cmd.PrintVersion(Version, CommitID, BuildDate, nil)
			os.Exit(output.ExitSuccess)
		}
		// No server configured yet: if we're about to launch the TUI (no
		// command args, interactive terminal), let the TUI's first-run
		// setup wizard collect the server URL instead of hard-failing
		// (AI.md PART 32 "first-run config wizard").
		if len(args) == 0 && isTUIEligible() {
			os.Exit(runInteractive(api.New(""), cfg, cfgPath))
		}
		printer.Error("%v", err)
		os.Exit(output.ExitConfig)
	}

	client := api.New(serverURL)

	if *showVersion {
		cmd.PrintVersion(Version, CommitID, BuildDate, client)
		os.Exit(output.ExitSuccess)
	}

	// Mode detection (AI.md PART 32 "Automatic Mode Detection"): no
	// subcommand + an interactive terminal launches the bubbletea TUI by
	// default; explicit commands and non-interactive/piped/dumb terminals
	// always use the plain CLI path. There is no --tui/--cli flag.
	if len(args) == 0 {
		if isTUIEligible() {
			os.Exit(runInteractive(client, cfg, cfgPath))
		}
		printer.Error("no command given; run %s --help for usage, or run interactively for the TUI", cmd.BinaryName)
		os.Exit(output.ExitUsage)
	}

	os.Exit(cmd.Dispatch(client, printer, format, args))
}

// isTUIEligible reports whether the current process environment qualifies
// for the bubbletea TUI (interactive terminal, not piped/dumb/non-tty).
// A native-display (GUI-capable) environment also falls back to the TUI
// here since this build has no native GUI implementation compiled in.
func isTUIEligible() bool {
	env := display.DetectDisplayEnv()
	return env.Mode == display.DisplayModeTUI || env.Mode == display.DisplayModeGUI
}

// runInteractive launches the bubbletea TUI and maps its outcome to a CLI
// exit code.
func runInteractive(client *api.Client, cfg *config.Config, cfgPath string) int {
	if err := tui.Run(client, cfg, cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return output.ExitGeneral
	}
	return output.ExitSuccess
}
