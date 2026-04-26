package paths

import (
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
}

// GetDirectories returns OS-specific directories
// Uses {org}/{name} structure: /etc/apimgr/gitignore/, ~/.config/apimgr/gitignore/
func GetDirectories() Directories {
	configDir, dataDir, logsDir := GetDefaultDirs(ProjectName)
	return Directories{
		Config: configDir,
		Data:   dataDir,
		Logs:   logsDir,
	}
}

// GetDefaultDirs returns OS-specific default directories based on privileges
// Uses {org}/{name} structure: /etc/apimgr/gitignore/, ~/.config/apimgr/gitignore/
func GetDefaultDirs(projectName string) (configDir, dataDir, logsDir string) {
	// Check if running in container
	if IsRunningInContainer() {
		return "/config", "/data", "/logs"
	}

	// Check if running as root/admin
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
				programData = "C:\\ProgramData"
			}
			configDir = filepath.Join(programData, OrgName, projectName)
			dataDir = filepath.Join(programData, OrgName, projectName, "data")
			logsDir = filepath.Join(programData, OrgName, projectName, "logs")
		default: // Linux, BSD, macOS
			configDir = filepath.Join("/etc", OrgName, projectName)
			dataDir = filepath.Join("/var/lib", OrgName, projectName)
			logsDir = filepath.Join("/var/log", OrgName, projectName)
		}
	} else {
		var homeDir string
		currentUser, err := user.Current()
		if err == nil {
			homeDir = currentUser.HomeDir
		} else {
			homeDir = os.Getenv("HOME")
			if homeDir == "" {
				homeDir = os.Getenv("USERPROFILE")
			}
		}

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
		case "darwin":
			configDir = filepath.Join(homeDir, ".config", OrgName, projectName)
			dataDir = filepath.Join(homeDir, ".local", "share", OrgName, projectName)
			logsDir = filepath.Join(homeDir, ".local", "share", OrgName, projectName, "logs")
		default: // Linux, BSD
			xdgConfig := os.Getenv("XDG_CONFIG_HOME")
			if xdgConfig == "" {
				xdgConfig = filepath.Join(homeDir, ".config")
			}
			xdgData := os.Getenv("XDG_DATA_HOME")
			if xdgData == "" {
				xdgData = filepath.Join(homeDir, ".local", "share")
			}

			configDir = filepath.Join(xdgConfig, OrgName, projectName)
			dataDir = filepath.Join(xdgData, OrgName, projectName)
			logsDir = filepath.Join(xdgData, OrgName, projectName, "logs")
		}
	}

	return configDir, dataDir, logsDir
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
	// Check for Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Check for common container init systems
	data, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return false
	}
	comm := string(data)
	return comm == "tini\n" || comm == "tini" || comm == "dumb-init\n"
}

// GetBackupDir returns the default backup directory
func GetBackupDir() string {
	return filepath.Join("/mnt/Backups", OrgName, ProjectName)
}

// PathManager provides compatibility with old interface
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
func (pm *PathManager) SetConfigDir(dir string) {
	pm.dirs.Config = dir
}

// SetDataDir overrides the data directory
func (pm *PathManager) SetDataDir(dir string) {
	pm.dirs.Data = dir
}

// SetLogsDir overrides the logs directory
func (pm *PathManager) SetLogsDir(dir string) {
	pm.dirs.Logs = dir
}

// GetConfigDir returns the config directory
func (pm *PathManager) GetConfigDir() string {
	return pm.dirs.Config
}

// GetDataDir returns the data directory
func (pm *PathManager) GetDataDir() string {
	return pm.dirs.Data
}

// GetLogsDir returns the logs directory
func (pm *PathManager) GetLogsDir() string {
	return pm.dirs.Logs
}

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
