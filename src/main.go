package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/paths"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/service"
	"github.com/apimgr/gitignore/src/templates"
)

// Version information (set by build flags)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

const projectName = "gitignore"

func init() {
	log.SetPrefix("gitignore: ")
	log.SetFlags(log.Lshortfile)
}

func main() {
	dirs := paths.GetDirectories()

	// Flags
	port := flag.String("port", "", "Server port (overrides config)")
	address := flag.String("address", "", "Server address (overrides config)")
	configDirFlag := flag.String("config", "", "Configuration directory")
	showVersion := flag.Bool("version", false, "Show version information")
	showStatus := flag.Bool("status", false, "Check server status (health check)")
	showHelp := flag.Bool("help", false, "Show help")

	// Mode: production (default) | development | dev | prod
	modeFlag := flag.String("mode", "", "Application mode: production|development (aliases: prod|dev)")

	// Service commands
	serviceCmd := flag.String("service", "", "Service commands: start, stop, restart, reload, status, --install, --uninstall, --disable, --help")

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", "Maintenance commands: backup, restore, update, mode, setup")

	// Update commands
	updateCmd := flag.String("update", "", "Update commands: check, yes, branch {stable|beta|daily}")

	flag.Parse()

	if *showHelp {
		printHelp()
		return
	}

	if *showVersion {
		fmt.Println(Version)
		return
	}

	// Resolve config directory
	configDir := dirs.Config
	if *configDirFlag != "" {
		configDir = *configDirFlag
	} else if envConfig := os.Getenv("CONFIG_DIR"); envConfig != "" {
		configDir = envConfig
	}

	// Override data/log dirs from environment (init-only)
	dataDir := dirs.Data
	if v := os.Getenv("DATA_DIR"); v != "" {
		dataDir = v
	}

	// Ensure directories exist
	if err := paths.EnsureDirectories(dirs); err != nil {
		log.Printf("Warning: failed to create directories: %v", err)
	}

	// Load configuration (auto-creates with random 64xxx port on first run)
	configPath := filepath.Join(configDir, "server.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Warning: failed to load config: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	// Health check (uses port from config)
	if *showStatus {
		checkPort := cfg.Server.Port
		if checkPort == "" {
			checkPort = "64580"
		}
		if err := checkHealth(checkPort); err != nil {
			fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")
		os.Exit(0)
	}

	// Handle --mode flag (with shortcuts)
	if *modeFlag != "" {
		resolved := resolveMode(*modeFlag)
		if resolved == "" {
			fmt.Fprintf(os.Stderr, "Invalid mode: %s\nValid: production, development (aliases: prod, dev)\n", *modeFlag)
			os.Exit(2)
		}
		if err := config.Update(func(c *config.Config) { c.Server.Mode = resolved }); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}
		fmt.Printf("Application mode set to: %s\n", resolved)
		return
	}

	// Handle --update flag
	if *updateCmd != "" {
		handleUpdateCommand(*updateCmd, cfg)
		return
	}

	// Handle --service flag
	if *serviceCmd != "" {
		handleServiceCommand(*serviceCmd, configDir)
		return
	}

	// Handle --maintenance flag
	if *maintenanceCmd != "" {
		handleMaintenanceCommand(*maintenanceCmd, configDir, dataDir, dirs.Logs, configPath)
		return
	}

	if len(flag.Args()) != 0 {
		flag.Usage()
		return
	}

	// ── Initialize database ──────────────────────────────────────────────────
	if err := db.Init(dataDir); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// ── First-run: generate admin credentials ─────────────────────────────
	if hasCredentials, err := db.HasAdminCredentials(); err == nil && !hasCredentials {
		adminUser := "admin"
		adminPass, err := db.GeneratePassword(20)
		if err != nil {
			log.Fatalf("Failed to generate admin password: %v", err)
		}
		rawToken, err := db.GenerateToken(32)
		if err != nil {
			log.Fatalf("Failed to generate API token: %v", err)
		}
		if err := db.SetAdminCredentials(adminUser, adminPass, rawToken); err != nil {
			log.Fatalf("Failed to store admin credentials: %v", err)
		}

		// Display ONCE — not logged, printed directly to stdout
		fmt.Println()
		fmt.Println("══════════════════════════════════════════════════════════")
		fmt.Printf("  Admin credentials (shown once — copy now)\n\n")
		fmt.Printf("  Username : %s\n", adminUser)
		fmt.Printf("  Password : %s\n", adminPass)
		fmt.Printf("  API Token: %s\n", rawToken)
		fmt.Println("══════════════════════════════════════════════════════════")
		fmt.Println()
	}

	// ── Resolve server address & port ────────────────────────────────────────
	serverAddress := cfg.Server.Address
	if *address != "" {
		serverAddress = *address
	} else if envAddr := os.Getenv("LISTEN"); envAddr != "" {
		serverAddress = envAddr
	}
	if serverAddress == "" {
		serverAddress = "[::]"
	}

	serverPort := cfg.Server.Port
	if *port != "" {
		serverPort = *port
	} else if envPort := os.Getenv("PORT"); envPort != "" {
		serverPort = envPort
	}
	portNum, _ := strconv.Atoi(serverPort)

	// Apply mode from environment if set
	if envMode := os.Getenv("MODE"); envMode != "" && cfg.Server.Mode == "production" {
		cfg.Server.Mode = resolveMode(envMode)
	}
	devMode := cfg.Server.Mode == "development"

	// ── Load templates ───────────────────────────────────────────────────────
	log.Println("Loading .gitignore templates...")
	templateMgr, err := templates.New()
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}
	log.Printf("Loaded %d templates", templateMgr.Count())

	// ── Signal handling ──────────────────────────────────────────────────────
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	// ── Start server ─────────────────────────────────────────────────────────
	pathMgr := paths.New()
	srv := server.New(&server.Config{
		Address:   serverAddress,
		Port:      portNum,
		DevMode:   devMode,
		Templates: templateMgr,
		Paths:     pathMgr,
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		Cfg:       cfg,
	})

	log.Printf("gitignore %s (commit: %s, built: %s)", Version, Commit, BuildDate)
	log.Printf("Listening on %s:%d", serverAddress, portNum)
	if devMode {
		log.Println("Development mode enabled")
	}

	errChan := make(chan error, 1)
	go func() { errChan <- srv.Start() }()

	for {
		select {
		case err := <-errChan:
			log.Fatal(err)
		case sig := <-sigChan:
			switch sig {
			case syscall.SIGHUP:
				log.Println("Received SIGHUP, reloading configuration...")
				if _, err := config.Load(configPath); err != nil {
					log.Printf("Failed to reload config: %v", err)
				} else {
					log.Println("Configuration reloaded")
				}
			default:
				log.Printf("Received signal %v, shutting down...", sig)
				os.Exit(0)
			}
		}
	}
}

