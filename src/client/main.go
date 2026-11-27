// Command gitignore-cli is the CLI client for the gitignore template server.
// It talks to public /api/v1/* endpoints without a token and accepts an
// optional Bearer token for protected operations, including private CLI updates.
//
//	gitignore-cli Go Node > .gitignore
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/cmd"
	"github.com/apimgr/gitignore/src/client/config"
	"github.com/apimgr/gitignore/src/client/gui"
	"github.com/apimgr/gitignore/src/client/output"
	clipath "github.com/apimgr/gitignore/src/client/path"
	"github.com/apimgr/gitignore/src/client/tui"
	clientupdate "github.com/apimgr/gitignore/src/client/update"
	"github.com/apimgr/gitignore/src/common/display"
	"github.com/apimgr/gitignore/src/common/i18n"
	serverconfig "github.com/apimgr/gitignore/src/config"
)

// Version information (set by build flags via -ldflags -X).
var (
	Version    = "dev"
	CommitID   = "unknown"
	BuildDate  = "N/A"
	BuildEpoch = "0"
)

var clientLanguage = i18n.DefaultLang

func clientText(key string, values ...interface{}) string {
	return i18n.TranslateFormat(clientLanguage, key, values...)
}

func setClientLanguage(flagLang, configLang string) {
	clientLanguage = i18n.ResolveCLILang(flagLang, configLang)
	cmd.SetLanguage(clientLanguage)
}

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
	flag.CommandLine = flag.NewFlagSet(cmd.BinaryName, flag.ContinueOnError)

	showHelp := flag.Bool("help", false, "Show help")
	flag.BoolVar(showHelp, "h", false, "Show help (shorthand)")

	showVersion := flag.Bool("version", false, "Show version")
	flag.BoolVar(showVersion, "v", false, "Show version (shorthand)")

	debugFlag := flag.Bool("debug", false, "Enable debug output")
	colorFlag := flag.String("color", "auto", "Color output: auto, yes, no")
	langFlag := flag.String("lang", "", "Language for output")
	serverFlag := flag.String("server", "", "Server URL")
	tokenFlag := flag.String("token", "", "API token for protected operations")
	updateFlag := flag.String("update", "", "Check for CLI updates: check, yes")
	configFlag := flag.String("config", "", "Config profile name")
	outputFlag := flag.String("output", "", "Output format: text, json, table")
	shellFlag := flag.Bool("shell", false, "Shell integration: --shell {completions|init|help} [SHELL]")

	flag.Usage = func() { cmd.PrintHelp(Version) }
	flag.CommandLine.SetOutput(io.Discard)
	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		setClientLanguage(*langFlag, "")
		fmt.Fprintln(os.Stderr, clientText("cli.client.main.invalid_arguments"))
		os.Exit(output.ExitUsage)
	}
	setClientLanguage(*langFlag, "")

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
	printer.SetLanguage(clientLanguage)

	if err := clipath.EnsureDirs(); err != nil {
		printer.ErrorText(clientText("cli.client.main.ensure_directories_failed", "error", err))
		os.Exit(output.ExitConfig)
	}

	cfg, cfgPath, err := config.Load(*configFlag, clientLanguage)
	if err != nil {
		printer.ErrorText(clientText("cli.client.main.load_config_failed", "error", err))
		os.Exit(output.ExitConfig)
	}
	setClientLanguage(*langFlag, cfg.Lang)
	printer.SetLanguage(clientLanguage)

	// Flag-to-config persistence: --server/--color/--output only get
	// written back to cli.yml when the stored value is empty or invalid
	// (AI.md PART 32 "Flag-to-Config Save Rules").
	changed := false
	if result, save := config.SaveIfEmptyOrInvalid(cfg.Server.Primary, *serverFlag, config.IsValidServerURL); save {
		cfg.Server.Primary = result
		changed = true
	}
	if result, save := config.SaveIfEmptyOrInvalid(cfg.Auth.Token, *tokenFlag, func(value string) bool {
		return strings.TrimSpace(value) != ""
	}); save {
		cfg.Auth.Token = result
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
	if result, save := config.SaveIfEmptyOrInvalid(cfg.Lang, *langFlag, i18n.IsSupported); save {
		cfg.Lang = result
		changed = true
	}
	if changed {
		if err := config.Save(cfgPath, cfg); err != nil {
			printer.WarnText(clientText("cli.client.main.persist_config_failed", "error", err))
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
		printer.ErrorText(clientText("cli.client.main.invalid_output", "format", format))
		os.Exit(output.ExitUsage)
	}

	if *debugFlag {
		fmt.Fprintln(os.Stderr, clientText("cli.client.main.debug", "config", cfgPath, "server", *serverFlag, "output", format, "color", *colorFlag))
	}
	client := api.New("")
	client.Lang = clientLanguage
	token, tokenErr := resolveClientToken(*tokenFlag, cfg)
	if tokenErr != nil {
		printer.ErrorText(tokenErr.Error())
		os.Exit(output.ExitConfig)
	}
	client.Token = token

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
				printer.ErrorText(modeErr.Error())
				os.Exit(output.ExitConfig)
			}
			os.Exit(runInteractive(mode, client, cfg, cfgPath))
		}
		printer.ErrorText(clientText("cli.client.main.no_server_configured"))
		os.Exit(output.ExitConfig)
	}

	client.BaseURL = serverURL
	// Resolve the output language (--lang → config → LC_ALL/LANG → en) and
	// advertise it so the server localizes error messages (AI.md PART 30).
	client.Lang = clientLanguage
	updateAction := strings.ToLower(strings.TrimSpace(*updateFlag))
	updateResult, updateErr := clientupdate.Check(context.Background(), client, Version, updateAction, serverconfig.IsTruthy(cfg.Update.Auto), display.DetectDisplayEnv().IsTerminal, os.Stdin, os.Stdout)
	if updateErr != nil {
		if updateAction != "" || updateResult.Forced {
			printer.ErrorText(updateErr.Error())
			os.Exit(output.ExitGeneral)
		}
		printer.WarnText(updateErr.Error())
	}
	if updateAction == "check" {
		if updateResult.Available {
			fmt.Fprintf(os.Stdout, "CLI update %s is available. Run %s --update yes to install it.\n", updateResult.Version, cmd.BinaryName)
		} else {
			fmt.Fprintln(os.Stdout, "No CLI update is available.")
		}
		os.Exit(output.ExitSuccess)
	}
	if updateAction == "yes" && !updateResult.Installed {
		fmt.Fprintln(os.Stdout, "No CLI update is available.")
		os.Exit(output.ExitSuccess)
	}

	if *showVersion {
		cmd.PrintVersion(Version, CommitID, BuildDate, client)
		os.Exit(output.ExitSuccess)
	}

	// A command argument always uses the plain CLI path. With no command,
	// display.mode and the local display/TTY environment choose GUI or TUI.
	if len(args) == 0 {
		mode, modeErr := selectInteractiveMode(cfg.Display.Mode)
		if modeErr != nil {
			printer.ErrorText(modeErr.Error())
			os.Exit(output.ExitConfig)
		}
		os.Exit(runInteractive(mode, client, cfg, cfgPath))
	}

	os.Exit(cmd.Dispatch(client, printer, format, args))
}

