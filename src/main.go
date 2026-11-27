package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/apimgr/gitignore/src/blocklist"
	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/common/banner"
	"github.com/apimgr/gitignore/src/common/i18n"
	commonlogging "github.com/apimgr/gitignore/src/common/logging"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/cve"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/diskcheck"
	"github.com/apimgr/gitignore/src/mode"
	"github.com/apimgr/gitignore/src/notify"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/scheduler"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/service"
	"github.com/apimgr/gitignore/src/template"
	"github.com/apimgr/gitignore/src/tor"
)

// Version information (set by build flags)
var (
	Version    = "dev"
	CommitID   = "unknown"
	BuildDate  = "N/A"
	BuildEpoch = "0"
	// OfficialSite is the official hosted instance URL, embedded from site.txt
	// or the OFFICIAL_SITE build arg (AI.md PART 25). Empty for self-hosted.
	OfficialSite = ""
)

const (
	projectName  = "gitignore"
	internalOrg  = "apimgr"
	internalName = "gitignore"
)

var outputLanguage = i18n.DefaultLang

func cliText(key string, values ...interface{}) string {
	return i18n.TranslateFormat(outputLanguage, key, values...)
}

func setOutputLanguage(flagLanguage, configLanguage string) {
	outputLanguage = i18n.ResolveCLILang(flagLanguage, configLanguage)
	service.SetLanguage(outputLanguage)
	config.SetLanguage(outputLanguage)
}

func resolveSecurityDataDir(value, dataDir string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return filepath.Join(dataDir, "security")
	}
	value = strings.ReplaceAll(value, "${DATA_DIR}", dataDir)
	value = strings.ReplaceAll(value, "$DATA_DIR", dataDir)
	value = strings.ReplaceAll(value, "{data_dir}", dataDir)
	if !filepath.IsAbs(value) {
		value = filepath.Join(dataDir, value)
	}
	return filepath.Clean(value)
}

func makeBlocklistConfig(cfg *config.Config, securityDir string) blocklist.Config {
	sources := make([]blocklist.Source, 0, len(cfg.Server.Security.Blocklists.ExtraSources))
	for _, source := range cfg.Server.Security.Blocklists.ExtraSources {
		sources = append(sources, blocklist.Source{ID: source.ID, Type: source.Type, URL: source.URL, Enabled: source.Enabled})
	}
	trusted := make([]netip.Prefix, 0, len(cfg.Server.TrustedProxies.Additional))
	for _, value := range cfg.Server.TrustedProxies.Additional {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			trusted = append(trusted, prefix.Masked())
			continue
		}
		if address, err := netip.ParseAddr(value); err == nil {
			bits := 128
			if address.Is4() {
				bits = 32
			}
			trusted = append(trusted, netip.PrefixFrom(address, bits))
		}
	}
	return blocklist.Config{SecurityDir: securityDir, Enabled: cfg.Server.Security.Blocklists.Enabled, ExtraSources: sources, TrustedPrefixes: trusted}
}

func writeWebhookAudit(logsDir, event string, fields map[string]string) {
	details := make(map[string]any, len(fields))
	for key, value := range fields {
		details[key] = value
	}
	_ = audit.Append(logsDir, audit.Entry{
		Event: event, Category: "system", Severity: "warn",
		Actor:   audit.Actor{Type: "system"},
		Details: details, Result: "failure",
	})
}

func parseDurationOr(value string, fallback time.Duration) time.Duration {
	if duration, err := time.ParseDuration(strings.TrimSpace(value)); err == nil && duration > 0 {
		return duration
	}
	return fallback
}

func databaseConnectionConfig(cfg *config.Config) db.ConnectionConfig {
	if cfg == nil {
		return db.ConnectionConfig{Driver: "sqlite"}
	}
	pool := cfg.Server.Database.Pool
	return db.ConnectionConfig{
		Driver:        cfg.Server.Database.Driver,
		URL:           cfg.Server.Database.URL,
		Token:         cfg.Server.Database.Token,
		LegacySecrets: cfg.LegacySecrets,
		Pool: db.PoolConfig{
			MaxOpen:     pool.MaxOpen,
			MaxIdle:     pool.MaxIdle,
			MaxLifetime: parseDurationOr(pool.MaxLifetime, 5*time.Minute),
			MaxIdleTime: parseDurationOr(pool.MaxIdleTime, time.Minute),
		},
		Cache: db.CacheConfig{
			Enabled:      cfg.Server.Database.Cache.Enabled,
			Path:         cfg.Server.Database.Cache.Path,
			SyncInterval: parseDurationOr(cfg.Server.Database.Cache.SyncInterval, 30*time.Second),
			OfflineMode:  cfg.Server.Database.Cache.OfflineMode,
			MaxAge:       parseDurationOr(cfg.Server.Database.Cache.MaxAge, time.Hour),
		},
	}
}

// Exit codes follow the project contract; usage errors use status 2.
const (
	// exUsage is the command-line usage error status.
	exUsage = 2
	// exUnavailable means a service is unavailable.
	exUnavailable = 69
	// exOSErr means an operating system error occurred.
	exOSErr = 71
	// exOSFile means an application data file is missing or unreadable.
	exOSFile = 72
	// exCantCreat means an output file could not be created.
	exCantCreat = 73
	// exIOErr means an I/O error occurred.
	exIOErr = 74
	// exTempFail requests a supervisor restart after staging an update.
	exTempFail = 75
	exConfig   = 78 // configuration error
)

func init() {
	if epoch := buildEpoch(); epoch > 0 {
		BuildDate = buildDate(epoch)
	}
	log.SetPrefix("gitignore: ")
	log.SetFlags(log.Lshortfile)
}

func buildDate(epoch int64) string {
	return time.Unix(epoch, 0).UTC().Format(time.RFC3339)
}

func buildEpoch() int64 {
	epoch, err := strconv.ParseInt(BuildEpoch, 10, 64)
	if err != nil {
		return 0
	}
	return epoch
}

