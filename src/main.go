package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/mode"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/scheduler"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/service"
	"github.com/apimgr/gitignore/src/template"
	"github.com/apimgr/gitignore/src/tor"
)

// Version information (set by build flags)
var (
	Version   = "dev"
	CommitID  = "unknown"
	BuildDate = "unknown"
	// OfficialSite is the official hosted instance URL, embedded from site.txt
	// or the OFFICIAL_SITE build arg (AI.md PART 25). Empty for self-hosted.
	OfficialSite = ""
)

const projectName = "gitignore"

// sysexits(3) exit codes — never invent custom schemes.
const (
	exUsage       = 64 // command line usage error
	exNoInput     = 66 // cannot open input
	exUnavailable = 69 // a service is unavailable
	exOSErr       = 71 // system error (e.g. can't fork, service control failed)
	exOSFile      = 72 // a (data) file the app depends on is missing/unreadable
	exCantCreat   = 73 // can't create (user) output file
	exIOErr       = 74 // an error occurred while doing I/O on some file
	exConfig      = 78 // configuration error
)

func init() {
	log.SetPrefix("gitignore: ")
	log.SetFlags(log.Lshortfile)
}

func main() {
	dirs := apppath.GetDirectories()

	// binaryName is the actual invoked binary name, shown in --help/--version
	// output (AI.md PART 8). Internal identifiers (service unit, config paths,
	// User-Agent) keep the hardcoded projectName instead.
	binaryName := filepath.Base(os.Args[0])

	// Flags
	port := flag.String("port", "", "Server port (overrides config)")
	address := flag.String("address", "", "Server address (overrides config)")
	configDirFlag := flag.String("config", "", "Configuration directory")

	showVersion := flag.Bool("version", false, "Show version information")
	flag.BoolVar(showVersion, "v", false, "Show version information (shorthand)")

	showStatus := flag.Bool("status", false, "Check server status (health check)")

	showHelp := flag.Bool("help", false, "Show help")
	flag.BoolVar(showHelp, "h", false, "Show help (shorthand)")

	debugFlag := flag.Bool("debug", false, "Enable debug output")
	colorFlag := flag.String("color", "auto", "Color output: auto, yes, no")

	// Mode: production (default) | development | dev | prod
	modeFlag := flag.String("mode", "", "Application mode: production|development (aliases: prod|dev)")

	// Service commands
	serviceCmd := flag.String("service", "", "Service commands: start, stop, restart, reload, status, --install, --uninstall, --disable, --help")

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", "Maintenance commands: backup, restore, update, mode, setup")

	// Force/assume-yes (used by --service --uninstall and other prompts)
	forceFlag := flag.Bool("force", false, "Assume yes to confirmation prompts")
	flag.BoolVar(forceFlag, "y", false, "Assume yes to confirmation prompts (shorthand)")

	// Update commands
	updateCmd := flag.String("update", "", "Update commands: check, yes, branch {stable|beta|daily}")

	// Directory and runtime path flags (AI.md PART 8 "Directory Flags"). Each
	// directory flag creates its target if missing; empty means "use the env
	// override or the OS default".
	dataDirFlag := flag.String("data", "", "Data directory")
	cacheDirFlag := flag.String("cache", "", "Cache directory")
	logDirFlag := flag.String("log", "", "Log directory")
	backupDirFlag := flag.String("backup", "", "Backup directory")
	pidFileFlag := flag.String("pid", "", "PID file path")
	baseURLFlag := flag.String("baseurl", "", "URL path prefix (default: /)")
	daemonFlag := flag.Bool("daemon", false, "Run as daemon (detach from terminal)")
	langFlag := flag.String("lang", "", "Language for output (default: auto, from LANG env)")
	shellCmd := flag.String("shell", "", "Shell integration: completions, init, help [SHELL]")

	flag.Parse()

	if *showHelp {
		printHelp(binaryName)
		return
	}

	if *showVersion {
		fmt.Printf("%s %s\nBuilt: %s\nGo: %s\nOS/Arch: %s/%s\n",
			binaryName, Version, BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}

	// --shell prints completions/init and exits immediately (AI.md PART 8). The
	// subcommand is the flag value; an optional SHELL follows as a positional.
	if *shellCmd != "" {
		os.Exit(handleShell(append([]string{*shellCmd}, flag.Args()...)))
	}

	// --lang sets the output locale (AI.md PART 8). Server terminal output is not
	// yet routed through i18n, so this only exports LANG for locale-aware
	// downstream code and the client; documented as a partial in AUDIT.AI.md.
	if *langFlag != "" {
		os.Setenv("LANG", *langFlag)
	}

	colorEnabled := resolveColor(*colorFlag)

	if *debugFlag {
		// --debug sets the independent debug axis only; it does NOT
		// force development mode (mode and debug are tracked independently)
		mode.SetDebug(true)
		log.SetFlags(log.Lshortfile | log.LstdFlags)
		log.Println("Debug mode enabled")
	}

	// Resolve config directory (flag > CONFIG_DIR env > OS default).
	configDir := dirs.Config
	if *configDirFlag != "" {
		configDir = *configDirFlag
	} else if envConfig := os.Getenv("CONFIG_DIR"); envConfig != "" {
		configDir = envConfig
	}

	// Resolve data directory (flag > DATA_DIR env > OS default). The --data flag
	// exports DATA_DIR so init-only readers (db, backup) observe the override.
	dataDir := dirs.Data
	if *dataDirFlag != "" {
		dataDir = *dataDirFlag
		os.Setenv("DATA_DIR", dataDir)
	} else if v := os.Getenv("DATA_DIR"); v != "" {
		dataDir = v
	}

	// Resolve log directory (flag > LOG_DIR env > OS default).
	logsDir := dirs.Logs
	if *logDirFlag != "" {
		logsDir = *logDirFlag
		os.Setenv("LOG_DIR", logsDir)
	} else if v := os.Getenv("LOG_DIR"); v != "" {
		logsDir = v
	}

	// --cache and --backup export their init-only env vars so GetCacheDir and
	// GetBackupDir resolve the override wherever they are called.
	if *cacheDirFlag != "" {
		os.Setenv("CACHE_DIR", *cacheDirFlag)
	}
	if *backupDirFlag != "" {
		os.Setenv("BACKUP_DIR", *backupDirFlag)
	}

	// Resolve PID file path (flag > OS default; empty on Windows/containers).
	pidFile := apppath.GetPIDFile()
	if *pidFileFlag != "" {
		pidFile = *pidFileFlag
	}

	// Ensure all runtime directories exist with privilege-appropriate perms
	// (AI.md PART 8 "Directory Validation Rules"): 0755 root / 0700 user.
	for _, d := range []string{configDir, dataDir, logsDir, apppath.GetCacheDir(), apppath.GetBackupDir()} {
		if err := ensureRuntimeDir(d); err != nil {
			log.Printf("Warning: failed to create directory %s: %v", d, err)
		}
	}

	// Load configuration (auto-creates with random 64xxx port on first run)
	configPath := filepath.Join(configDir, "server.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Warning: failed to load config: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	// --baseurl overrides the configured URL path prefix (AI.md PART 8).
	if *baseURLFlag != "" {
		cfg.Server.BaseURL = normalizeBaseURL(*baseURLFlag)
	} else {
		cfg.Server.BaseURL = normalizeBaseURL(cfg.Server.BaseURL)
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
		if addr := tor.ReadHostname(dataDir); addr != "" {
			fmt.Println("Tor Hidden Service: Connected")
			fmt.Printf("  Address: %s\n", addr)
		}
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
			log.Printf("Failed to save config: %v", err)
			os.Exit(exConfig)
		}
		fmt.Printf("Application mode set to: %s\n", resolved)
		return
	}

	// Handle --update flag
	if *updateCmd != "" {
		handleUpdateCommand(*updateCmd, flag.Args(), cfg)
		return
	}

	// Handle --service flag
	if *serviceCmd != "" {
		handleServiceCommand(*serviceCmd, configDir, *forceFlag)
		return
	}

	// Handle --maintenance flag
	if *maintenanceCmd != "" {
		handleMaintenanceCommand(*maintenanceCmd, configDir, dataDir, logsDir, configPath)
		return
	}

	// Scheduler subcommand (AI.md PART 18 "CLI Commands").
	if cmdArgs := flag.Args(); len(cmdArgs) > 0 && cmdArgs[0] == "scheduler" {
		handleSchedulerCommand(cmdArgs[1:], cfg, configDir, dataDir, logsDir, colorEnabled)
		return
	}

	// Tor subcommand (AI.md PART 31 "CLI").
	if cmdArgs := flag.Args(); len(cmdArgs) > 0 && cmdArgs[0] == "tor" {
		handleTorCommand(cmdArgs[1:], cfg, dataDir)
		return
	}

	// Email subcommand (AI.md PART 17 "CLI").
	if cmdArgs := flag.Args(); len(cmdArgs) > 0 && cmdArgs[0] == "email" {
		handleEmailCommand(cmdArgs[1:], cfg, configDir)
		return
	}

	if len(flag.Args()) != 0 {
		flag.Usage()
		return
	}

	// ── Daemonize ────────────────────────────────────────────────────────────
	// Detach before acquiring any resources so only the child runs the server
	// (AI.md PART 8 "--daemon"). The parent reports the child PID and exits 0.
	if *daemonFlag || cfg.Server.Daemonize {
		isParent, derr := daemonize()
		if derr != nil {
			log.Printf("Failed to daemonize: %v", derr)
			os.Exit(exOSErr)
		}
		if isParent {
			os.Exit(0)
		}
	}

	// ── Initialize database ──────────────────────────────────────────────────
	if err := db.Init(dataDir); err != nil {
		log.Printf("Failed to initialize database: %v", err)
		os.Exit(exUnavailable)
	}
	defer db.Close()

	// ── First-run: generate admin credentials ─────────────────────────────
	if hasCredentials, err := db.HasAdminCredentials(); err == nil && !hasCredentials {
		adminUser := "admin"
		adminPass, err := db.GeneratePassword(20)
		if err != nil {
			log.Printf("Failed to generate admin password: %v", err)
			os.Exit(exOSErr)
		}
		rawToken, err := db.GenerateToken(32)
		if err != nil {
			log.Printf("Failed to generate API token: %v", err)
			os.Exit(exOSErr)
		}
		if err := db.SetAdminCredentials(adminUser, adminPass, rawToken); err != nil {
			log.Printf("Failed to store admin credentials: %v", err)
			os.Exit(exIOErr)
		}

		// Display ONCE — not logged, printed directly to stdout
		heading := "  Admin credentials (shown once — copy now)"
		if colorEnabled {
			heading = "\033[1;33m" + heading + "\033[0m"
		}
		fmt.Println()
		fmt.Println("══════════════════════════════════════════════════════════")
		fmt.Printf("%s\n\n", heading)
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

	// Wire the independent mode/debug axes: mode from config (env/--mode
	// already folded in above), debug from DEBUG env var, then --debug
	// CLI flag always wins last (already applied above via SetDebug).
	if err := mode.Set(cfg.Server.Mode); err != nil {
		log.Printf("Warning: invalid mode %q, defaulting to production: %v", cfg.Server.Mode, err)
	}
	mode.InitDebug()
	if *debugFlag {
		mode.SetDebug(true)
	}
	devMode := mode.IsAppModeDev()

	// ── Load templates ───────────────────────────────────────────────────────
	log.Println("Loading .gitignore templates...")
	templateMgr, err := template.New()
	if err != nil {
		log.Printf("Failed to load templates: %v", err)
		os.Exit(exOSFile)
	}
	log.Printf("Loaded %d templates", templateMgr.Count())

	// ── Signal handling ──────────────────────────────────────────────────────
	// Platform-dependent subscription (AI.md PART 8): SIGTERM/SIGINT/SIGQUIT and
	// SIGRTMIN+3 shut down gracefully, SIGUSR1 reopens logs, SIGUSR2 dumps
	// status, and SIGHUP is explicitly ignored (config auto-reloads via the file
	// watcher). See signal_unix.go / signal_windows.go.
	sigChan := make(chan os.Signal, 1)
	notifyShutdownSignals(sigChan)

	// ── Initialize GeoIP (AI.md PART 19) ─────────────────────────────────────
	// Opens any databases already on disk; missing databases fail open. The
	// manager is shared by the server (country blocking / lookups) and the
	// scheduler (weekly refresh).
	geoipMgr := newGeoIP(cfg, dataDir)

	// ── Write PID file ───────────────────────────────────────────────────────
	// Stale-aware; skipped entirely inside containers (AI.md PART 8 "PID File
	// Handling"). A live prior instance is fatal.
	if err := writePIDFile(pidFile); err != nil {
		log.Printf("%v", err)
		os.Exit(exOSErr)
	}

	// ── Start server ─────────────────────────────────────────────────────────
	pathMgr := apppath.New()
	srv := server.New(&server.Config{
		Address:   serverAddress,
		Port:      portNum,
		DevMode:   devMode,
		Templates: templateMgr,
		Paths:     pathMgr,
		Version:   Version,
		Commit:    CommitID,
		BuildDate: BuildDate,
		Cfg:       cfg,
		GeoIP:     geoipMgr,
	})

	log.Printf("gitignore %s (commit: %s, built: %s)", Version, CommitID, BuildDate)
	log.Printf("Listening on %s:%d", serverAddress, portNum)
	if devMode {
		log.Println("Development mode enabled")
	}

	errChan := make(chan error, 1)
	go func() { errChan <- srv.Start() }()

	// ── Initialize operator email notifications (AI.md PART 17) ───────────────
	// Detects or tests SMTP and sets the process-wide notifier. Best-effort: no
	// working SMTP simply leaves email disabled.
	initEmail(cfg, configDir, dataDir)

	// ── Start the always-running task scheduler (AI.md PART 18) ───────────────
	var sched *scheduler.Scheduler
	if s, err := buildScheduler(cfg, configDir, dataDir, logsDir, geoipMgr); err != nil {
		log.Printf("Failed to build scheduler: %v", err)
	} else {
		sched = s
		if err := sched.Start(context.Background()); err != nil {
			log.Printf("Failed to start scheduler: %v", err)
		}
	}

	// First-run GeoIP download (AI.md PART 19): best-effort, non-blocking.
	bootstrapGeoIP(context.Background(), geoipMgr)

	// ── Start the Tor hidden service (AI.md PART 31) ──────────────────────────
	// Best-effort and non-blocking: the server never fails to start because of
	// Tor. The manager is nil when no tor binary is installed.
	torCtx, torCancel := context.WithCancel(context.Background())
	torMgr := startTor(torCtx, cfg, configDir, dataDir, portNum)

	for {
		select {
		case err := <-errChan:
			log.Printf("Server error: %v", err)
			removePIDFile(pidFile)
			os.Exit(exUnavailable)
		case sig := <-sigChan:
			switch classifySignal(sig) {
			case sigActionReopenLogs:
				log.Println("Received SIGUSR1, reopening logs...")
				reopenLogs()
			case sigActionStatusDump:
				log.Println("Received SIGUSR2, dumping status...")
				dumpStatus()
			default:
				log.Printf("Received signal %v, shutting down...", sig)
				// Stop Tor FIRST (server owns the Tor lifecycle, AI.md PART 31).
				torCancel()
				if torMgr != nil {
					if err := torMgr.Close(); err != nil {
						log.Printf("Error stopping Tor: %v", err)
					}
				}
				if sched != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
					sched.Stop(ctx)
					cancel()
				}
				if geoipMgr != nil {
					_ = geoipMgr.Close()
				}
				removePIDFile(pidFile)
				os.Exit(0)
			}
		}
	}
}

// resolveColor determines whether color output should be enabled.
// Precedence: --color=no/yes > NO_COLOR env var > --color=auto (TTY detect).
func resolveColor(flagValue string) bool {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "yes":
		return true
	case "no":
		return false
	default:
		if _, set := os.LookupEnv("NO_COLOR"); set {
			return false
		}
		return isatty.IsTerminal(os.Stdout.Fd())
	}
}

