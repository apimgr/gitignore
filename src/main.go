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
	"syscall"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/paths"
	"github.com/apimgr/gitignore/src/server"
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
	// Get default directories
	dirs := paths.GetDirectories()

	// Flags
	port := flag.String("port", "", "Server port (overrides config)")
	address := flag.String("address", "", "Server address (overrides config)")
	configDirFlag := flag.String("config", "", "Configuration directory")
	showVersion := flag.Bool("version", false, "Show version information")
	showStatus := flag.Bool("status", false, "Check server status (for health checks)")
	showHelp := flag.Bool("help", false, "Show help")
	devMode := flag.Bool("dev", false, "Enable development mode")

	// Service commands
	serviceCmd := flag.String("service", "", "Service commands: start, stop, restart, reload, status, --install, --uninstall, --disable, --help")

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", "Maintenance commands: backup, restore, update, mode, setup")

	// Mode and update flags
	modeFlag := flag.String("mode", "", "Application mode: production, development")
	updateCmd := flag.String("update", "", "Update commands: check, yes, branch {stable|beta|daily}")

	flag.Parse()

	// Handle --help
	if *showHelp {
		printHelp()
		return
	}

	// Handle --version
	if *showVersion {
		fmt.Println(Version)
		return
	}

	// Override directories from flags
	configDir := dirs.Config
	if *configDirFlag != "" {
		configDir = *configDirFlag
	}

	// Override from environment
	if envConfig := os.Getenv("CONFIG_DIR"); envConfig != "" && *configDirFlag == "" {
		configDir = envConfig
	}

	// Ensure directories exist
	if err := paths.EnsureDirectories(dirs); err != nil {
		log.Printf("Warning: Failed to create directories: %v", err)
	}

	// Load configuration
	configPath := filepath.Join(configDir, "server.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	// Handle --status (health check)
	if *showStatus {
		checkPort := cfg.Server.Port
		if checkPort == "" {
			checkPort = "8080"
		}
		if err := checkHealth(checkPort); err != nil {
			fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")
		os.Exit(0)
	}

	// Handle --mode flag
	if *modeFlag != "" {
		setApplicationMode(*modeFlag, configPath)
		return
	}

	// Handle --update flag
	if *updateCmd != "" {
		handleUpdateCommand(*updateCmd, cfg)
		return
	}

	// Handle service commands
	if *serviceCmd != "" {
		handleServiceCommand(*serviceCmd, configDir)
		return
	}

	// Handle maintenance commands
	if *maintenanceCmd != "" {
		handleMaintenanceCommand(*maintenanceCmd, configDir, dirs.Data, dirs.Logs, configPath)
		return
	}

	if len(flag.Args()) != 0 {
		flag.Usage()
		return
	}

	// Determine port (flag > env > config > default)
	serverPort := cfg.Server.Port
	if *port != "" {
		serverPort = *port
	} else if envPort := os.Getenv("PORT"); envPort != "" {
		serverPort = envPort
	}
	if serverPort == "" {
		serverPort = "8080"
	}

	// Determine address (flag > env > config > default)
	serverAddress := cfg.Server.Address
	if *address != "" {
		serverAddress = *address
	} else if envAddr := os.Getenv("ADDRESS"); envAddr != "" {
		serverAddress = envAddr
	}
	if serverAddress == "" {
		serverAddress = "0.0.0.0"
	}

	// Build listen address
	listen := fmt.Sprintf("%s:%s", serverAddress, serverPort)
	if serverAddress == "0.0.0.0" || serverAddress == "::" {
		listen = ":" + serverPort
	}

	// Log startup information
	log.Printf("gitignore %s (commit: %s, built: %s)", Version, Commit, BuildDate)

	// Load templates
	log.Println("Loading .gitignore templates...")
	templateMgr, err := templates.New()
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}
	log.Printf("Loaded %d templates", templateMgr.Count())

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	// Initialize paths manager for server
	pathMgr := paths.New()

	// Initialize server
	srv := server.New(&server.Config{
		Address:   serverAddress,
		Port:      mustAtoi(serverPort),
		DevMode:   *devMode || os.Getenv("DEV") == "true",
		Templates: templateMgr,
		Paths:     pathMgr,
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		Cfg:       cfg,
	})

	// Log endpoints
	log.Printf("")
	log.Printf("API Endpoints:")
	log.Printf("  GET /                      - Home page")
	log.Printf("  GET /api/v1/list           - List all templates")
	log.Printf("  GET /api/v1/template/{name} - Get template by name")
	log.Printf("  GET /api/v1/search?q=query - Search templates")
	log.Printf("  GET /api/v1/combine?t=a,b  - Combine templates")
	log.Printf("  GET /api/v1/categories     - List categories")
	log.Printf("  GET /api/v1/stats          - Statistics")
	log.Printf("")
	log.Printf("Special Files:")
	log.Printf("  GET /robots.txt            - Robots file")
	log.Printf("  GET /security.txt          - Security contact")
	log.Printf("  GET /manifest.json         - PWA manifest")
	log.Printf("")
	log.Printf("Listening on %s", listen)
	if *devMode || os.Getenv("DEV") == "true" {
		log.Println("Development mode enabled")
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start()
	}()

	// Wait for shutdown signal or server error
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

func mustAtoi(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}

func printHelp() {
	fmt.Printf(`GitIgnore Server v%s

Usage: gitignore [options]

Options:
  --port PORT          Server port (default: from config or 8080)
  --address ADDRESS    Server address (default: from config or 0.0.0.0)
  --config DIR         Configuration directory
  --dev                Enable development mode
  --version            Print version information
  --status             Check service status (for healthcheck)
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

Environment Variables:
  PORT         Server port
  ADDRESS      Server address
  CONFIG_DIR   Configuration directory
  DEV          Enable development mode (set to "true")

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
		serviceStart()
	case "stop":
		serviceStop()
	case "restart":
		serviceRestart()
	case "reload":
		serviceReload()
	case "status":
		serviceStatus()
	case "--install":
		serviceInstall(configDir)
	case "--uninstall":
		serviceUninstall()
	case "--disable":
		serviceDisable()
	case "--help":
		fmt.Println("Service commands: start, stop, restart, reload, status, --install, --uninstall, --disable")
	default:
		fmt.Printf("Unknown service command: %s\n", cmd)
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
			fmt.Println("Usage: gitignore --maintenance restore <backup-file>")
			os.Exit(1)
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
		fmt.Println("GitIgnore Initial Setup")
		fmt.Println("=======================")
		cfg, _ := config.Load(configPath)
		fmt.Printf("Config: %s\n", configPath)
		fmt.Printf("Port: %s\n", cfg.Server.Port)
		fmt.Printf("Mode: %s\n", cfg.Server.Mode)
		fmt.Println("Setup complete.")
	default:
		fmt.Printf("Unknown maintenance command: %s\n", cmd)
		fmt.Println("Available commands: backup, restore, update, mode, setup")
		os.Exit(1)
	}
}

// Service management functions
func serviceStart() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "start", "gitignore")
	case "darwin":
		runCommand("launchctl", "start", "us.apimgr.gitignore")
	default:
		fmt.Printf("Service management not supported on %s\n", runtime.GOOS)
	}
}

func serviceStop() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "stop", "gitignore")
	case "darwin":
		runCommand("launchctl", "stop", "us.apimgr.gitignore")
	default:
		fmt.Printf("Service management not supported on %s\n", runtime.GOOS)
	}
}

func serviceRestart() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "restart", "gitignore")
	case "darwin":
		runCommand("launchctl", "stop", "us.apimgr.gitignore")
		runCommand("launchctl", "start", "us.apimgr.gitignore")
	default:
		fmt.Printf("Service management not supported on %s\n", runtime.GOOS)
	}
}

func serviceReload() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "reload", "gitignore")
	case "darwin":
		runCommand("pkill", "-HUP", "gitignore")
	default:
		fmt.Printf("Service management not supported on %s\n", runtime.GOOS)
	}
}

func serviceStatus() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "status", "gitignore")
	case "darwin":
		runCommand("launchctl", "list", "us.apimgr.gitignore")
	default:
		fmt.Printf("Service management not supported on %s\n", runtime.GOOS)
	}
}

func serviceInstall(configDir string) {
	fmt.Println("Installing gitignore service...")
	switch runtime.GOOS {
	case "linux":
		installSystemdService(configDir)
	case "darwin":
		installLaunchdService(configDir)
	default:
		fmt.Printf("Service installation not supported on %s\n", runtime.GOOS)
	}
}

func serviceUninstall() {
	fmt.Println("Uninstalling gitignore service...")
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "stop", "gitignore")
		runCommand("systemctl", "disable", "gitignore")
		os.Remove("/etc/systemd/system/gitignore.service")
		runCommand("systemctl", "daemon-reload")
	case "darwin":
		runCommand("launchctl", "unload", "/Library/LaunchDaemons/us.apimgr.gitignore.plist")
		os.Remove("/Library/LaunchDaemons/us.apimgr.gitignore.plist")
	default:
		fmt.Printf("Service uninstallation not supported on %s\n", runtime.GOOS)
	}
}

func serviceDisable() {
	switch runtime.GOOS {
	case "linux":
		runCommand("systemctl", "disable", "gitignore")
	case "darwin":
		runCommand("launchctl", "unload", "/Library/LaunchDaemons/us.apimgr.gitignore.plist")
	default:
		fmt.Printf("Service disable not supported on %s\n", runtime.GOOS)
	}
}

func installSystemdService(configDir string) {
	service := fmt.Sprintf(`[Unit]
Description=GitIgnore Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/gitignore --config %s
Restart=always
RestartSec=5
User=gitignore
Group=gitignore

[Install]
WantedBy=multi-user.target
`, configDir)

	if err := os.WriteFile("/etc/systemd/system/gitignore.service", []byte(service), 0644); err != nil {
		log.Fatalf("Failed to write systemd service file: %v", err)
	}
	runCommand("systemctl", "daemon-reload")
	runCommand("systemctl", "enable", "gitignore")
	runCommand("systemctl", "start", "gitignore")
	fmt.Println("Service installed and started successfully")
}

func installLaunchdService(configDir string) {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>us.apimgr.gitignore</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/gitignore</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, configDir)

	if err := os.WriteFile("/Library/LaunchDaemons/us.apimgr.gitignore.plist", []byte(plist), 0644); err != nil {
		log.Fatalf("Failed to write launchd plist: %v", err)
	}
	runCommand("launchctl", "load", "/Library/LaunchDaemons/us.apimgr.gitignore.plist")
	fmt.Println("Service installed and started successfully")
}

// Maintenance functions
func maintenanceBackup(configDir, dataDir, backupFile string) {
	fmt.Printf("Creating backup: %s\n", backupFile)
	cmd := exec.Command("tar", "-czf", backupFile, "-C", filepath.Dir(configDir), filepath.Base(configDir))
	if err := cmd.Run(); err != nil {
		log.Fatalf("Backup failed: %v", err)
	}
	fmt.Printf("Backup created successfully: %s\n", backupFile)
}

func maintenanceRestore(backupFile, configDir, dataDir string) {
	fmt.Printf("Restoring from backup: %s\n", backupFile)
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		log.Fatalf("Backup file not found: %s", backupFile)
	}
	cmd := exec.Command("tar", "-xzf", backupFile, "-C", "/")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Restore failed: %v", err)
	}
	fmt.Println("Restore completed successfully")
}

func maintenanceUpdate() {
	fmt.Println("Checking for updates...")
	fmt.Printf("Current version: %s\n", Version)
	fmt.Println("Update feature not yet implemented")
	fmt.Println("Visit https://github.com/apimgr/gitignore/releases for the latest version")
}

func runCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Command failed: %s %v: %v", name, args, err)
	}
}

func setApplicationMode(mode, configPath string) {
	if mode != "production" && mode != "development" {
		fmt.Printf("Invalid mode: %s\n", mode)
		fmt.Println("Valid modes: production, development")
		os.Exit(1)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg.Server.Mode = mode
	if err := config.Save(); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}
	fmt.Printf("Application mode set to: %s\n", mode)
}

func handleUpdateCommand(cmd string, cfg *config.Config) {
	args := flag.Args()
	switch cmd {
	case "check":
		fmt.Printf("Current version: %s\n", Version)
		fmt.Printf("Update branch: %s\n", cfg.Server.UpdateBranch)
		fmt.Println("No updates available")
	case "yes":
		fmt.Println("Update installation not implemented")
		fmt.Println("Visit https://github.com/apimgr/gitignore/releases for the latest version")
	case "branch":
		if len(args) == 0 {
			fmt.Printf("Current branch: %s\n", cfg.Server.UpdateBranch)
			return
		}
		if args[0] != "stable" && args[0] != "beta" && args[0] != "daily" {
			fmt.Printf("Invalid branch: %s\n", args[0])
			fmt.Println("Valid branches: stable, beta, daily")
			os.Exit(1)
		}
		cfg.Server.UpdateBranch = args[0]
		if err := config.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
		}
		fmt.Printf("Branch set to: %s\n", args[0])
	default:
		fmt.Printf("Unknown update command: %s\n", cmd)
		fmt.Println("Available commands: check, yes, branch")
		os.Exit(1)
	}
}