func main() {
	dirs := apppath.GetDirectories()
	maintenanceState := &server.MaintenanceState{}
	maintenanceWake := make(chan struct{}, 1)
	var databaseAvailable atomic.Bool
	databaseAvailable.Store(true)
	db.SetCriticalErrorHandler(func(error) {
		databaseAvailable.Store(false)
		maintenanceState.Enter("database_write", "Database write failed", time.Now().UTC())
		select {
		case maintenanceWake <- struct{}{}:
		default:
		}
	})

	// binaryName is the actual invoked binary name, shown in --help/--version
	// output (AI.md PART 8). Internal identifiers (service unit, config paths,
	// User-Agent) keep the hardcoded projectName instead.
	binaryName := filepath.Base(os.Args[0])
	flag.CommandLine = flag.NewFlagSet(binaryName, flag.ContinueOnError)

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
	modeFlag := flag.String("mode", "", "Application mode: production|development|debug (aliases: prod|dev|devel)")

	// Service commands
	serviceCmd := flag.String("service", "", "Service commands: start, stop, restart, reload, --install, --uninstall, --disable, --help")

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", "Maintenance commands: backup, restore, update, mode, setup, token, data, compliance")

	// --yes is a command modifier for operations that explicitly support skipping confirmation.
	forceFlag := flag.Bool("yes", false, "Skip supported confirmation prompts")

	// Update commands
	updateCmd := flag.String("update", "", "Update commands: check, yes, branch {stable|beta|devel}")

	// Directory and runtime path flags (AI.md PART 8 "Directory Flags"). Each
	// directory flag creates its target if missing; empty means "use the env
	// override or the OS default".
	dataDirFlag := flag.String("data", "", "Data directory")
	cacheDirFlag := flag.String("cache", "", "Cache directory")
	logDirFlag := flag.String("log", "", "Log directory")
	backupDirFlag := flag.String("backup", "", "Backup directory")
	pidFileFlag := flag.String("pid", "", "PID file path")
	baseURLFlag := flag.String("baseurl", "", "URL path prefix (default: /)")
	i2pFlag := flag.Bool("i2p", false, "Enable the I2P eepsite (default on in containers)")
	daemonFlag := flag.Bool("daemon", false, "Run as daemon (detach from terminal)")
	langFlag := flag.String("lang", "", "Language for output (default: auto, from LANG env)")
	shellCmd := flag.String("shell", "", "Shell integration: completions, init, help [SHELL]")
	flag.Usage = func() { printHelp(binaryName) }
	flag.CommandLine.SetOutput(io.Discard)

	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		setOutputLanguage(*langFlag, "")
		fmt.Fprintln(os.Stderr, cliText("cli.main.invalid_command_line"))
		os.Exit(2)
	}
	debugFlagSpecified := false
	flag.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "debug" {
			debugFlagSpecified = true
		}
	})
	setOutputLanguage(*langFlag, "")

	if *showHelp {
		printHelp(binaryName)
		return
	}

	if *showVersion {
		fmt.Printf("%s\n%s\n%s\n%s\n",
			cliText("version.name_version", "project_name", binaryName, "project_version", Version),
			cliText("version.built", "build_date", BuildDate),
			cliText("version.go", "go_version", runtime.Version()),
			cliText("version.os_arch", "goos", runtime.GOOS, "goarch", runtime.GOARCH))
		return
	}

	// --shell prints completions/init and exits immediately (AI.md PART 8). The
	// subcommand is the flag value; an optional SHELL follows as a positional.
	if *shellCmd != "" {
		os.Exit(handleShell(append([]string{*shellCmd}, flag.Args()...)))
	}

	colorEnabled := resolveColor(*colorFlag)

	if *debugFlag {
		// --debug sets the independent debug axis only; it does NOT
		// force development mode (mode and debug are tracked independently)
		mode.SetDebug(true)
		log.SetFlags(log.Lshortfile | log.LstdFlags)
		log.Println(cliText("cli.main.debug_enabled"))
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
		if err := os.Setenv("DATA_DIR", dataDir); err != nil {
			log.Printf("%s", cliText("cli.main.set_data_directory_failed", "error", err))
			os.Exit(exOSErr)
		}
	} else if v := os.Getenv("DATA_DIR"); v != "" {
		dataDir = v
	}
	// Resolve log directory (flag > LOG_DIR env > OS default).
	logsDir := dirs.Logs
	if *logDirFlag != "" {
		logsDir = *logDirFlag
		if err := os.Setenv("LOG_DIR", logsDir); err != nil {
			log.Printf("%s", cliText("cli.main.set_log_directory_failed", "error", err))
			os.Exit(exOSErr)
		}
	} else if v := os.Getenv("LOG_DIR"); v != "" {
		logsDir = v
	}

	// --cache and --backup export their init-only env vars so GetCacheDir and
	// GetBackupDir resolve the override wherever they are called.
	if *cacheDirFlag != "" {
		if err := os.Setenv("CACHE_DIR", *cacheDirFlag); err != nil {
			log.Printf("%s", cliText("cli.main.set_cache_directory_failed", "error", err))
			os.Exit(exOSErr)
		}
	}
	if *backupDirFlag != "" {
		if err := os.Setenv("BACKUP_DIR", *backupDirFlag); err != nil {
			log.Printf("%s", cliText("cli.main.set_backup_directory_failed", "error", err))
			os.Exit(exOSErr)
		}
	}
	for _, directory := range []struct {
		name      string
		value     *string
		env       string
		setErrKey string
	}{
		{"config", &configDir, "", ""},
		{"data", &dataDir, "DATA_DIR", "cli.main.set_data_directory_failed"},
		{"logs", &logsDir, "LOG_DIR", "cli.main.set_log_directory_failed"},
	} {
		validated, err := apppath.SafeDirPath(*directory.value)
		if err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.invalid_directory_path", "path", directory.name))
			os.Exit(exUsage)
		}
		*directory.value = validated
		if directory.env != "" {
			if err := os.Setenv(directory.env, validated); err != nil {
				log.Printf("%s", cliText(directory.setErrKey, "error", err))
				os.Exit(exOSErr)
			}
		}
	}
	databaseDir, err := apppath.SafeDirPath(apppath.GetDatabaseDir(dataDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.main.invalid_directory_path", "path", "database"))
		os.Exit(exUsage)
	}
	if err := os.Setenv("DATABASE_DIR", databaseDir); err != nil {
		log.Printf("%s", cliText("cli.main.set_database_directory_failed", "error", err))
		os.Exit(exOSErr)
	}
	for _, directory := range []struct {
		name      string
		key       string
		path      string
		setErrKey string
	}{
		{"cache", "CACHE_DIR", apppath.GetCacheDir(), "cli.main.set_cache_directory_failed"},
		{"backup", "BACKUP_DIR", apppath.GetBackupDir(), "cli.main.set_backup_directory_failed"},
	} {
		validated, err := apppath.SafeDirPath(directory.path)
		if err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.invalid_directory_path", "path", directory.name))
			os.Exit(exUsage)
		}
		if err := os.Setenv(directory.key, validated); err != nil {
			log.Printf("%s", cliText(directory.setErrKey, "error", err))
			os.Exit(exOSErr)
		}
	}

	// Resolve PID file path (flag > environment > OS default; empty on Windows).
	pidFile := apppath.GetPIDFile()
	if *pidFileFlag != "" {
		pidFile = *pidFileFlag
	} else if envPIDFile := strings.TrimSpace(os.Getenv("PID_FILE")); envPIDFile != "" {
		pidFile = envPIDFile
	}
	if pidFile != "" {
		validated, err := apppath.SafeDirPath(pidFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.Translate(outputLanguage, "errors.invalid_path_argument"))
			os.Exit(exUsage)
		}
		pidFile = validated
	}
	for _, dir := range []string{configDir, dataDir} {
		if err := ensureRuntimeDir(dir); err != nil {
			log.Printf("ERROR required private-path parent is unavailable: %s", dir)
			os.Exit(exUnavailable)
		}
	}
	if err := apppath.EnforcePrivate(configDir, dataDir); err != nil {
		log.Printf("private path enforcement failed: %v", err)
		os.Exit(exUnavailable)
	}

	if os.Geteuid() == 0 {
		if err := service.EnsureSystemUser(dataDir); err != nil {
			log.Printf("failed to prepare the service account: %v", err)
			os.Exit(exUnavailable)
		}
	}
	// Ensure all runtime directories exist with privilege-appropriate perms
	// (AI.md PART 8 "Directory Validation Rules"): 0755 root / 0700 user.
	for _, d := range []string{configDir, dataDir, apppath.GetCacheDir(), logsDir, apppath.GetBackupDir(), os.Getenv("DATABASE_DIR")} {
		if err := ensureRuntimeDir(d); err != nil {
			log.Printf("%s", cliText("cli.main.create_directory_warning", "path", d, "error", err))
			maintenanceState.Enter("file_write", "Required file storage is unavailable", time.Now().UTC())
		} else if err := probeRuntimeDir(d); err != nil {
			log.Printf("required runtime directory is not writable: %s", d)
			maintenanceState.Enter("file_write", "Required file storage is unavailable", time.Now().UTC())
		}
	}

	// Load configuration (auto-creates with random 64xxx port on first run)
	configPath := filepath.Join(configDir, "server.yml")
	_, configStatErr := os.Stat(configPath)
	firstRun := errors.Is(configStatErr, os.ErrNotExist)
	startupSettings := config.PeekStartupSettings(configPath)
	config.SetAuditLogDir(logsDir)
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("%s", cliText("cli.main.load_config_warning", "error", err))
		cfg = config.DefaultConfig()
	}
	if !startupSettings.PIDFile && *pidFileFlag == "" && strings.TrimSpace(os.Getenv("PID_FILE")) == "" {
		pidFile = ""
	}
	setOutputLanguage(*langFlag, cfg.Server.I18n.DefaultLanguage)
	flag.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "i2p" {
			cfg.Server.I2P.Enabled = *i2pFlag
		}
	})
	i2pRuntime := newI2PRuntime(cfg.Server.I2P.Enabled)

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
		checkPort = strings.Split(checkPort, ",")[0]
		if err := checkHealth(checkPort, cfg.Server.BaseURL); err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.health_check_failed", "error", err))
			os.Exit(1)
		}
		fmt.Println(cliText("cli.main.health_check_ok"))
		if addr := tor.ReadHostname(dataDir); addr != "" {
			fmt.Println(cliText("cli.main.tor_hidden_service_connected"))
			fmt.Printf("  %s\n", cliText("cli.main.address", "address", addr))
		}
		os.Exit(0)
	}

	// Handle --mode flag (with shortcuts)
	if *modeFlag != "" {
		resolved := resolveMode(*modeFlag)
		if resolved == "" {
			fmt.Fprintln(os.Stderr, cliText("cli.main.invalid_mode", "mode", *modeFlag))
			os.Exit(2)
		}
		cfg.Server.Mode = resolved
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
		handleMaintenanceCommand(*maintenanceCmd, configDir, dataDir, logsDir, configPath, *forceFlag)
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
	if *daemonFlag || startupSettings.Daemonize {
		isParent, derr := daemonize()
		if derr != nil {
			log.Printf("%s", cliText("cli.main.daemonize_failed", "error", derr))
			os.Exit(exOSErr)
		}
		if isParent {
			os.Exit(0)
		}
	}

	// ── Initialize database ──────────────────────────────────────────────────
	initializeDatabase := func() error {
		if err := db.Init(dataDir, databaseConnectionConfig(cfg)); err != nil {
			return err
		}
		if len(cfg.LegacySecrets) > 0 {
			if err := config.CompleteLegacySecretMigration(configPath); err != nil {
				log.Printf("could not remove imported legacy secret settings from configuration; backup and database copies remain available")
			} else {
				cfg.LegacySecrets = nil
			}
		}
		pendingEncryptionKey, needsEncryptionKeyPersistence, err := db.PendingEncryptionKeyRotation(cfg.Server.Security.EncryptionKey)
		if err != nil {
			return fmt.Errorf("recover encryption key rotation: %w", err)
		}
		if needsEncryptionKeyPersistence {
			if _, err := config.RotateEncryptionKey(configPath, pendingEncryptionKey); err != nil {
				return fmt.Errorf("persist recovered encryption key rotation: %w", err)
			}
			cfg.Server.Security.EncryptionKey = pendingEncryptionKey
		}
		if err := db.CompleteEncryptionKeyRotation(cfg.Server.Security.EncryptionKey); err != nil {
			return fmt.Errorf("finish encryption key rotation: %w", err)
		}
		if err := reconcilePGPPrivateKey(configDir); err != nil {
			return fmt.Errorf("finish installation secret rotation: %w", err)
		}
		if err := audit.SetDatabaseSink(db.AppendAuditEntry); err != nil {
			return fmt.Errorf("persist buffered audit events: %w", err)
		}
		return nil
	}
	if err := initializeDatabase(); err != nil {
		databaseAvailable.Store(false)
		maintenanceState.Enter("database_connection", "Database connection failed", time.Now().UTC())
		log.Printf("database initialization failed; entering maintenance mode: %v", err)
	}
	defer func() { _ = db.Close() }()
	configureRateLimitCache(cfg.Server.Cache, configDir)
	if databaseAvailable.Load() {
		if _, err := applyBackupRetention(apppath.GetBackupDir(), cfg.Server.Backup.Retention, cfg.Server.Backup.EncryptionPassword); err != nil {
			log.Printf("backup retention cleanup failed: %v", err)
		}
		if _, integrityErr := audit.VerifyAndReport(logsDir); integrityErr != nil {
			log.Printf("audit chain verification found a broken entry: %v", integrityErr)
		}
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
	if normalized, ok := config.NormalizePort(serverPort); ok {
		serverPort = normalized
	} else {
		log.Printf("invalid server port; selecting a valid default")
		serverPort = config.DefaultPort()
		if err := config.Update(func(current *config.Config) { current.Server.Port = serverPort }); err != nil {
			log.Printf("could not persist the selected server port")
		}
	}
	cfg.Server.Address = serverAddress
	cfg.Server.Port = serverPort
	portParts := strings.Split(serverPort, ",")
	portNum, _ := strconv.Atoi(strings.TrimSpace(portParts[0]))
	httpsPort := 0
	if len(portParts) == 2 {
		httpsPort, _ = strconv.Atoi(strings.TrimSpace(portParts[1]))
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 && (portNum < 1024 || httpsPort > 0 && httpsPort < 1024) {
		if method := service.DetectEscalation(); method != service.EscalateNone && confirmPortEscalation(*forceFlag) {
			executable, err := os.Executable()
			if err == nil {
				args := append([]string{executable}, os.Args[1:]...)
				args = append(args,
					"--config", configDir,
					"--data", dataDir,
					"--cache", apppath.GetCacheDir(),
					"--log", logsDir,
					"--backup", apppath.GetBackupDir(),
					"--address", serverAddress,
					"--port", serverPort,
				)
				if pidFile != "" {
					args = append(args, "--pid", pidFile)
				}
				if cfg.Server.BaseURL != "" {
					args = append(args, "--baseurl", cfg.Server.BaseURL)
				}
				if cfg.Server.Mode != "" {
					args = append(args, "--mode", cfg.Server.Mode)
				}
				if err := service.ExecElevated(method, args); err != nil {
					log.Printf("privilege escalation failed")
					os.Exit(exUnavailable)
				} else {
					os.Exit(0)
				}
			}
		}
		log.Printf("privileged port unavailable; selecting an unprivileged default port")
		serverPort = config.UnprivilegedDefaultPort()
		portNum, _ = strconv.Atoi(serverPort)
		httpsPort = 0
		if err := config.Update(func(current *config.Config) { current.Server.Port = serverPort }); err != nil {
			log.Printf("could not persist the selected server port")
		}
	}
	if httpsPort == 0 && portNum == 443 {
		cfg.Server.SSL.Enabled = true
	}
	cfg.Server.Port = serverPort

	// Apply mode from environment if set
	if envMode := os.Getenv("MODE"); envMode != "" && *modeFlag == "" {
		if resolved := resolveMode(envMode); resolved != "" {
			cfg.Server.Mode = resolved
		}
	}

	// Wire the independent mode/debug axes: mode from config (env/--mode
	// already folded in above), debug from DEBUG env var, then --debug
	// CLI flag always wins last (already applied above via SetDebug).
	if err := mode.Set(cfg.Server.Mode); err != nil {
		log.Printf("%s", cliText("cli.main.invalid_config_mode", "mode", cfg.Server.Mode, "error", err))
	}
	mode.InitDebug()
	if debugFlagSpecified {
		mode.SetDebug(*debugFlag)
	}
	mode.SetProfilingRates(cfg.Server.Debug.BlockProfileRate, cfg.Server.Debug.MutexProfileFraction)
	if _, err := openApplicationLogs(logsDir, cfg.Server.Logging, mode.Get(), mode.IsDebugEnabled()); err != nil {
		log.Printf("application logging initialization failed: %v", err)
		os.Exit(exUnavailable)
	}
	devMode := mode.IsAppModeDev()

	// ── Load templates ───────────────────────────────────────────────────────
	log.Println(cliText("cli.main.loading_templates"))
	templateMgr, err := template.New()
	if err != nil {
		log.Printf("%s", cliText("cli.main.load_templates_failed", "error", err))
		os.Exit(exOSFile)
	}
	log.Printf("%s", cliText("cli.main.templates_loaded", "count", templateMgr.Count()))

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
		log.Printf("%s", cliText("cli.main.write_pid_file_failed", "error", err))
		os.Exit(exOSErr)
	}

	// ── Start server ─────────────────────────────────────────────────────────
	pathMgr := apppath.New()
	pathMgr.SetConfigDir(configDir)
	pathMgr.SetDataDir(dataDir)
	pathMgr.SetLogsDir(logsDir)
	securityDir := resolveSecurityDataDir(cfg.Data.SecurityDir, dataDir)
	blocklistCfg := makeBlocklistConfig(cfg, securityDir)
	var blocklistMatcher *blocklist.Matcher
	if cfg.Server.Security.Blocklists.Enabled {
		blocklistMatcher = blocklist.NewMatcher(securityDir, blocklistCfg.ExtraSources)
		if err := blocklistMatcher.Reload(time.Now().UTC()); err != nil {
			log.Printf("%s", cliText("cli.main.blocklist_load_failed", "error", err))
		}
	}
	var srv *server.Server
	cfgManager, managerErr := config.NewConfigManager(config.ConfigManagerOptions{
		ConfigPath: configPath,
		AssetsDir:  filepath.Join(configDir, "assets"),
		Initial:    cfg,
		Load: func() (*config.Config, error) {
			next, err := config.Load(configPath)
			if err != nil {
				return nil, err
			}
			if *port != "" {
				next.Server.Port = *port
			} else if value := os.Getenv("PORT"); value != "" {
				next.Server.Port = value
			}
			if *address != "" {
				next.Server.Address = *address
			} else if value := os.Getenv("LISTEN"); value != "" {
				next.Server.Address = value
			}
			if *baseURLFlag != "" {
				next.Server.BaseURL = normalizeBaseURL(*baseURLFlag)
			} else {
				next.Server.BaseURL = normalizeBaseURL(next.Server.BaseURL)
			}
			if envMode := os.Getenv("MODE"); envMode != "" && next.Server.Mode == "production" {
				next.Server.Mode = resolveMode(envMode)
			}
			flag.Visit(func(parsed *flag.Flag) {
				if parsed.Name == "i2p" {
					next.Server.I2P.Enabled = *i2pFlag
				}
			})
			return next, nil
		},
		Apply: func(next *config.Config, changes config.ConfigChanges) error {
			if srv == nil {
				return fmt.Errorf("%s", cliText("cli.main.server_not_ready"))
			}
			srv.ReplaceConfig(next)
			if len(changes.GracefulReload) > 0 {
				reopenLogs()
			}
			if len(changes.HotReload) > 0 || len(changes.GracefulReload) > 0 {
				log.Printf("%s", cliText("cli.main.configuration_reloaded", "changes", strings.Join(append(changes.HotReload, changes.GracefulReload...), ", ")))
			}
			if len(changes.RestartRequired) > 0 {
				log.Printf("%s", cliText("cli.main.configuration_restart_required", "changes", strings.Join(changes.RestartRequired, ", ")))
			}
			return nil
		},
		OnError: func(error) { log.Println(cliText("cli.main.configuration_reload_failed")) },
	})
	if managerErr != nil {
		log.Printf("%s", cliText("cli.main.configuration_watcher_unavailable", "error", managerErr))
	}
	webhookDispatcher := notify.NewDispatcher(notify.Options{
		UserAgent: projectName + "/" + Version,
		Audit: func(event string, fields map[string]string) {
			writeWebhookAudit(logsDir, event, fields)
		},
	})
	startedElevated := server.ProcessStartedElevated()
	var windowsServiceReady func()
	serverReady := make(chan struct{})
	torCtx, torCancel := context.WithCancel(context.Background())
	i2pCtx, i2pCancel := context.WithCancel(context.Background())
	var torMgr *tor.TorManager
	srv = server.New(&server.Config{
		Address:          serverAddress,
		Port:             portNum,
		HTTPSPort:        httpsPort,
		DevMode:          devMode,
		Templates:        templateMgr,
		Paths:            pathMgr,
		Version:          Version,
		Commit:           CommitID,
		BuildDate:        BuildDate,
		Cfg:              cfg,
		ConfigManager:    cfgManager,
		GeoIP:            geoipMgr,
		Blocklist:        blocklistMatcher,
		SecurityDir:      securityDir,
		MaintenanceState: maintenanceState,
		BeforePrivilegeDrop: func() error {
			return applyStagedUpdate(cfg, dataDir, logsDir, startedElevated)
		},
		BeforeServe: func() {
			torMgr = startTor(torCtx, cfg, configDir, dataDir, portNum, srv)
			go i2pRuntime.run(i2pCtx, cfg.Server.I2P, configDir, dataDir, logsDir, srv.HTTPHandler())
		},
		OnReady: func() {
			close(serverReady)
			if windowsServiceReady != nil {
				windowsServiceReady()
			}
			emitLifecycleEmail("startup", "", 0)
		},
		AccessLog: func(entry server.AccessLogEntry) {
			manager := activeApplicationLogs.Load()
			if manager == nil {
				return
			}
			_ = manager.Access(commonlogging.AccessEntry{
				Time: entry.Time, RemoteIP: entry.RemoteIP, Method: entry.Method,
				Path: entry.Path, Query: entry.Query, Status: entry.Status,
				Bytes: entry.Bytes, Latency: entry.Latency, UserAgent: entry.UserAgent,
				Referer: entry.Referer, RequestID: entry.RequestID, Host: entry.Host,
				Protocol: entry.Protocol, TLSVersion: entry.TLSVersion,
				Country: entry.Country, ASN: entry.ASN,
			})
			healthFailure := entry.Method == http.MethodGet && (strings.HasSuffix(entry.Path, "/healthz") || strings.HasSuffix(entry.Path, "/healthz.txt")) && (entry.Status < http.StatusOK || entry.Status >= http.StatusMultipleChoices)
			if entry.Status >= http.StatusInternalServerError || healthFailure {
				log.Printf("ERROR HTTP request failed: method=%s path=%s status=%d", entry.Method, entry.Path, entry.Status)
			}
		},
		I2PStatus:         i2pRuntime.healthInfo,
		WebhookDispatcher: webhookDispatcher,
		SendContact: func(to, subject, body string) error {
			if notifier == nil || notifier.sender == nil || !notifier.sender.CanSend() {
				return fmt.Errorf("%s", cliText("cli.main.email_delivery_not_configured"))
			}
			return notifier.sender.Send(to, subject, body)
		},
		SendContactReply: func(to, replyTo, subject, body string) error {
			if notifier == nil || notifier.sender == nil || !notifier.sender.CanSend() {
				return fmt.Errorf("%s", cliText("cli.main.email_delivery_not_configured"))
			}
			return notifier.sender.SendWithReplyTo(to, replyTo, subject, body)
		},
	})
	if cfgManager != nil {
		cfgManager.Start(context.Background())
	}

	log.Printf("%s", cliText("cli.main.server_version", "version", Version, "commit", CommitID, "build_date", BuildDate))
	log.Printf("%s", cliText("cli.main.listening", "address", serverAddress, "port", portNum))
	if devMode {
		log.Println(cliText("cli.main.development_mode_enabled"))
	}
	ownershipRoots := []string{configDir, dataDir, apppath.GetCacheDir(), logsDir, apppath.GetBackupDir(), databaseDir}
	if db.IsSQLite() {
		ownershipRoots = append(ownershipRoots, filepath.Dir(db.LocalDatabasePath(dataDir)))
	}
	if cfg.Server.Database.Cache.Enabled {
		ownershipRoots = append(ownershipRoots, filepath.Dir(db.LocalCachePath(dataDir)))
	}
	if err := server.SetRuntimeOwnership("gitignore", "gitignore", ownershipRoots...); err != nil {
		log.Printf("failed to set runtime directory ownership: %v", err)
		os.Exit(exUnavailable)
	}

	// ── Initialize operator email notifications (AI.md PART 17) ───────────────
	// Detects or tests SMTP and sets the process-wide notifier. Best-effort: no
	// working SMTP simply leaves email disabled.
	initEmail(cfg, configDir, dataDir, i2pRuntime.healthInfo)
	maintenanceObserver := func(event string, snapshot server.MaintenanceSnapshot) {
		severity, result := "warning", "failure"
		if event == "server.maintenance_recovered" {
			severity, result = "info", "success"
		}
		if err := audit.Append(logsDir, audit.Entry{
			Event: event, Category: "system", Severity: severity,
			Actor:   audit.Actor{Type: "system"},
			Details: map[string]any{"reason": snapshot.Reason, "attempts": snapshot.Attempts},
			Result:  result,
		}); err != nil {
			log.Printf("could not record maintenance transition: event=%s", event)
		}
		shouldNotify := event == "server.maintenance_entered" && cfg.Server.Maintenance.Notify.OnEnter ||
			event == "server.maintenance_recovered" && cfg.Server.Maintenance.Notify.OnExit ||
			event == "server.self_healing_exhausted"
		if shouldNotify && notifier != nil && notifier.sender != nil && notifier.sender.CanSend() && notifier.recipient != "" {
			subject := "Service maintenance status: " + snapshot.Reason
			body := "The service entered read-only maintenance mode. Reason: " + snapshot.Reason + "."
			if event == "server.maintenance_recovered" {
				body = "The service recovered from read-only maintenance mode. Reason: " + snapshot.Reason + ". Duration: " + time.Since(snapshot.Since).Round(time.Second).String() + "."
			} else if event == "server.self_healing_exhausted" {
				body = "Automatic recovery stopped after the configured retry limit. Fix the underlying issue and restart the service."
			}
			if err := notifier.sender.Send(notifier.recipient, subject, body); err != nil {
				log.Printf("maintenance notification delivery failed: event=%s", event)
			}
		}
	}
	maintenanceState.SetObserver(maintenanceObserver)
	if snapshot := maintenanceState.Snapshot(); snapshot.Active {
		maintenanceObserver("server.maintenance_entered", snapshot)
	}
	errChan := make(chan error, 1)
	windowsService, serviceDetectionErr := service.IsWindowsService()
	if serviceDetectionErr != nil {
		log.Printf("Windows service detection failed")
		os.Exit(exUnavailable)
	}
	if windowsService {
		go func() {
			handled, serviceErr := service.RunAsWindowsService(func(ctx context.Context, ready func()) error {
				windowsServiceReady = ready
				go func() {
					<-ctx.Done()
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_ = srv.Shutdown(shutdownCtx)
				}()
				return srv.Start()
			})
			if handled {
				errChan <- serviceErr
			}
		}()
	} else {
		go func() { errChan <- srv.Start() }()
	}
	srv.SetAbuseAlertHandler(emitAbuseSecurityAlert)

	// First-run GeoIP download (AI.md PART 19): best-effort, non-blocking.
	bootstrapGeoIP(context.Background(), geoipMgr, srv.RefreshHealth)

	// ── Start the Tor hidden service (AI.md PART 31) ──────────────────────────
	// Best-effort and non-blocking: the server never fails to start because of
	// Tor. The manager is nil when no tor binary is installed.
	readyForBanner := false
	select {
	case <-serverReady:
		readyForBanner = true
	case earlyErr := <-errChan:
		errChan <- earlyErr
	case <-time.After(30 * time.Second):
		log.Printf("WARN server did not become ready in time for startup banner")
	}
	if readyForBanner && !*daemonFlag && !startupSettings.Daemonize {
		var onionAddress, i2pAddress string
		deadline := time.Now().Add(5 * time.Second)
		for {
			if torMgr != nil {
				onionAddress = tor.ReadHostname(dataDir)
			}
			if cfg.Server.I2P.Enabled {
				i2pAddress = i2pRuntime.healthInfo().Hostname
			}
			if ((onionAddress != "" || torMgr == nil) && (i2pAddress != "" || !cfg.Server.I2P.Enabled)) || time.Now().After(deadline) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		fqdn := resolveStartupFQDN()
		if fqdn == "" {
			log.Printf("WARN startup FQDN could not be resolved; omitting public URL from banner")
		}
		urls := startupURLs(fqdn, serverPort, cfg.Server.BaseURL, cfg.Server.APIVersion, cfg.Server.SSL.Enabled)
		listenURL := ""
		if len(urls) > 0 {
			listenURL = urls[0]
		}
		now := time.Now()
		location, err := time.LoadLocation(startupTimezone())
		if err != nil {
			location, _ = time.LoadLocation("America/New_York")
		}
		if location != nil {
			now = now.In(location)
		}
		banner.PrintStartupBanner(banner.BannerConfig{
			AppName: projectName, Version: Version, AppMode: mode.Get().String(), Debug: devMode,
			URLs: urls, ListenURL: listenURL, StartupTime: now, FirstRun: firstRun,
			TorURL: overlayURL(onionAddress), I2PURL: overlayURL(i2pAddress), Plain: !colorEnabled,
			SMTPAddress: cfg.Server.Notifications.Email.SMTP.Host, SMTPPort: cfg.Server.Notifications.Email.SMTP.Port,
		})
	}

	// Security databases download on first run and refresh on their scheduled
	// cadence. These bootstrap updates never block the HTTP listener.
	go func() {
		if _, err := blocklist.Update(context.Background(), blocklistCfg); err != nil {
			log.Printf("%s", cliText("cli.main.blocklist_bootstrap_update_failed", "error", err))
		}
		if blocklistMatcher != nil {
			if err := blocklistMatcher.Reload(time.Now().UTC()); err != nil {
				log.Printf("%s", cliText("cli.main.blocklist_bootstrap_reload_failed", "error", err))
			}
		}
		_ = srv.RefreshHealth()
	}()
	go func() {
		if err := cve.Update(context.Background(), cve.Config{
			SecurityDir: securityDir, NVDSource: cfg.Data.CVE.Source, NVDAPIKey: cfg.Data.CVE.APIKey,
			TrivyEnabled: cfg.Data.Trivy.Enabled, TrivySource: cfg.Data.Trivy.Source,
		}); err != nil {
			log.Printf("%s", cliText("cli.main.cve_bootstrap_update_failed", "error", err))
		}
		_ = srv.RefreshHealth()
	}()

	checker := diskcheck.New(diskcheck.Config{
		DataDir: dataDir, LogDir: logsDir, BackupDir: apppath.GetBackupDir(),
		Enabled:          cfg.Server.Notifications.DiskLow.Enabled,
		ThresholdPercent: cfg.Server.Notifications.DiskLow.ThresholdPercent,
		Repeat:           parseDurationOr(cfg.Server.Notifications.DiskLow.Repeat, 24*time.Hour),
		Notify: func(event diskcheck.Event) {
			emitDiskLow(event.Path, event.UsedPercent, event.FreeBytes, event.ThresholdPercent)
		},
		Log: func(level, message string, fields map[string]interface{}) {
			switch message {
			case "Disk space low":
				message = cliText("cli.main.disk_space_low")
			case "Disk space recovered":
				message = cliText("cli.main.disk_space_recovered")
			}
			log.Printf("%s %s fields=%v", level, message, fields)
		},
	})
	auditRetention := cfg.Server.Logging.Audit.Keep
	if auditRetention == "" {
		auditRetention = cfg.Server.Logging.Defaults.Keep
	}
	auditCompress := cfg.Server.Logging.Audit.Compress != nil && *cfg.Server.Logging.Audit.Compress
	runtimeDeps := scheduler.Deps{
		StartedElevated:  startedElevated,
		RestartForUpdate: srv.RestartForUpdate,
		SSLRenewal:       func(ctx context.Context) error { return srv.RenewTLS(ctx) },
		LogRotation:      scheduler.HandlerFunc(logRotationTask),
		AuditRetention:   auditRetention,
		AuditCompress:    auditCompress,
		BlocklistUpdate: func(ctx context.Context) error {
			_, updateErr := blocklist.Update(ctx, blocklistCfg)
			var reloadErr error
			if blocklistMatcher != nil {
				reloadErr = blocklistMatcher.Reload(time.Now().UTC())
			}
			refreshErr := srv.RefreshHealth()
			if updateErr != nil {
				return updateErr
			}
			if reloadErr != nil {
				return reloadErr
			}
			return refreshErr
		},
		HealthRefresh: func(ctx context.Context) error { return srv.RefreshHealth() },
		CVEUpdate: func(ctx context.Context) error {
			if err := cve.Update(ctx, cve.Config{SecurityDir: securityDir, NVDSource: cfg.Data.CVE.Source, NVDAPIKey: cfg.Data.CVE.APIKey, TrivyEnabled: cfg.Data.Trivy.Enabled, TrivySource: cfg.Data.Trivy.Source}); err != nil {
				return err
			}
			return srv.RefreshHealth()
		},
		DiskCheck: func(ctx context.Context) error {
			_, err := checker.Check(ctx)
			return err
		},
		DiskCheckInterval:     cfg.Server.Notifications.DiskLow.CheckInterval,
		DatabaseCheck:         scheduler.HandlerFunc(newDatabaseCheckHandler(cfg.Server.Notifications.DatabaseError)),
		DatabaseCheckInterval: cfg.Server.Notifications.DatabaseError.CheckInterval,
		TorHealth: func(ctx context.Context) error {
			if torMgr == nil {
				return fmt.Errorf("%s", cliText("cli.main.tor_manager_unavailable"))
			}
			return torMgr.HealthCheck(ctx)
		},
		I2PHealth:          i2pRuntime.healthCheck,
		OwnerIdlePurgeDays: cfg.Server.Tokens.IdlePurgeDays,
		PurgeIdleOwners: func(ctx context.Context) error {
			if cfg.Server.Tokens.IdlePurgeDays <= 0 {
				return nil
			}
			_, err := db.PurgeIdleOwners(ctx, time.Now().UTC().AddDate(0, 0, -cfg.Server.Tokens.IdlePurgeDays))
			return err
		},
		ContactChallengeCleanup: func(ctx context.Context) error { return srv.CleanupExpiredContactChallenges(ctx) },
		ExpiredIPBlocks:         srv.CleanupExpiredIPBlocks,
	}
	runtimeDeps.TaskState = srv.SchedulerMetricsObserver()
	// ── Start the always-running task scheduler (AI.md PART 18) ───────────────
	var sched *scheduler.Scheduler
	var schedMu sync.Mutex
	startScheduler := func() {
		schedMu.Lock()
		defer schedMu.Unlock()
		if sched != nil {
			return
		}
		built, err := buildScheduler(cfg, configDir, dataDir, logsDir, geoipMgr, runtimeDeps)
		if err != nil {
			log.Printf("%s", cliText("cli.main.build_scheduler_failed", "error", err))
			return
		}
		if err := built.Start(context.Background()); err != nil {
			log.Printf("%s", cliText("cli.main.start_scheduler_failed", "error", err))
			return
		}
		sched = built
	}
	if databaseAvailable.Load() && !maintenanceState.Snapshot().Active {
		startScheduler()
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			var directoryErr error
			for _, directory := range []string{configDir, dataDir, apppath.GetCacheDir(), logsDir, apppath.GetBackupDir(), databaseDir} {
				if err := probeRuntimeDir(directory); err != nil {
					directoryErr = err
					break
				}
			}
			if directoryErr != nil {
				maintenanceState.Enter("file_write", "Required file storage is unavailable", time.Now().UTC())
			} else if err := db.Ping(); err != nil {
				databaseAvailable.Store(false)
				maintenanceState.Enter("database_connection", "Database connection failed", time.Now().UTC())
			}
			if maintenanceState.Snapshot().Active {
				select {
				case maintenanceWake <- struct{}{}:
				default:
				}
			}
		}
	}()
	go func() {
		for {
			<-maintenanceWake
			snapshot := maintenanceState.Snapshot()
			if !snapshot.Active || snapshot.Exhausted {
				continue
			}
			attempts := snapshot.Attempts
			retrySeconds := int(cfg.Server.Maintenance.SelfHealing.RetryInterval)
			if retrySeconds <= 0 {
				retrySeconds = 30
			}
			maxAttempts := cfg.Server.Maintenance.SelfHealing.MaxAttempts
			var lastAttempt *time.Time
		retry:
			for {
				nextAttempt := time.Now().UTC().Add(time.Duration(retrySeconds) * time.Second)
				maintenanceState.RecordAttempt(attempts, lastAttempt, &nextAttempt)
				timer := time.NewTimer(time.Duration(retrySeconds) * time.Second)
				<-timer.C
				attemptTime := time.Now().UTC()
				if cleanupErr := cleanupMaintenanceArtifacts(logsDir, apppath.GetBackupDir(), cfg); cleanupErr != nil {
					log.Printf("self-healing cleanup failed: %v", cleanupErr)
				}
				if !databaseAvailable.Load() {
					if err := db.Close(); err != nil {
						log.Printf("self-healing: closing unavailable database failed")
					}
					if err := initializeDatabase(); err != nil {
						attempts++
						lastAttempt = &attemptTime
						log.Printf("self-healing: database recovery attempt failed: %v", err)
						if maxAttempts > 0 && attempts >= maxAttempts {
							maintenanceState.Exhaust(attempts, lastAttempt)
							log.Printf("self-healing exhausted after %d attempts; operator action is required", attempts)
							break retry
						}
						continue
					}
					databaseAvailable.Store(true)
					configureRateLimitCache(cfg.Server.Cache, configDir)
				}
				var directoryErr error
				for _, directory := range []string{configDir, dataDir, apppath.GetCacheDir(), logsDir, apppath.GetBackupDir(), databaseDir} {
					if err := ensureRuntimeDir(directory); err != nil {
						directoryErr = err
						break
					}
					if err := probeRuntimeDir(directory); err != nil {
						directoryErr = err
						break
					}
				}
				if directoryErr != nil || db.Ping() != nil {
					attempts++
					lastAttempt = &attemptTime
					if directoryErr != nil {
						maintenanceState.Enter("file_write", "Required file storage is unavailable", attemptTime)
					} else {
						databaseAvailable.Store(false)
						maintenanceState.Enter("database_connection", "Database connection failed", attemptTime)
					}
					if maxAttempts > 0 && attempts >= maxAttempts {
						maintenanceState.Exhaust(attempts, lastAttempt)
						log.Printf("self-healing exhausted after %d attempts; operator action is required", attempts)
						break retry
					}
					continue
				}
				if err := db.ProbeWrite(); err != nil {
					attempts++
					lastAttempt = &attemptTime
					databaseAvailable.Store(false)
					maintenanceState.Enter("database_write", "Database write failed", attemptTime)
					if maxAttempts > 0 && attempts >= maxAttempts {
						maintenanceState.Exhaust(attempts, lastAttempt)
						break retry
					}
					continue
				}
				if _, err := applyBackupRetention(apppath.GetBackupDir(), cfg.Server.Backup.Retention, cfg.Server.Backup.EncryptionPassword); err != nil {
					log.Printf("self-healing: backup cleanup failed: %v", err)
				}
				if _, integrityErr := audit.VerifyAndReport(logsDir); integrityErr != nil {
					log.Printf("audit chain verification found a broken entry: %v", integrityErr)
				}
				if err := srv.RefreshHealth(); err != nil {
					attempts++
					lastAttempt = &attemptTime
					maintenanceState.RecordAttempt(attempts, lastAttempt, nil)
					if maxAttempts > 0 && attempts >= maxAttempts {
						maintenanceState.Exhaust(attempts, lastAttempt)
						break retry
					}
					continue
				}
				previous := maintenanceState.Recover()
				log.Printf("Issue resolved, exiting maintenance mode after %s", time.Since(previous.Since).Round(time.Second))
				startScheduler()
				break retry
			}
		}
	}()
	if snapshot := maintenanceState.Snapshot(); snapshot.Active {
		select {
		case maintenanceWake <- struct{}{}:
		default:
		}
	}

	for {
		select {
		case err := <-errChan:
			if err == nil {
				if cfgManager != nil {
					cfgManager.Stop()
				}
				i2pCancel()
				torCancel()
				if torMgr != nil {
					_ = torMgr.Close()
				}
				schedMu.Lock()
				activeScheduler := sched
				schedMu.Unlock()
				if activeScheduler != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
					activeScheduler.Stop(ctx)
					cancel()
				}
				if geoipMgr != nil {
					_ = geoipMgr.Close()
				}
				emitLifecycleEmail("shutdown", "signal", 5*time.Second)
				_ = db.Close()
				removePIDFile(pidFile)
				_ = closeApplicationLogs()
				os.Exit(0)
			}
			if errors.Is(err, server.ErrUpdateRestart) {
				schedMu.Lock()
				activeScheduler := sched
				schedMu.Unlock()
				if activeScheduler != nil {
					activeScheduler.Stop(context.Background())
				}
				if cfgManager != nil {
					cfgManager.Stop()
				}
				i2pCancel()
				torCancel()
				emitLifecycleEmail("shutdown", "update restart", 5*time.Second)
				if err := db.Close(); err != nil {
					log.Printf("database shutdown failed")
				}
				removePIDFile(pidFile)
				_ = closeApplicationLogs()
				os.Exit(exTempFail)
			}
			log.Printf("%s", cliText("cli.main.server_error", "error", err))
			if cfgManager != nil {
				cfgManager.Stop()
			}
			removePIDFile(pidFile)
			_ = closeApplicationLogs()
			os.Exit(exUnavailable)
		case sig := <-sigChan:
			switch classifySignal(sig) {
			case sigActionReopenLogs:
				log.Println(cliText("cli.received_sigusr1"))
				reopenLogs()
			case sigActionStatusDump:
				log.Println(cliText("cli.received_sigusr2"))
				dumpStatus()
			default:
				log.Printf("%s", cliText("cli.received_signal", "signal", sig))
				shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
				if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
					log.Printf("graceful server shutdown failed: %v", shutdownErr)
				}
				cancelShutdown()
				if cfgManager != nil {
					cfgManager.Stop()
				}
				i2pCancel()
				// Stop Tor FIRST (server owns the Tor lifecycle, AI.md PART 31).
				torCancel()
				if torMgr != nil {
					if err := torMgr.Close(); err != nil {
						log.Printf("%s", cliText("cli.main.stop_tor_failed", "error", err))
					}
				}
				schedMu.Lock()
				activeScheduler := sched
				schedMu.Unlock()
				if activeScheduler != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
					activeScheduler.Stop(ctx)
					cancel()
				}
				if geoipMgr != nil {
					_ = geoipMgr.Close()
				}
				emitLifecycleEmail("shutdown", "signal", 5*time.Second)
				if err := db.Close(); err != nil {
					log.Printf("database shutdown failed")
				}
				removePIDFile(pidFile)
				_ = closeApplicationLogs()
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
		if os.Getenv("NO_COLOR") != "" {
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

func probeRuntimeDir(path string) error {
	if path == "" {
		return nil
	}
	probe, err := os.CreateTemp(path, ".gitignore-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	return nil
}

func cleanupMaintenanceArtifacts(logsDir, backupDir string, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if ownedDirectory(logsDir) {
		retentionDays := cfg.Server.Maintenance.Cleanup.LogRetentionDays
		if retentionDays <= 0 {
			retentionDays = 7
		}
		entries, err := os.ReadDir(logsDir)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		for _, entry := range entries {
			name := entry.Name()
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || strings.HasPrefix(name, "audit.log.") || !strings.Contains(name, ".log.") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(logsDir, name)); err != nil {
				return err
			}
		}
	}
	if ownedDirectory(backupDir) {
		if _, err := applyBackupRetention(backupDir, cfg.Server.Backup.Retention, cfg.Server.Backup.EncryptionPassword); err != nil {
			return err
		}
	}
	return cleanupProjectTempArtifacts()
}

func ownedDirectory(path string) bool {
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil || filepath.Clean(abs) == filepath.VolumeName(abs)+string(os.PathSeparator) {
		return false
	}
	volume := filepath.VolumeName(abs)
	current := volume + string(os.PathSeparator)
	relative := strings.TrimPrefix(strings.TrimPrefix(abs, volume), string(os.PathSeparator))
	for _, segment := range strings.Split(relative, string(os.PathSeparator)) {
		if segment == "" {
			continue
		}
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if current == abs && !info.IsDir() {
			return false
		}
	}
	return true
}

func cleanupProjectTempArtifacts() error {
	base := filepath.Join(os.TempDir(), internalOrg)
	if !ownedDirectory(base) {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), internalName+"-") {
			continue
		}
		path := filepath.Join(base, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
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
	case "development", "dev", "devel":
		return "development"
	case "debug":
		return "debug"
	}
	return ""
}

func startupURLs(fqdn, portPair, baseURL, apiVersion string, tlsEnabled bool) []string {
	fqdn = strings.TrimSpace(fqdn)
	if fqdn == "" {
		return nil
	}
	ports := strings.Split(portPair, ",")
	base := "/" + strings.Trim(strings.TrimSpace(baseURL), "/")
	if base == "/" {
		base = ""
	}
	version := strings.Trim(strings.TrimSpace(apiVersion), "/")
	if version == "" {
		version = "v1"
	}
	urls := make([]string, 0, len(ports)*2)
	for index, value := range ports {
		port := strings.TrimSpace(value)
		if port == "" {
			continue
		}
		scheme := "http"
		if index == 1 || port == "443" || tlsEnabled && len(ports) == 1 {
			scheme = "https"
		}
		host := fqdn
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
		displayPort := port
		if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
			displayPort = ""
		}
		origin := scheme + "://" + host
		if displayPort != "" {
			origin += ":" + displayPort
		}
		urls = append(urls, origin+base, origin+base+"/api/"+version+"/server/swagger")
	}
	return urls
}

func resolveStartupFQDN() string {
	if domain := firstDomain(os.Getenv("DOMAIN")); domain != "" {
		return domain
	}
	if hostname, err := os.Hostname(); err == nil && usableConfiguredHost(hostname) {
		return hostname
	}
	if hostname := strings.TrimSpace(os.Getenv("HOSTNAME")); usableConfiguredHost(hostname) {
		return hostname
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var ipv6, ipv4 string
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil {
			continue
		}
		parsed, parseErr := netip.ParseAddr(ip.String())
		if parseErr != nil || !parsed.IsGlobalUnicast() || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() {
			continue
		}
		if parsed.Is6() && ipv6 == "" {
			ipv6 = parsed.String()
		}
		if parsed.Is4() && ipv4 == "" {
			ipv4 = parsed.String()
		}
	}
	if ipv6 != "" {
		return ipv6
	}
	return ipv4
}

func firstDomain(domains string) string {
	for _, domain := range strings.Split(domains, ",") {
		if value := strings.TrimSpace(domain); usableConfiguredHost(value) {
			return value
		}
	}
	return ""
}

func usableConfiguredHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return !address.IsLoopback() && !address.IsUnspecified() && !address.IsMulticast()
	}
	return true
}

