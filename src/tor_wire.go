package main

import (
	"context"
	"fmt"
	"os"

	"github.com/apimgr/gitignore/src/config"
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
func startTor(ctx context.Context, cfg *config.Config, configDir, dataDir string, serverPort int) *tor.TorManager {
	if _, found := tor.FindBinary(cfg.Server.Tor.Binary); !found {
		fmt.Println("Tor hidden service: disabled (tor not installed)")
		return nil
	}

	torCfg := cfg.Server.Tor
	mgr := tor.NewTorManager(ctx, serverPort, configDir, dataDir, &torCfg)

	go func() {
		if err := mgr.Start(); err != nil {
			fmt.Printf("Tor: bootstrap failed: %v\n", err)
			return
		}
		fmt.Printf("Tor: %s\n", mgr.OnionAddress())
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
			fmt.Println("Tor Hidden Service: disabled (tor not installed)")
			return
		}
		addr := tor.ReadHostname(dataDir)
		if addr == "" {
			fmt.Printf("Tor Hidden Service: configured (binary: %s)\n", bin)
			fmt.Println("  Address: not yet generated (start the server once)")
			return
		}
		fmt.Println("Tor Hidden Service: Connected")
		fmt.Printf("  Address: %s\n", addr)

	case "validate":
		bin, err := tor.ValidateConfig(&cfg.Server.Tor)
		if err != nil {
			fmt.Printf("Tor config invalid: %v\n", err)
			os.Exit(exConfig)
		}
		fmt.Printf("Tor config OK (binary: %s)\n", bin)

	case "restart", "reload":
		fmt.Println("Tor is managed by the running server process.")
		fmt.Println("Restart the server (or send it SIGHUP) to restart Tor.")

	case "regenerate":
		if err := tor.RegenerateKeys(dataDir); err != nil {
			fmt.Printf("Failed to regenerate address: %v\n", err)
			os.Exit(exIOErr)
		}
		fmt.Println("Onion keys deleted; a new .onion address is generated on next server start.")

	case "vanity":
		handleTorVanity(args[1:], dataDir)

	case "import-keys":
		if len(args) < 2 {
			fmt.Println("Usage: gitignore tor import-keys <path>")
			os.Exit(exUsage)
		}
		id, err := tor.ImportKey(dataDir, args[1])
		if err != nil {
			fmt.Printf("Failed to import keys: %v\n", err)
			os.Exit(exIOErr)
		}
		fmt.Printf("Imported keys for %s.onion; restart the server to apply.\n", id)

	default:
		fmt.Println("Tor commands: status, validate, restart, regenerate, vanity start <prefix>, vanity apply, import-keys <path>")
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
			fmt.Println("Usage: gitignore tor vanity start <prefix>")
			os.Exit(exUsage)
		}
		prefix := args[1]
		fmt.Printf("Searching for a .onion address starting with %q (Ctrl-C to stop)...\n", prefix)
		blob, id, err := tor.GenerateVanityKey(context.Background(), prefix)
		if err != nil {
			fmt.Printf("Vanity search failed: %v\n", err)
			os.Exit(exUsage)
		}
		if err := tor.StageVanityKey(dataDir, blob); err != nil {
			fmt.Printf("Failed to stage vanity key: %v\n", err)
			os.Exit(exIOErr)
		}
		fmt.Printf("Found %s.onion — run 'gitignore tor vanity apply' to use it.\n", id)

	case "apply":
		id, err := tor.ApplyStagedVanityKey(dataDir)
		if err != nil {
			fmt.Printf("Failed to apply vanity key: %v\n", err)
			os.Exit(exIOErr)
		}
		fmt.Printf("Applied %s.onion; restart the server to activate.\n", id)

	default:
		fmt.Println("Vanity commands: vanity start <prefix>, vanity apply")
		os.Exit(exUsage)
	}
}