func resolveClientToken(flagValue string, cfg *config.Config) (string, error) {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv(strings.ToUpper(api.ProjectName) + "_TOKEN")); value != "" {
		return value, nil
	}
	if cfg == nil {
		return "", nil
	}
	if tokenFile := strings.TrimSpace(cfg.Auth.TokenFile); tokenFile != "" {
		tokenFile = os.ExpandEnv(tokenFile)
		if strings.HasPrefix(tokenFile, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				tokenFile = filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(tokenFile, "~"), "/\\"))
			}
		}
		info, err := os.Stat(tokenFile)
		if err != nil {
			return "", fmt.Errorf("read API token file: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() > 64*1024 {
			return "", fmt.Errorf("API token file must be a regular file no larger than 64 KiB")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return "", fmt.Errorf("API token file permissions must be 0600 or more restrictive")
		}
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", fmt.Errorf("read API token file: %w", err)
		}
		if value := strings.TrimSpace(string(data)); value != "" {
			return value, nil
		}
		return "", fmt.Errorf("API token file is empty")
	}
	return strings.TrimSpace(cfg.Auth.Token), nil
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
			return 0, fmt.Errorf("%s", clientText("cli.client.main.remote_requires_terminal"))
		}
		if preference == "gui" {
			return 0, fmt.Errorf("%s", clientText("cli.client.main.gui_unavailable_remote"))
		}
		return interactiveTUI, nil
	}

	switch preference {
	case "gui":
		if !gui.IsGUIAvailable() {
			return 0, fmt.Errorf("%s", clientText("cli.client.main.gui_unavailable"))
		}
		return interactiveGUI, nil
	case "tui":
		if !isTUIEligible() {
			return 0, fmt.Errorf("%s", clientText("cli.client.main.tui_requires_terminal"))
		}
		return interactiveTUI, nil
	case "", "auto":
		if gui.IsGUIAvailable() {
			return interactiveGUI, nil
		}
		if isTUIEligible() {
			return interactiveTUI, nil
		}
		return 0, fmt.Errorf("%s", clientText("cli.client.main.interactive_unavailable"))
	default:
		return 0, fmt.Errorf("%s", clientText("cli.client.main.invalid_display_mode", "mode", preference))
	}
}

// runInteractive launches the selected graphical or terminal UI.
func runInteractive(mode interactiveMode, client *api.Client, cfg *config.Config, cfgPath string) int {
	if mode == interactiveGUI {
		if err := gui.LaunchGUI(client, cfg, cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, clientText("cli.client.main.gui_error", "error", err))
			return output.ExitGeneral
		}
		return output.ExitSuccess
	}
	if err := tui.Run(client, cfg, cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, clientText("cli.client.main.tui_error", "error", err))
		return output.ExitGeneral
	}
	return output.ExitSuccess
}