func overlayURL(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	return "http://" + address
}

func startupTimezone() string {
	if value := strings.TrimSpace(os.Getenv("TZ")); value != "" {
		return value
	}
	return "America/New_York"
}

func printHelp(binaryName string) {
	fmt.Println(cliText("cli.server_help",
		"binary", binaryName,
		"version", Version,
		"update_branch_usage", cliText("cli.update_branch_usage")))
}

func checkHealth(port string, baseURLs ...string) error {
	baseURL := ""
	if len(baseURLs) > 0 && baseURLs[0] != "/" {
		baseURL = "/" + strings.Trim(baseURLs[0], "/")
	}
	url := fmt.Sprintf("http://127.0.0.1:%s%s/server/healthz", port, baseURL)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", cliText("cli.main.health_status_error", "status", resp.StatusCode))
	}
	return nil
}

func handleServiceCommand(cmd, configDir string, force bool) {
	switch cmd {
	case "start":
		if err := service.Start(); err != nil {
			log.Printf("%s", cliText("cli.main.start_service_failed", "error", err))
			os.Exit(exOSErr)
		}
	case "stop":
		if err := service.Stop(); err != nil {
			log.Printf("%s", cliText("cli.main.stop_service_failed", "error", err))
			os.Exit(exOSErr)
		}
	case "restart":
		if err := service.Restart(); err != nil {
			log.Printf("%s", cliText("cli.main.restart_service_failed", "error", err))
			os.Exit(exOSErr)
		}
	case "reload":
		if err := service.Reload(); err != nil {
			log.Printf("%s", cliText("cli.main.reload_service_failed", "error", err))
			os.Exit(exOSErr)
		}
	case "--install":
		installService()
	case "--uninstall":
		if err := service.Uninstall(force); err != nil {
			log.Printf("%s", cliText("cli.main.uninstall_service_failed", "error", err))
			os.Exit(exOSErr)
		}
	case "--disable":
		serviceDisable()
	case "--help":
		fmt.Println(cliText("cli.main.service_commands"))
	default:
		fmt.Fprintln(os.Stderr, cliText("cli.main.unknown_service_command", "command", cmd))
		os.Exit(1)
	}
}

