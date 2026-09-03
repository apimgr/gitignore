// Package paths resolves CLI runtime directories. Unlike the server's
// src/path package (which switches between system and user directories
// based on privilege), CLI directories are ALWAYS user-scope — even when
// the CLI is invoked as root — per AI.md PART 32 "Directory Structure".
package path

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	orgName     = "apimgr"
	projectName = "gitignore"
)

// ConfigDir returns the CLI config directory.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), orgName, projectName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", orgName, projectName)
}

// DataDir returns the CLI data directory.
func DataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), orgName, projectName, "data")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", orgName, projectName)
}

// CacheDir returns the CLI cache directory.
func CacheDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), orgName, projectName, "cache")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", orgName, projectName)
}

// LogDir returns the CLI log directory.
func LogDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), orgName, projectName, "log")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "log", orgName, projectName)
}

// ConfigFile returns the path to the named config profile (default "cli").
// An absolute path is returned unchanged; a bare name resolves to
// {ConfigDir}/{name}.yml.
func ConfigFile(name string) string {
	if name == "" {
		name = "cli"
	}
	if filepath.IsAbs(name) {
		return name
	}
	if filepath.Ext(name) == "" {
		name += ".yml"
	}
	return filepath.Join(ConfigDir(), name)
}

// LogFile returns the CLI log file path.
func LogFile() string {
	return filepath.Join(LogDir(), "cli.log")
}

// EnsureDirs creates all CLI directories with user-only (0700) permissions.
// Must be called on every startup before any file operation.
func EnsureDirs() error {
	dirs := []string{ConfigDir(), DataDir(), CacheDir(), LogDir()}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dir, 0o700); err != nil {
				return err
			}
		}
	}
	return nil
}