// resolveMode normalises mode shortcuts per spec
func resolveMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "production", "prod":
		return "production"
	case "development", "dev":
		return "development"
	}
	return ""
}

func printHelp() {
	fmt.Printf(`gitignore %s

Usage: gitignore [options]

Options:
  --port PORT          Server port (default: random 64000-64999)
  --address ADDRESS    Listen address (default: [::])
  --config DIR         Configuration directory
  --mode MODE          Application mode: production|development (aliases: prod|dev)
  --version            Print version information
  --status             Health check (exit 0 = healthy)
  --help               Show this help message

Service Commands:
  --service start      Start the service
  --service stop       Stop the service
  --service restart    Restart the service
  --service reload     Reload configuration
  --service status     Show service status
  --service --install  Install as system service
  --service --uninstall Remove system service

Maintenance Commands:
  --maintenance backup [file]   Backup configuration and data
  --maintenance restore [file]  Restore from backup
  --maintenance update          Check for and install updates

Update Commands:
  --update check               Check for updates
  --update yes                 Install latest update
  --update branch BRANCH       Set update branch (stable|beta|daily)

Environment Variables (runtime):
  PORT       Server port
  LISTEN     Listen address
  MODE       Application mode
  DOMAIN     FQDN override

Environment Variables (init-only, first run):
  CONFIG_DIR   Configuration directory
  DATA_DIR     Data directory
  LOG_DIR      Log directory

Configuration:
  Root:    /etc/apimgr/gitignore/server.yml
  User:    ~/.config/apimgr/gitignore/server.yml
  Docker:  /config/server.yml

`, Version)
}

func checkHealth(port string) error {
	url := fmt.Sprintf("http://127.0.0.1:%s/healthz", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}

func handleServiceCommand(cmd, configDir string) {
	switch cmd {
	case "start":
		if err := service.Start(); err != nil {
			log.Fatalf("Failed to start service: %v", err)
		}
	case "stop":
		if err := service.Stop(); err != nil {
			log.Fatalf("Failed to stop service: %v", err)
		}
	case "restart":
		if err := service.Restart(); err != nil {
			log.Fatalf("Failed to restart service: %v", err)
		}
	case "reload":
		if err := service.Reload(); err != nil {
			log.Fatalf("Failed to reload service: %v", err)
		}
	case "status":
		serviceStatus()
	case "--install":
		if err := service.Install(); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}
	case "--uninstall":
		if err := service.Uninstall(); err != nil {
			log.Fatalf("Failed to uninstall service: %v", err)
		}
	case "--disable":
		serviceDisable()
	case "--help":
		fmt.Println("Service commands: start, stop, restart, reload, status, --install, --uninstall, --disable")
	default:
		fmt.Fprintf(os.Stderr, "Unknown service command: %s\n", cmd)
		os.Exit(1)
	}
}