// confirmPortEscalation asks before re-executing the server with elevated privileges.
func confirmPortEscalation(assumeYes bool) bool {
	if assumeYes {
		return true
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	fmt.Println(i18n.TranslateFormat(outputLanguage, "cli.requires_privileges", "action", "Binding to a privileged port"))
	fmt.Print(i18n.Translate(outputLanguage, "cli.escalate_prompt"))
	response, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && response == "" {
		return false
	}
	response = strings.ToLower(strings.TrimSpace(response))
	switch response {
	case "", "y", "yes", "j", "ja", "o", "oui", "s", "si", "да", "نعم":
		return true
	default:
		return false
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
			log.Printf("%s", cliText("cli.main.install_service_failed", "error", err))
			os.Exit(exCantCreat)
		}
		return
	}

	if method := service.DetectEscalation(); method != service.EscalateNone {
		exePath, err := os.Executable()
		if err == nil {
			fmt.Println(cliText("cli.main.install_service_escalation", "method", method))
			args := append([]string{exePath}, os.Args[1:]...)
			if err := service.ExecElevated(method, args); err != nil {
				log.Printf("%s", cliText("cli.main.install_service_via_failed", "method", method, "error", err))
				os.Exit(exOSErr)
			}
			return
		}
	}

	fmt.Println(cliText("cli.main.no_privilege_escalation"))
	fmt.Println(cliText("cli.main.user_service_fallback"))
	if err := service.InstallUser(); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.main.install_user_service_failed", "error", err))
		fmt.Fprintln(os.Stderr, cliText("cli.main.install_service_retry"))
		os.Exit(exCantCreat)
	}
}

