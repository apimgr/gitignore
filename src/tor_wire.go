package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/tor"
)

// torBinaryInstalled reports whether a usable tor binary is available, honoring
// the configured binary path (AI.md PART 31 detection order).
func torBinaryInstalled(cfg *config.Config) bool {
	_, found := tor.FindBinary(cfg.Server.Tor.Binary)
	return found
}

// startTor launches the Tor hidden service in the background when a tor binary
// is present. Startup is best-effort and non-blocking: bootstrap can take
// minutes, and the server never fails to start because of Tor (AI.md PART 31).
// It returns the manager (which the caller closes on shutdown) or nil when Tor
// is unavailable.
func startTor(ctx context.Context, cfg *config.Config, configDir, dataDir string, serverPort int, srv *server.Server) *tor.TorManager {
	if _, found := tor.FindBinary(cfg.Server.Tor.Binary); !found {
		fmt.Println(cliText("cli.tor.disabled"))
		return nil
	}

	torCfg := cfg.Server.Tor
	mgr := tor.NewTorManager(ctx, serverPort, configDir, dataDir, &torCfg)
	go srv.WatchTorBackend(ctx, mgr.BackendPort, mgr.OnionAddress)

	go func() {
		if err := mgr.Start(); err != nil {
			fmt.Println(cliText("cli.tor.bootstrap_failed", "error", err))
			return
		}
		mgr.Monitor(ctx)
	}()

	return mgr
}

// handleTorCommand implements the `tor` CLI subcommand (AI.md PART 31). These
// commands operate on on-disk key material and configuration; where a change
// affects a running server it takes effect after the server restarts (or is
// sent SIGHUP).
func handleTorCommand(args []string, cfg *config.Config, dataDir string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "status":
		bin, found := tor.FindBinary(cfg.Server.Tor.Binary)
		if !found {
			fmt.Println(cliText("cli.tor.disabled"))
			return
		}
		addr := tor.ReadHostname(dataDir)
		if addr == "" {
			fmt.Println(cliText("cli.tor.configured", "binary", bin))
			fmt.Println(cliText("cli.tor.address_pending"))
			return
		}
		fmt.Println(cliText("cli.tor.connected"))
		fmt.Println(cliText("cli.tor.address", "address", addr))

	case "validate":
		bin, err := tor.ValidateConfig(&cfg.Server.Tor)
		if err != nil {
			fmt.Println(cliText("cli.tor.config_invalid", "error", err))
			os.Exit(exConfig)
		}
		fmt.Println(cliText("cli.tor.config_ok", "binary", bin))

	case "restart", "reload":
		fmt.Println(cliText("cli.tor.managed_by_server"))
		fmt.Println(cliText("cli.tor.restart_instruction"))

	case "regenerate":
		if err := tor.RegenerateKeys(dataDir); err != nil {
			fmt.Println(cliText("cli.tor.regenerate_failed", "error", err))
			os.Exit(exIOErr)
		}
		fmt.Println(cliText("cli.tor.keys_regenerated"))

	case "vanity":
		handleTorVanity(args[1:], dataDir)

	case "import-keys":
		if len(args) < 2 {
			fmt.Println(cliText("cli.tor.import_keys_usage", "binary", filepath.Base(os.Args[0])))
			os.Exit(exUsage)
		}
		id, err := tor.ImportKey(dataDir, args[1])
		if err != nil {
			fmt.Println(cliText("cli.tor.import_keys_failed", "error", err))
			os.Exit(exIOErr)
		}
		fmt.Println(cliText("cli.tor.import_keys_succeeded", "address", id+".onion"))

	default:
		fmt.Println(cliText("cli.tor.commands"))
		os.Exit(exUsage)
	}
}

// handleTorVanity implements `tor vanity start <prefix>` and `tor vanity apply`.
func handleTorVanity(args []string, dataDir string) {
	action := ""
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "start":
		if len(args) < 2 {
			fmt.Println(cliText("cli.tor.vanity_start_usage", "binary", filepath.Base(os.Args[0])))
			os.Exit(exUsage)
		}
		prefix := args[1]
		fmt.Println(cliText("cli.tor.vanity_searching", "prefix", prefix))
		blob, id, err := tor.GenerateVanityKey(context.Background(), prefix)
		if err != nil {
			fmt.Println(cliText("cli.tor.vanity_search_failed", "error", err))
			os.Exit(exUsage)
		}
		if err := tor.StageVanityKey(dataDir, blob); err != nil {
			fmt.Println(cliText("cli.tor.vanity_stage_failed", "error", err))
			os.Exit(exIOErr)
		}
		fmt.Println(cliText("cli.tor.vanity_found", "address", id+".onion"))

	case "apply":
		id, err := tor.ApplyStagedVanityKey(dataDir)
		if err != nil {
			fmt.Println(cliText("cli.tor.vanity_apply_failed", "error", err))
			os.Exit(exIOErr)
		}
		fmt.Println(cliText("cli.tor.vanity_applied", "address", id+".onion"))

	default:
		fmt.Println(cliText("cli.tor.vanity_commands"))
		os.Exit(exUsage)
	}
}
