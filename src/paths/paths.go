package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

// PathManager handles OS-specific directory paths
type PathManager struct {
	configDir  string
	dataDir    string
	logsDir    string
	runtimeDir string
}

// New creates a new PathManager with OS-specific defaults
func New() *PathManager {
	pm := &PathManager{}
	pm.detectDefaults()
	return pm
}

// detectDefaults detects OS-specific default directories
func (pm *PathManager) detectDefaults() {
	goos := runtime.GOOS
	uid := os.Getuid()

	// Check if running in Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		pm.configDir = "/config"
		pm.dataDir = "/data"
		pm.logsDir = "/data/logs"
		pm.runtimeDir = "/tmp"
		return
	}

	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd":
		if uid == 0 {
			// Root user
			pm.configDir = "/etc/gitignore"
			pm.dataDir = "/var/lib/gitignore"
			pm.logsDir = "/var/log/gitignore"
			pm.runtimeDir = "/run/gitignore"
		} else {
			// Non-root user
			homeDir := getHomeDir()
			pm.configDir = filepath.Join(homeDir, ".config", "gitignore")
			pm.dataDir = filepath.Join(homeDir, ".local", "share", "gitignore")
			pm.logsDir = filepath.Join(homeDir, ".local", "state", "gitignore")
			pm.runtimeDir = filepath.Join(homeDir, ".local", "run", "gitignore")
		}

	case "darwin":
		if uid == 0 {
			// Root/privileged user
			pm.configDir = "/Library/Application Support/GitIgnore"
			pm.dataDir = "/Library/Application Support/GitIgnore/data"
			pm.logsDir = "/Library/Logs/GitIgnore"
			pm.runtimeDir = "/var/run/gitignore"
		} else {
			// Non-privileged user
			homeDir := getHomeDir()
			pm.configDir = filepath.Join(homeDir, "Library", "Application Support", "GitIgnore")
			pm.dataDir = filepath.Join(homeDir, "Library", "Application Support", "GitIgnore", "data")
			pm.logsDir = filepath.Join(homeDir, "Library", "Logs", "GitIgnore")
			pm.runtimeDir = filepath.Join(homeDir, "Library", "Application Support", "GitIgnore", "run")
		}

	case "windows":
		programData := os.Getenv("PROGRAMDATA")
		appData := os.Getenv("APPDATA")

		if programData != "" && isAdmin() {
			// System-wide installation
			pm.configDir = filepath.Join(programData, "GitIgnore", "config")
			pm.dataDir = filepath.Join(programData, "GitIgnore", "data")
			pm.logsDir = filepath.Join(programData, "GitIgnore", "logs")
			pm.runtimeDir = filepath.Join(programData, "GitIgnore", "run")
		} else if appData != "" {
			// User installation
			pm.configDir = filepath.Join(appData, "GitIgnore", "config")
			pm.dataDir = filepath.Join(appData, "GitIgnore", "data")
			pm.logsDir = filepath.Join(appData, "GitIgnore", "logs")
			pm.runtimeDir = filepath.Join(appData, "GitIgnore", "run")
		} else {
			// Fallback
			homeDir := getHomeDir()
			pm.configDir = filepath.Join(homeDir, "GitIgnore", "config")
			pm.dataDir = filepath.Join(homeDir, "GitIgnore", "data")
			pm.logsDir = filepath.Join(homeDir, "GitIgnore", "logs")
			pm.runtimeDir = filepath.Join(homeDir, "GitIgnore", "run")
		}

	default:
		// Fallback for unknown OS
		homeDir := getHomeDir()
		pm.configDir = filepath.Join(homeDir, ".gitignore", "config")
		pm.dataDir = filepath.Join(homeDir, ".gitignore", "data")
		pm.logsDir = filepath.Join(homeDir, ".gitignore", "logs")
		pm.runtimeDir = filepath.Join(homeDir, ".gitignore", "run")
	}
}

// getHomeDir returns the user's home directory
func getHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if user, err := user.Current(); err == nil {
		return user.HomeDir
	}
	return "."
}

// isAdmin checks if running as administrator (Windows)
func isAdmin() bool {
	// Simple check - in production might want more sophisticated detection
	return os.Getuid() == 0
}

// SetConfigDir overrides the config directory
func (pm *PathManager) SetConfigDir(dir string) {
	pm.configDir = dir
}

// SetDataDir overrides the data directory
func (pm *PathManager) SetDataDir(dir string) {
	pm.dataDir = dir
}

// SetLogsDir overrides the logs directory
func (pm *PathManager) SetLogsDir(dir string) {
	pm.logsDir = dir
}

// SetRuntimeDir overrides the runtime directory
func (pm *PathManager) SetRuntimeDir(dir string) {
	pm.runtimeDir = dir
}

// GetConfigDir returns the config directory
func (pm *PathManager) GetConfigDir() string {
	return pm.configDir
}

// GetDataDir returns the data directory
func (pm *PathManager) GetDataDir() string {
	return pm.dataDir
}

// GetLogsDir returns the logs directory
func (pm *PathManager) GetLogsDir() string {
	return pm.logsDir
}

// GetRuntimeDir returns the runtime directory
func (pm *PathManager) GetRuntimeDir() string {
	return pm.runtimeDir
}

// ConfigPath returns a path within the config directory
func (pm *PathManager) ConfigPath(filename string) string {
	return filepath.Join(pm.configDir, filename)
}

// DataPath returns a path within the data directory
func (pm *PathManager) DataPath(filename string) string {
	return filepath.Join(pm.dataDir, filename)
}

// LogsPath returns a path within the logs directory
func (pm *PathManager) LogsPath(filename string) string {
	return filepath.Join(pm.logsDir, filename)
}

// RuntimePath returns a path within the runtime directory
func (pm *PathManager) RuntimePath(filename string) string {
	return filepath.Join(pm.runtimeDir, filename)
}

// EnsureDirectories creates all necessary directories
func (pm *PathManager) EnsureDirectories() error {
	dirs := []string{
		pm.configDir,
		pm.dataDir,
		pm.logsDir,
		pm.runtimeDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}