func handleMaintenanceCommand(cmd, configDir, dataDir, logsDir, configPath string, assumeYes ...bool) {
	args := flag.Args()
	yes := len(assumeYes) > 0 && assumeYes[0]

	switch cmd {
	case "backup":
		if len(args) > 0 && args[0] == "test" {
			if len(args) > 2 {
				fmt.Fprintln(os.Stderr, cliText("cli.main.backup_test_usage"))
				os.Exit(2)
			}
			backupFile := ""
			if len(args) == 2 {
				backupFile = args[1]
			} else {
				var err error
				backupFile, err = latestBackupFile(apppath.GetBackupDir())
				if err != nil {
					log.Printf("%s", cliText("cli.main.backup_test_failed", "error", err))
					os.Exit(exIOErr)
				}
			}
			password := ""
			if strings.HasSuffix(backupFile, ".enc") {
				var err error
				password, err = promptPassword(cliText("cli.main.backup_password_prompt"))
				if err != nil {
					log.Printf("%s", cliText("cli.main.backup_test_failed", "error", err))
					os.Exit(exIOErr)
				}
			}
			if err := verifyBackupPGP(backupFile, password); err != nil {
				log.Printf("%s", cliText("cli.main.backup_pgp_verification_failed", "error", err))
				os.Exit(exIOErr)
			}
			fmt.Println(cliText("cli.main.backup_pgp_verified"))
			return
		}
		cfg, _ := config.Load(configPath)
		if err := db.Init(dataDir, databaseConnectionConfig(cfg)); err != nil {
			log.Printf("%s", cliText("cli.main.initialize_database_failed", "error", err))
			os.Exit(exUnavailable)
		}
		backupFile := ""
		if len(args) > 0 {
			backupFile = args[0]
		} else {
			backupDir := apppath.GetBackupDir()
			if err := os.MkdirAll(backupDir, 0755); err != nil {
				log.Printf("%s", cliText("cli.main.create_backup_directory_failed", "error", err))
				os.Exit(exCantCreat)
			}
			timestamp := time.Now().Format("2006-01-02_150405")
			backupFile = filepath.Join(backupDir, fmt.Sprintf("gitignore_backup_%s.tar.gz", timestamp))
		}
		if err := runBackup(cfg, configDir, dataDir, backupFile); err != nil {
			_ = db.Close()
			log.Printf("%s", cliText("cli.main.backup_failed", "error", err))
			os.Exit(exIOErr)
		}
		_ = db.Close()

	case "restore":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, cliText("cli.main.restore_usage"))
			os.Exit(2)
		}
		if err := runRestore(args[0], configDir, dataDir); err != nil {
			log.Printf("%s", cliText("cli.main.restore_failed", "error", err))
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
			fmt.Println(cliText("cli.main.current_mode", "mode", m))
			return
		}
		resolved := resolveMode(args[0])
		if resolved == "" {
			fmt.Fprintln(os.Stderr, cliText("cli.main.invalid_mode", "mode", args[0]))
			os.Exit(2)
		}
		if err := config.Update(func(c *config.Config) { c.Server.Mode = resolved }); err != nil {
			log.Printf("%s", cliText("cli.main.save_config_failed", "error", err))
			os.Exit(exConfig)
		}
		fmt.Println(cliText("cli.main.mode_set", "mode", resolved))

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
			log.Printf("%s", cliText("cli.main.reset_configuration_failed", "error", err))
			os.Exit(exConfig)
		}
		fmt.Println(cliText("cli.main.setup_title"))
		fmt.Println(cliText("cli.main.setup_separator"))
		fmt.Println(cliText("cli.main.config_path", "path", configPath))
		fmt.Println(cliText("cli.main.port_value", "port", port))
		fmt.Println(cliText("cli.main.mode_value", "mode", defaults.Server.Mode))
		fmt.Println(cliText("cli.main.configuration_reset"))
		fmt.Println(cliText("cli.main.setup_complete"))

	case "pgp":
		if err := handlePGPMaintenance(configDir, configPath, dataDir, logsDir, args); err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.pgp_maintenance_failed", "error", err))
			os.Exit(exConfig)
		}

	case "security":
		if err := handleSecurityMaintenance(configPath, dataDir, logsDir, args); err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.security_maintenance_failed", "error", err))
			os.Exit(exConfig)
		}

	case "secret":
		if err := handleSecretMaintenance(configPath, configDir, dataDir, logsDir, args); err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.maintenance_failed", "error", err))
			os.Exit(exConfig)
		}

	case "token", "data", "compliance":
		cfg, err := config.Load(configPath)
		if err == nil {
			if yes {
				args = append(args, "--yes")
			}
			err = handleOwnerMaintenance(cmd, cfg, dataDir, logsDir, args)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.main.maintenance_failed", "error", err))
			os.Exit(exConfig)
		}

	default:
		fmt.Fprintln(os.Stderr, cliText("cli.main.unknown_maintenance_command", "command", cmd))
		fmt.Fprintln(os.Stderr, cliText("cli.main.available_maintenance_commands"))
		os.Exit(1)
	}
}

