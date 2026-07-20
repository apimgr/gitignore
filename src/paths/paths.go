package paths

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

const (
	// OrgName is the organization name for directory structure
	OrgName = "apimgr"
	// ProjectName is the project name
	ProjectName = "gitignore"
)

// Directories holds the application directories
type Directories struct {
	Config string
	Data   string
	Logs   string
	Backup string
}

// GetDirectories returns OS-specific directories
func GetDirectories() Directories {
	configDir, dataDir, logsDir, backupDir := GetDefaultDirs(ProjectName)
	return Directories{
		Config: configDir,
		Data:   dataDir,
		Logs:   logsDir,
		Backup: backupDir,
	}
}

// GetDefaultDirs returns OS-specific default directories based on privileges
func GetDefaultDirs(projectName string) (configDir, dataDir, logsDir, backupDir string) {
	// Check if running in container
	if IsRunningInContainer() {
		return "/config", "/data", "/data/logs", "/data/backups"
	}

	isRoot := false
	if runtime.GOOS == "windows" {
		isRoot = os.Getenv("USERDOMAIN") == os.Getenv("COMPUTERNAME")
	} else {
		isRoot = os.Geteuid() == 0
	}

	if isRoot {
		switch runtime.GOOS {
		case "windows":
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = `C:\ProgramData`
			}
			base := filepath.Join(programData, OrgName, projectName)
			configDir = base
			dataDir = filepath.Join(base, "data")
			logsDir = filepath.Join(base, "logs")
			backupDir = filepath.Join(programData, "Backups", OrgName, projectName)

		case "darwin":
			configDir = filepath.Join("/Library/Application Support", OrgName, projectName)
			dataDir = filepath.Join("/Library/Application Support", OrgName, projectName, "data")
			logsDir = filepath.Join("/Library/Logs", OrgName, projectName)
			backupDir = filepath.Join("/Library/Backups", OrgName, projectName)

		case "freebsd", "openbsd", "netbsd":
			configDir = filepath.Join("/usr/local/etc", OrgName, projectName)
			dataDir = filepath.Join("/var/db", OrgName, projectName)
			logsDir = filepath.Join("/var/log", OrgName, projectName)
			backupDir = filepath.Join("/var/backups", OrgName, projectName)

		default: // Linux
			configDir = filepath.Join("/etc", OrgName, projectName)
			dataDir = filepath.Join("/var/lib", OrgName, projectName)
			logsDir = filepath.Join("/var/log", OrgName, projectName)
			backupDir = filepath.Join("/mnt/Backups", OrgName, projectName)
		}
		return
	}

	// Non-privileged user
	homeDir := userHomeDir()

	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		configDir = filepath.Join(appData, OrgName, projectName)
		dataDir = filepath.Join(localAppData, OrgName, projectName)
		logsDir = filepath.Join(localAppData, OrgName, projectName, "logs")
		backupDir = filepath.Join(localAppData, "Backups", OrgName, projectName)

	case "darwin":
		appSupport := filepath.Join(homeDir, "Library", "Application Support")
		configDir = filepath.Join(appSupport, OrgName, projectName)
		dataDir = filepath.Join(appSupport, OrgName, projectName)
		logsDir = filepath.Join(homeDir, "Library", "Logs", OrgName, projectName)
		backupDir = filepath.Join(homeDir, "Library", "Backups", OrgName, projectName)

	case "freebsd", "openbsd", "netbsd":
		xdgConfig := xdgConfigHome(homeDir)
		xdgData := xdgDataHome(homeDir)
		configDir = filepath.Join(xdgConfig, OrgName, projectName)
		dataDir = filepath.Join(xdgData, OrgName, projectName)
		logsDir = filepath.Join(xdgData, OrgName, projectName, "logs")
		backupDir = filepath.Join(homeDir, ".local", "backups", OrgName, projectName)

	default: // Linux
		xdgConfig := xdgConfigHome(homeDir)
		xdgData := xdgDataHome(homeDir)
		configDir = filepath.Join(xdgConfig, OrgName, projectName)
		dataDir = filepath.Join(xdgData, OrgName, projectName)
		logsDir = filepath.Join(xdgData, OrgName, projectName, "logs")
		backupDir = filepath.Join(homeDir, ".local", "backups", OrgName, projectName)
	}
	return
}

