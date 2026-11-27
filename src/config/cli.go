package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	apppath "github.com/apimgr/gitignore/src/path"
)

func loadCommandLine(args []string) (*ServerConfig, error) {
	options := RuntimeOptions{}
	set := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&options.Port, "port", "", "Server port (overrides config)")
	set.StringVar(&options.Address, "address", "", "Server address (overrides config)")
	set.StringVar(&options.ConfigDir, "config", "", "Configuration directory")
	set.BoolVar(&options.Version, "version", false, "Show version information")
	set.BoolVar(&options.Version, "v", false, "Show version information (shorthand)")
	set.BoolVar(&options.Status, "status", false, "Check server status (health check)")
	set.BoolVar(&options.Help, "help", false, "Show help")
	set.BoolVar(&options.Help, "h", false, "Show help (shorthand)")
	set.BoolVar(&options.Debug, "debug", false, "Enable debug output")
	set.StringVar(&options.Color, "color", "auto", "Color output: auto, yes, no")
	set.StringVar(&options.Mode, "mode", "", "Application mode: production|development|debug (aliases: prod|dev|devel)")
	set.StringVar(&options.Service, "service", "", "Service commands")
	set.StringVar(&options.Maintenance, "maintenance", "", "Maintenance commands")
	set.BoolVar(&options.Force, "yes", false, "Skip supported confirmation prompts")
	set.StringVar(&options.Update, "update", "", "Update commands")
	set.StringVar(&options.DataDir, "data", "", "Data directory")
	set.StringVar(&options.CacheDir, "cache", "", "Cache directory")
	set.StringVar(&options.LogDir, "log", "", "Log directory")
	set.StringVar(&options.BackupDir, "backup", "", "Backup directory")
	set.StringVar(&options.PIDFile, "pid", "", "PID file path")
	set.StringVar(&options.BaseURL, "baseurl", "", "URL path prefix (default: /)")
	set.BoolVar(&options.I2P, "i2p", false, "Enable the I2P eepsite (default on in containers)")
	set.BoolVar(&options.Daemon, "daemon", false, "Run as daemon (detach from terminal)")
	set.StringVar(&options.Language, "lang", "", "Language for output (default: auto, from LANG env)")
	set.StringVar(&options.Shell, "shell", "", "Shell integration: completions, init, help [SHELL]")
	if err := set.Parse(args); err != nil {
		return nil, err
	}
	options.Args = set.Args()
	set.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "debug":
			options.DebugSpecified = true
		case "i2p":
			options.I2PSpecified = true
		}
	})
	flag.CommandLine = set

	dirs := apppath.GetDirectories()
	configDir := dirs.Config
	if envConfig := os.Getenv("CONFIG_DIR"); envConfig != "" {
		configDir = envConfig
	}
	if options.ConfigDir != "" {
		configDir = options.ConfigDir
	}
	var err error
	configDir, err = apppath.SafeDirPath(configDir)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration directory: %w", err)
	}
	configPath := filepath.Join(configDir, "server.yml")
	options.ConfigPath = configPath
	_, statErr := os.Stat(configPath)
	options.FirstRun = errors.Is(statErr, os.ErrNotExist)
	options.Startup = PeekStartupSettings(configPath)
	logsDir := dirs.Logs
	if options.LogDir != "" {
		logsDir = options.LogDir
	} else if envLogs := os.Getenv("LOG_DIR"); envLogs != "" {
		logsDir = envLogs
	}
	SetAuditLogDir(logsDir)
	var cfg *ServerConfig
	if options.Help || options.Version || options.Shell != "" {
		cfg = DefaultConfig()
	} else {
		cfg, err = loadConfigFile(configPath)
		if err != nil {
			return nil, err
		}
	}
	cfg.Runtime = options
	if options.I2PSpecified {
		cfg.Server.I2P.Enabled = options.I2P
	}
	return cfg, nil
}