func latestBackupFile(directory string) (string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	var latest string
	var latestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".tar.gz") && !strings.HasSuffix(entry.Name(), ".tar.gz.enc")) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if latest == "" || info.ModTime().After(latestTime) {
			latest = filepath.Join(directory, entry.Name())
			latestTime = info.ModTime()
		}
	}
	if latest == "" {
		return "", fmt.Errorf("%s", cliText("cli.main.no_backup_archives", "directory", directory))
	}
	return latest, nil
}

func serviceStatus() {
	installed, running, enabled, pid := service.Status()

	installedStr := cliText("cli.main.service_not_installed")
	if installed {
		installedStr = cliText("cli.main.service_installed")
	}
	state := cliText("cli.main.service_stopped")
	if running {
		state = cliText("cli.main.service_running")
	}
	autoStart := cliText("cli.main.service_disabled")
	if enabled {
		autoStart = cliText("cli.main.service_enabled")
	}
	pidStr := "-"
	if running && pid > 0 {
		pidStr = strconv.Itoa(pid)
	}

	fmt.Println(cliText("cli.main.current_status"))
	fmt.Println(cliText("cli.main.service_label", "value", installedStr))
	fmt.Println(cliText("cli.main.state_label", "value", state))
	fmt.Println(cliText("cli.main.autostart_label", "value", autoStart))
	fmt.Println(cliText("cli.main.pid_label", "value", pidStr))
}

func serviceDisable() {
	if err := service.Disable(); err != nil {
		log.Printf("%s", cliText("cli.main.service_disable_failed", "error", err))
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
			fmt.Println(cliText("cli.main.current_branch", "branch", cfg.Server.Update.Branch))
			return
		}
		branch, ok := config.NormalizeUpdateBranch(subArgs[0])
		if !ok {
			fmt.Fprintln(os.Stderr, cliText("cli.invalid_update_branch"))
			os.Exit(exUsage)
		}
		if err := config.Update(func(c *config.Config) { c.Server.Update.Branch = branch }); err != nil {
			log.Printf("%s", cliText("cli.main.save_config_failed", "error", err))
			os.Exit(exConfig)
		}
		fmt.Println(cliText("cli.main.branch_set", "branch", branch))
	default:
		fmt.Fprintln(os.Stderr, cliText("cli.main.unknown_update_command", "command", sub))
		os.Exit(exUsage)
	}
}

func runCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("%s", cliText("cli.main.command_failed", "name", name, "args", args, "error", err))
	}
}