// EnsureDirectories creates all required directories
func EnsureDirectories(dirs Directories) error {
	for _, dir := range []string{dirs.Config, dirs.Data, dirs.Logs} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDir creates a directory if it doesn't exist
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// IsRunningInContainer checks if running inside a container
func IsRunningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return false
	}
	comm := string(data)
	return comm == "tini\n" || comm == "tini" || comm == "dumb-init\n"
}

// GetBackupDir returns the default backup directory
func GetBackupDir() string {
	_, _, _, backupDir := GetDefaultDirs(ProjectName)
	return backupDir
}

// isPrivileged reports whether the process is running with elevated
// privileges (root on Unix, an administrator account on Windows).
func isPrivileged() bool {
	if runtime.GOOS == "windows" {
		return os.Getenv("USERDOMAIN") == os.Getenv("COMPUTERNAME")
	}
	return os.Geteuid() == 0
}

// GetCacheDir returns the OS-specific cache directory based on privilege
// level, matching the layout used by GetDefaultDirs.
func GetCacheDir() string {
	if IsRunningInContainer() {
		return "/data/cache"
	}

	if isPrivileged() {
		switch runtime.GOOS {
		case "windows":
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = `C:\ProgramData`
			}
			return filepath.Join(programData, OrgName, ProjectName, "cache")
		case "darwin":
			return filepath.Join("/Library/Caches", OrgName, ProjectName)
		default:
			return filepath.Join("/var/cache", OrgName, ProjectName)
		}
	}

	homeDir := userHomeDir()
	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		return filepath.Join(localAppData, OrgName, ProjectName, "cache")
	case "darwin":
		return filepath.Join(homeDir, "Library", "Caches", OrgName, ProjectName)
	default:
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache == "" {
			xdgCache = filepath.Join(homeDir, ".cache")
		}
		return filepath.Join(xdgCache, OrgName, ProjectName)
	}
}

// GetPIDFile returns the OS-specific PID file path based on privilege
// level. Returns an empty string on Windows, which does not use PID files.
func GetPIDFile() string {
	if runtime.GOOS == "windows" {
		return ""
	}

	if isPrivileged() {
		return fmt.Sprintf("/var/run/%s/%s.pid", OrgName, ProjectName)
	}

	homeDir := userHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(homeDir, "Library", "Application Support", OrgName, ProjectName, ProjectName+".pid")
	}
	return filepath.Join(xdgDataHome(homeDir), OrgName, ProjectName, ProjectName+".pid")
}

// PathManager provides path operations rooted at the application directories
type PathManager struct {
	dirs Directories
}

// New creates a new PathManager with OS-specific defaults
func New() *PathManager {
	return &PathManager{
		dirs: GetDirectories(),
	}
}

// SetConfigDir overrides the config directory
func (pm *PathManager) SetConfigDir(dir string) { pm.dirs.Config = dir }

// SetDataDir overrides the data directory
func (pm *PathManager) SetDataDir(dir string) { pm.dirs.Data = dir }

// SetLogsDir overrides the logs directory
func (pm *PathManager) SetLogsDir(dir string) { pm.dirs.Logs = dir }

// GetConfigDir returns the config directory
func (pm *PathManager) GetConfigDir() string { return pm.dirs.Config }

// GetDataDir returns the data directory
func (pm *PathManager) GetDataDir() string { return pm.dirs.Data }

// GetLogsDir returns the logs directory
func (pm *PathManager) GetLogsDir() string { return pm.dirs.Logs }

// GetBackupDir returns the backup directory
func (pm *PathManager) GetBackupDir() string { return pm.dirs.Backup }

// ConfigPath returns a path within the config directory
func (pm *PathManager) ConfigPath(filename string) string {
	return filepath.Join(pm.dirs.Config, filename)
}

// DataPath returns a path within the data directory
func (pm *PathManager) DataPath(filename string) string {
	return filepath.Join(pm.dirs.Data, filename)
}

// LogsPath returns a path within the logs directory
func (pm *PathManager) LogsPath(filename string) string {
	return filepath.Join(pm.dirs.Logs, filename)
}

// EnsureDirectories creates all necessary directories
func (pm *PathManager) EnsureDirectories() error {
	return EnsureDirectories(pm.dirs)
}

// helpers

func userHomeDir() string {
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE")
}

func xdgConfigHome(homeDir string) string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir, ".config")
}

func xdgDataHome(homeDir string) string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir, ".local", "share")
}
