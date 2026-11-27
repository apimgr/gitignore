package path

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
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
		return filepath.Join("/config", projectName), filepath.Join("/data", projectName), filepath.Join("/data", "log", projectName), filepath.Join("/data", "backups", projectName)
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
		logsDir = filepath.Join(homeDir, ".local", "log", OrgName, projectName)
		backupDir = filepath.Join(xdgData, "Backups", OrgName, projectName)

	default: // Linux
		xdgConfig := xdgConfigHome(homeDir)
		xdgData := xdgDataHome(homeDir)
		configDir = filepath.Join(xdgConfig, OrgName, projectName)
		dataDir = filepath.Join(xdgData, OrgName, projectName)
		logsDir = filepath.Join(homeDir, ".local", "log", OrgName, projectName)
		backupDir = filepath.Join(xdgData, "Backups", OrgName, projectName)
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

// EnforcePrivate fixes the private configuration and data trees before they are read.
func EnforcePrivate(configDir, dataDir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for _, dir := range []string{
		filepath.Join(configDir, "yaml.d"), filepath.Join(configDir, "assets"),
		filepath.Join(configDir, "ssl"), filepath.Join(configDir, "tor"),
		filepath.Join(configDir, "i2p"), filepath.Join(dataDir, "security"),
		filepath.Join(dataDir, "cache", "branding"),
		filepath.Join(dataDir, "tor"), filepath.Join(dataDir, "tor", "site"),
	} {
		if err := enforcePrivateTree(dir); err != nil {
			return err
		}
	}
	for _, file := range []string{filepath.Join(configDir, "server.yml"), filepath.Join(configDir, "secrets.yml")} {
		info, err := os.Lstat(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot inspect private path %s (mode unknown)", file)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot secure private path %s (mode %s)", file, info.Mode())
		}
		if err := enforcePrivateMode(file, info, 0600); err != nil {
			return err
		}
		f, err := os.OpenFile(file, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("private path %s (mode %s) is not usable", file, info.Mode())
		}
		_ = f.Close()
	}
	return nil
}

func enforcePrivateTree(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return fmt.Errorf("cannot create private path %s (mode 0700)", root)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("cannot secure private path %s (mode unknown)", path)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("cannot inspect private path %s (mode unknown)", path)
		}
		mode := os.FileMode(0600)
		if info.IsDir() {
			mode = 0700
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot secure private path %s (mode %s)", path, info.Mode())
		}
		if err := enforcePrivateMode(path, info, mode); err != nil {
			return err
		}
		if info.IsDir() {
			probe, err := os.CreateTemp(path, ".private-probe-")
			if err != nil {
				return fmt.Errorf("private path %s (mode %s) is not usable", path, info.Mode())
			}
			name := probe.Name()
			_ = probe.Close()
			if err := os.Remove(name); err != nil {
				return fmt.Errorf("private path %s (mode %s) is not usable", path, info.Mode())
			}
		} else {
			file, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				return fmt.Errorf("private path %s (mode %s) is not usable", path, info.Mode())
			}
			_ = file.Close()
		}
		return nil
	})
}

func enforcePrivateMode(path string, info fs.FileInfo, mode os.FileMode) error {
	if info.Mode().Perm() == mode {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		if !os.IsPermission(err) || info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("cannot secure private path %s (mode %s)", path, info.Mode())
		}
	}
	return nil
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

// GetBackupDir returns the default backup directory. The BACKUP_DIR init-only
// environment variable (AI.md PART 5) overrides the OS-specific default when
// set; the --backup CLI flag is applied by exporting BACKUP_DIR at startup.
func GetBackupDir() string {
	if v := strings.TrimSpace(os.Getenv("BACKUP_DIR")); v != "" {
		return v
	}
	_, _, _, backupDir := GetDefaultDirs(ProjectName)
	return backupDir
}

// GetDatabaseDir returns the database directory, honoring DATABASE_DIR before
// applying the container or native default.
func GetDatabaseDir(dataDir string) string {
	if v := strings.TrimSpace(os.Getenv("DATABASE_DIR")); v != "" {
		return v
	}
	if IsRunningInContainer() {
		return filepath.Join("/data", "db", "sqlite")
	}
	return filepath.Join(dataDir, "db")
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
	// CACHE_DIR init-only override (AI.md PART 5); the --cache CLI flag is
	// applied by exporting CACHE_DIR at startup.
	if v := strings.TrimSpace(os.Getenv("CACHE_DIR")); v != "" {
		return v
	}
	if IsRunningInContainer() {
		return filepath.Join("/data", ProjectName, "cache")
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