func handleMaintenanceCommand(cmd, configDir, dataDir, logsDir, configPath string) {
	args := flag.Args()

	switch cmd {
	case "backup":
		backupFile := ""
		if len(args) > 0 {
			backupFile = args[0]
		} else {
			backupDir := paths.GetBackupDir()
			if err := os.MkdirAll(backupDir, 0755); err != nil {
				log.Fatalf("Failed to create backup directory: %v", err)
			}
			timestamp := time.Now().Format("20060102-150405")
			backupFile = filepath.Join(backupDir, fmt.Sprintf("gitignore-backup-%s.tar.gz", timestamp))
		}
		maintenanceBackup(configDir, dataDir, backupFile)

	case "restore":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: gitignore --maintenance restore <backup-file>")
			os.Exit(2)
		}
		maintenanceRestore(args[0], configDir, dataDir)

	case "update":
		maintenanceUpdate()

	case "mode":
		cfg, _ := config.Load(configPath)
		mode := cfg.Server.Mode
		if mode == "" {
			mode = "production"
		}
		fmt.Printf("Current mode: %s\n", mode)

	case "setup":
		cfg, _ := config.Load(configPath)
		fmt.Println("gitignore Setup")
		fmt.Println("===============")
		fmt.Printf("Config: %s\n", configPath)
		fmt.Printf("Port:   %s\n", cfg.Server.Port)
		fmt.Printf("Mode:   %s\n", cfg.Server.Mode)
		fmt.Println("Setup complete.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown maintenance command: %s\n", cmd)
		fmt.Fprintln(os.Stderr, "Available: backup, restore, update, mode, setup")
		os.Exit(1)
	}
}

func serviceStatus() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "status", projectName)
	case "darwin":
		runCommand("launchctl", "list", "apimgr.gitignore")
	default:
		fmt.Printf("Service status not supported on %s\n", runtime.GOOS)
	}
}

func serviceDisable() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "disable", projectName)
	case "darwin":
		runCommand("launchctl", "unload", "/Library/LaunchDaemons/apimgr.gitignore.plist")
	default:
		fmt.Printf("Service disable not supported on %s\n", runtime.GOOS)
	}
}

func maintenanceBackup(configDir, dataDir, backupFile string) {
	fmt.Printf("Creating backup: %s\n", backupFile)
	cmd := exec.Command("tar", "-czf", backupFile, "-C", filepath.Dir(configDir), filepath.Base(configDir))
	if err := cmd.Run(); err != nil {
		log.Fatalf("Backup failed: %v", err)
	}
	fmt.Printf("Backup created: %s\n", backupFile)
}

func maintenanceRestore(backupFile, configDir, dataDir string) {
	fmt.Printf("Restoring from: %s\n", backupFile)
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		log.Fatalf("Backup file not found: %s", backupFile)
	}
	cmd := exec.Command("tar", "-xzf", backupFile, "-C", "/")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Restore failed: %v", err)
	}
	fmt.Println("Restore completed")
}

func maintenanceUpdate() {
	fmt.Printf("Current version: %s\n", Version)
	fmt.Println("Visit https://github.com/apimgr/gitignore/releases for the latest version")
}

func handleUpdateCommand(cmd string, cfg *config.Config) {
	args := flag.Args()
	switch cmd {
	case "check":
		fmt.Printf("Current version: %s\n", Version)
		fmt.Printf("Update branch: %s\n", cfg.Server.UpdateBranch)
	case "yes":
		fmt.Println("Visit https://github.com/apimgr/gitignore/releases for the latest version")
	case "branch":
		if len(args) == 0 {
			fmt.Printf("Current branch: %s\n", cfg.Server.UpdateBranch)
			return
		}
		if args[0] != "stable" && args[0] != "beta" && args[0] != "daily" {
			fmt.Fprintf(os.Stderr, "Invalid branch: %s\nValid: stable, beta, daily\n", args[0])
			os.Exit(2)
		}
		if err := config.Update(func(c *config.Config) { c.Server.UpdateBranch = args[0] }); err != nil {
			log.Printf("Failed to save config: %v", err)
		}
		fmt.Printf("Branch set to: %s\n", args[0])
	default:
		fmt.Fprintf(os.Stderr, "Unknown update command: %s\n", cmd)
		os.Exit(1)
	}
}

func runCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Command failed: %s %v: %v", name, args, err)
	}
}