// ensureRuntimeDir creates a runtime directory with privilege-appropriate
// permissions (AI.md PART 8 "Directory Validation Rules"): 0755 for root, 0700
// for an unprivileged user. Empty paths (e.g. a Windows PID dir) are a no-op.
func ensureRuntimeDir(path string) error {
	if path == "" {
		return nil
	}
	perm := os.FileMode(0700)
	if os.Geteuid() == 0 {
		perm = 0755
	}
	return os.MkdirAll(path, perm)
}

// normalizeBaseURL canonicalises a URL path prefix (AI.md PART 8 "--baseurl"):
// a leading slash is enforced, a trailing slash is trimmed, and an empty value
// becomes the root "/".
func normalizeBaseURL(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return p
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

func printHelp(binaryName string) {
	fmt.Printf(`%[1]s %[2]s

Usage: %[1]s [options]

Information:
  -h, --help                     Show help
  -v, --version                  Show version
  --status                       Show server status and health (exit 0 = healthy)

Shell Integration:
  --shell completions [SHELL]    Print shell completions
  --shell init [SHELL]           Print shell init command
  --shell help                   Show shell help

Server Configuration:
  --mode {production|development} Application mode (default: production)
  --config DIR                   Config directory
  --data DIR                     Data directory
  --cache DIR                    Cache directory
  --log DIR                      Log directory
  --backup DIR                   Backup directory
  --pid FILE                     PID file path
  --address ADDR                 Listen address (default: [::])
  --port PORT                    Listen port (default: random 64xxx, 80 in container)
  --baseurl PATH                 URL path prefix (default: /)
  --daemon                       Run as daemon (detach from terminal)
  --debug                        Enable debug mode
  --color {auto|yes|no}          Color output (default: auto)
  --lang CODE                    Language for output (default: auto)

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

Scheduler Commands:
  scheduler list               List all scheduled tasks and status
  scheduler show <id>          Show task details
  scheduler run <id>           Run a task immediately
  scheduler enable <id>        Enable a task
  scheduler disable <id>       Disable a task
  scheduler history <id>       Show a task's last recorded run

Email Commands:
  email test [recipient]       Send a test email to verify SMTP
  email status                 Show SMTP configuration and reachability
  email validate               Validate all email templates

Environment Variables (runtime):
  PORT       Server port
  LISTEN     Listen address
  MODE       Application mode
  DOMAIN     FQDN override
  SMTP_HOST      SMTP server host (overrides config)
  SMTP_PORT      SMTP server port (default 587)
  SMTP_USERNAME  SMTP auth username
  SMTP_PASSWORD  SMTP auth password
  SMTP_TLS       TLS mode: auto, starttls, tls, none
  SMTP_FROM_NAME  Envelope From display name
  SMTP_FROM_EMAIL Envelope From address

Environment Variables (init-only, first run):
  CONFIG_DIR    Configuration directory
  DATA_DIR      Data directory
  DATABASE_DIR  Database directory (default: {data}/db)
  CACHE_DIR     Cache directory
  LOG_DIR       Log directory
  BACKUP_DIR    Backup directory

Configuration:
  Root:    /etc/apimgr/gitignore/server.yml
  User:    ~/.config/apimgr/gitignore/server.yml
  Docker:  /config/server.yml

`, binaryName, Version)
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

func handleServiceCommand(cmd, configDir string, force bool) {
	switch cmd {
	case "start":
		if err := service.Start(); err != nil {
			log.Printf("Failed to start service: %v", err)
			os.Exit(exOSErr)
		}
	case "stop":
		if err := service.Stop(); err != nil {
			log.Printf("Failed to stop service: %v", err)
			os.Exit(exOSErr)
		}
	case "restart":
		if err := service.Restart(); err != nil {
			log.Printf("Failed to restart service: %v", err)
			os.Exit(exOSErr)
		}
	case "reload":
		if err := service.Reload(); err != nil {
			log.Printf("Failed to reload service: %v", err)
			os.Exit(exOSErr)
		}
	case "status":
		serviceStatus()
	case "--install":
		installService()
	case "--uninstall":
		if err := service.Uninstall(force); err != nil {
			log.Printf("Failed to uninstall service: %v", err)
			os.Exit(exOSErr)
		}
	case "--disable":
		serviceDisable()
	case "--help":
		fmt.Println("Service commands: start, stop, restart, reload, status, --install, --uninstall, --disable, --help")
		fmt.Println()
		serviceStatus()
	default:
		fmt.Fprintf(os.Stderr, "Unknown service command: %s\n", cmd)
		os.Exit(1)
	}
}

// installService implements the smart privilege-escalation flow required
// for `--service --install` (AI.md PART 23): already elevated -> install
// directly; else try sudo/su/pkexec/doas (or runas on Windows) in order,
// re-executing this same command line under escalation; else fall back
// to a user-level service install; else print an informative error.
func installService() {
	if service.IsElevated() {
		if err := service.Install(); err != nil {
			log.Printf("Failed to install service: %v", err)
			os.Exit(exCantCreat)
		}
		return
	}

	if method := service.DetectEscalation(); method != service.EscalateNone {
		exePath, err := os.Executable()
		if err == nil {
			fmt.Printf("Elevated privileges required to install a system service (using %s)...\n", method)
			args := append([]string{exePath}, os.Args[1:]...)
			if err := service.ExecElevated(method, args); err != nil {
				log.Printf("Failed to install service via %s: %v", method, err)
				os.Exit(exOSErr)
			}
			return
		}
	}

	fmt.Println("No privilege escalation method available (sudo/su/pkexec/doas not found).")
	fmt.Println("Falling back to a user-level service install...")
	if err := service.InstallUser(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install user-level service: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run this command as root, or install sudo/su/pkexec/doas, then retry.")
		os.Exit(exCantCreat)
	}
}

func handleMaintenanceCommand(cmd, configDir, dataDir, logsDir, configPath string) {
	args := flag.Args()

	switch cmd {
	case "backup":
		cfg, _ := config.Load(configPath)
		backupFile := ""
		if len(args) > 0 {
			backupFile = args[0]
		} else {
			backupDir := apppath.GetBackupDir()
			if err := os.MkdirAll(backupDir, 0755); err != nil {
				log.Printf("Failed to create backup directory: %v", err)
				os.Exit(exCantCreat)
			}
			timestamp := time.Now().Format("2006-01-02_150405")
			backupFile = filepath.Join(backupDir, fmt.Sprintf("gitignore_backup_%s.tar.gz", timestamp))
		}
		if err := runBackup(cfg, configDir, dataDir, backupFile); err != nil {
			log.Printf("Backup failed: %v", err)
			os.Exit(exIOErr)
		}

	case "restore":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: gitignore --maintenance restore <backup-file>")
			os.Exit(2)
		}
		if err := runRestore(args[0], configDir, dataDir); err != nil {
			log.Printf("Restore failed: %v", err)
			os.Exit(exIOErr)
		}

	case "update":
		cfg, _ := config.Load(configPath)
		maintenanceUpdate(cfg, args)

	case "mode":
		if len(args) == 0 {
			cfg, _ := config.Load(configPath)
			m := cfg.Server.Mode
			if m == "" {
				m = "production"
			}
			fmt.Printf("Current mode: %s\n", m)
			return
		}
		resolved := resolveMode(args[0])
		if resolved == "" {
			fmt.Fprintf(os.Stderr, "Invalid mode: %s\nValid: production, development (aliases: prod, dev)\n", args[0])
			os.Exit(2)
		}
		if err := config.Update(func(c *config.Config) { c.Server.Mode = resolved }); err != nil {
			log.Printf("Failed to save config: %v", err)
			os.Exit(exConfig)
		}
		fmt.Printf("Application mode set to: %s\n", resolved)

	case "setup":
		cfg, _ := config.Load(configPath)
		defaults := config.DefaultConfig()
		// Reset server configuration to defaults, preserving only the
		// identity fields that would otherwise orphan the running
		// deployment (listen port and FQDN); everything else — mode,
		// user/group, SSL, update branch, etc. — goes back to spec defaults.
		port := cfg.Server.Port
		fqdn := cfg.Server.FQDN
		if err := config.Update(func(c *config.Config) {
			c.Server = defaults.Server
			c.Server.Port = port
			c.Server.FQDN = fqdn
		}); err != nil {
			log.Printf("Failed to reset configuration: %v", err)
			os.Exit(exConfig)
		}
		fmt.Println("gitignore Setup")
		fmt.Println("===============")
		fmt.Printf("Config: %s\n", configPath)
		fmt.Printf("Port:   %s\n", port)
		fmt.Printf("Mode:   %s\n", defaults.Server.Mode)
		fmt.Println("Server configuration reset to defaults.")
		fmt.Println("Setup complete.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown maintenance command: %s\n", cmd)
		fmt.Fprintln(os.Stderr, "Available: backup, restore, update, mode, setup")
		os.Exit(1)
	}
}

func serviceStatus() {
	installed, running, enabled, pid := service.Status()

	installedStr := "not installed"
	if installed {
		installedStr = "installed"
	}
	state := "stopped"
	if running {
		state = "running"
	}
	autoStart := "disabled"
	if enabled {
		autoStart = "enabled"
	}
	pidStr := "-"
	if running && pid > 0 {
		pidStr = strconv.Itoa(pid)
	}

	fmt.Println("Current status:")
	fmt.Printf("  Service:    %s\n", installedStr)
	fmt.Printf("  State:      %s\n", state)
	fmt.Printf("  Auto-start: %s\n", autoStart)
	fmt.Printf("  PID:        %s\n", pidStr)
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

// maintenanceUpdate implements `--maintenance update [cmd]`, an alias for
// `--update [cmd]` with the same default (yes) when no subcommand is given
// (AI.md PART 22).
func maintenanceUpdate(cfg *config.Config, args []string) {
	sub := "yes"
	var subArgs []string
	if len(args) > 0 {
		sub = args[0]
		subArgs = args[1:]
	}
	handleUpdateCommand(sub, subArgs, cfg)
}

// handleUpdateCommand dispatches the --update subcommands (AI.md PART 22): the
// default and `yes` perform an in-place update; `check` reports availability
// without installing; `branch` sets the release channel in config.
func handleUpdateCommand(sub string, subArgs []string, cfg *config.Config) {
	switch sub {
	case "", "yes":
		runUpdateInstall(cfg)
	case "check":
		runUpdateCheck(cfg)
	case "branch":
		if len(subArgs) == 0 {
			fmt.Printf("Current branch: %s\n", cfg.Server.Update.Branch)
			return
		}
		if subArgs[0] != "stable" && subArgs[0] != "beta" && subArgs[0] != "daily" {
			fmt.Fprintf(os.Stderr, "Invalid branch: %s\nValid: stable, beta, daily\n", subArgs[0])
			os.Exit(exUsage)
		}
		if err := config.Update(func(c *config.Config) { c.Server.Update.Branch = subArgs[0] }); err != nil {
			log.Printf("Failed to save config: %v", err)
			os.Exit(exConfig)
		}
		fmt.Printf("Branch set to: %s\n", subArgs[0])
	default:
		fmt.Fprintf(os.Stderr, "Unknown update command: %s\n", sub)
		os.Exit(exUsage)
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
