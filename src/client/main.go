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
	"strconv"
	"time"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/cmd"
	"github.com/apimgr/gitignore/src/client/config"
	"github.com/apimgr/gitignore/src/client/gui"
	"github.com/apimgr/gitignore/src/client/output"
	clipath "github.com/apimgr/gitignore/src/client/path"
	"github.com/apimgr/gitignore/src/client/tui"
	"github.com/apimgr/gitignore/src/common/display"
	"github.com/apimgr/gitignore/src/common/i18n"
)

// Version information (set by build flags via -ldflags -X).
var (
	Version    = "dev"
	CommitID   = "unknown"
	BuildDate  = "N/A"
	BuildEpoch = "0"
)

func init() {
	if epoch, err := strconv.ParseInt(BuildEpoch, 10, 64); err == nil && epoch > 0 {
		BuildDate = buildDate(epoch)
	}
}

func buildDate(epoch int64) string {
	return time.Unix(epoch, 0).UTC().Format(time.RFC3339)
}

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
		// No server configured yet: an interactive TUI or GUI can collect
		// the server URL with its first-run setup screen.
		if len(args) == 0 {
			mode, modeErr := selectInteractiveMode(cfg.Display.Mode)
			if modeErr != nil {
				printer.Error("%v", modeErr)
				os.Exit(output.ExitConfig)
			}
			os.Exit(runInteractive(mode, api.New(""), cfg, cfgPath))
		}
		printer.Error("%v", err)
		os.Exit(output.ExitConfig)
	}

	client := api.New(serverURL)
	// Resolve the output language (--lang → config → LC_ALL/LANG → en) and
	// advertise it so the server localizes error messages (AI.md PART 30).
	client.Lang = i18n.ResolveCLILang(*langFlag, cfg.Lang)

	if *showVersion {
		cmd.PrintVersion(Version, CommitID, BuildDate, client)
		os.Exit(output.ExitSuccess)
	}

	// A command argument always uses the plain CLI path. With no command,
	// display.mode and the local display/TTY environment choose GUI or TUI.
	if len(args) == 0 {
		mode, modeErr := selectInteractiveMode(cfg.Display.Mode)
		if modeErr != nil {
			printer.Error("%v", modeErr)
			os.Exit(output.ExitConfig)
		}
		os.Exit(runInteractive(mode, client, cfg, cfgPath))
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

type interactiveMode int

const (
	interactiveTUI interactiveMode = iota
	interactiveGUI
)

func selectInteractiveMode(preference string) (interactiveMode, error) {
	env := display.DetectDisplayEnv()
	if env.IsSSH || env.IsMosh {
		if !isTUIEligible() {
			return 0, fmt.Errorf("interactive mode unavailable: remote sessions require an interactive terminal")
		}
		if preference == "gui" {
			return 0, fmt.Errorf("GUI unavailable in remote SSH or Mosh sessions")
		}
		return interactiveTUI, nil
	}

	switch preference {
	case "gui":
		if !gui.IsGUIAvailable() {
			return 0, fmt.Errorf("GUI unavailable: no supported local display or GUI build is available")
		}
		return interactiveGUI, nil
	case "tui":
		if !isTUIEligible() {
			return 0, fmt.Errorf("TUI unavailable: an interactive terminal is required")
		}
		return interactiveTUI, nil
	case "", "auto":
		if gui.IsGUIAvailable() {
			return interactiveGUI, nil
		}
		if isTUIEligible() {
			return interactiveTUI, nil
		}
		return 0, fmt.Errorf("interactive mode unavailable: connect a display or interactive terminal")
	default:
		return 0, fmt.Errorf("invalid display.mode %q: expected auto, gui, or tui", preference)
	}
}

// runInteractive launches the selected graphical or terminal UI.
func runInteractive(mode interactiveMode, client *api.Client, cfg *config.Config, cfgPath string) int {
	if mode == interactiveGUI {
		if err := gui.LaunchGUI(client, cfg, cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "gui error: %v\n", err)
			return output.ExitGeneral
		}
		return output.ExitSuccess
	}
	if err := tui.Run(client, cfg, cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return output.ExitGeneral
	}
	return output.ExitSuccess
}
