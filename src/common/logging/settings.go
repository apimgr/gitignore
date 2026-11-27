package logging

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apimgr/gitignore/src/config"
)

// Kind describes the effective settings for one configured log file.
type Kind struct {
	Name     string
	Filename string
	Format   string
	Custom   string
	Rotate   string
	Keep     string
	Enabled  bool
	Compress bool
}

// Settings contains the normalized level and six file kinds from server.logs.
type Settings struct {
	Level string
	Kinds map[string]Kind
}

// Resolve applies the documented server.logs defaults and validates every
// effective setting before a logger opens files.
func Resolve(cfg config.LoggingConfig, appMode string, debug bool) (Settings, error) {
	level := strings.ToLower(strings.TrimSpace(cfg.Level))
	if level == "" {
		if strings.EqualFold(appMode, "production") && !debug {
			level = "info"
		} else {
			level = "debug"
		}
	}
	if level != "debug" && level != "info" && level != "warn" && level != "error" {
		return Settings{}, fmt.Errorf("invalid log level")
	}
	type item struct {
		name    string
		value   config.LogFileConfig
		format  string
		rotate  string
		enabled bool
	}
	items := []item{
		{"access", cfg.Access, "apache", "monthly", true},
		{"server", cfg.Server, "text", "", true},
		{"error", cfg.Error, "text", "", true},
		{"audit", cfg.Audit.LogFileConfig, "json", "daily", true},
		{"security", cfg.Security, "fail2ban", "", true},
		{"debug", cfg.Debug, "text", "", false},
	}
	result := Settings{Level: level, Kinds: make(map[string]Kind, len(items))}
	for _, entry := range items {
		value := entry.value
		format := strings.ToLower(strings.TrimSpace(value.Format))
		if format == "" {
			format = entry.format
		}
		if !validFormat(entry.name, format) {
			return Settings{}, fmt.Errorf("invalid %s log format", entry.name)
		}
		filename := strings.TrimSpace(value.Filename)
		if filename == "" {
			filename = entry.name + ".log"
		}
		if filepath.Base(filename) != filename || strings.ContainsAny(filename, "/\\") || filename == "." || filename == ".." {
			return Settings{}, fmt.Errorf("invalid %s log filename", entry.name)
		}
		rotate := strings.TrimSpace(value.Rotate)
		if rotate == "" {
			rotate = strings.TrimSpace(entry.rotate)
		}
		if rotate == "" {
			rotate = strings.TrimSpace(cfg.Defaults.Rotate)
		}
		if rotate == "" {
			rotate = "weekly,50MB"
		}
		if !validRotation(rotate) {
			return Settings{}, fmt.Errorf("invalid %s log rotation policy", entry.name)
		}
		keep := strings.TrimSpace(value.Keep)
		if keep == "" {
			keep = strings.TrimSpace(cfg.Defaults.Keep)
		}
		if keep == "" {
			keep = "none"
		}
		if !validRetention(keep) {
			return Settings{}, fmt.Errorf("invalid %s log retention policy", entry.name)
		}
		enabled := entry.enabled
		if value.Enabled != nil {
			enabled = *value.Enabled
		}
		compress := value.Compress != nil && *value.Compress
		custom := value.Custom
		if custom == "" {
			custom = cfg.Defaults.Custom
		}
		if format == "custom" && !validCustom(custom) {
			return Settings{}, fmt.Errorf("invalid custom format for %s log", entry.name)
		}
		result.Kinds[entry.name] = Kind{Name: entry.name, Filename: filename, Format: format, Custom: custom, Rotate: rotate, Keep: keep, Enabled: enabled, Compress: compress}
	}
	return result, nil
}

func validCustom(format string) bool {
	allowed := map[string]bool{"time": true, "date": true, "datetime": true, "remote_ip": true, "method": true, "path": true, "query": true, "status": true, "bytes": true, "latency": true, "latency_ms": true, "user_agent": true, "referer": true, "request_id": true, "fqdn": true, "protocol": true, "tls_version": true, "country": true, "asn": true}
	for index := 0; index < len(format); {
		if format[index] == '{' {
			end := strings.IndexByte(format[index:], '}')
			if end < 0 || !allowed[format[index+1:index+end]] {
				return false
			}
			index += end + 1
			continue
		}
		if format[index] == '}' {
			return false
		}
		index++
	}
	return strings.TrimSpace(format) != ""
}

func validFormat(kind, format string) bool {
	switch kind {
	case "access":
		return format == "apache" || format == "nginx" || format == "json" || format == "custom"
	case "server", "error", "debug":
		return format == "text" || format == "json"
	case "audit":
		return format == "json"
	case "security":
		return format == "fail2ban" || format == "syslog" || format == "cef" || format == "json" || format == "text"
	default:
		return false
	}
}

func validRotation(value string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "daily" || part == "weekly" || part == "monthly" || part == "yearly" || part == "never" {
			continue
		}
		if !validSize(part) {
			return false
		}
	}
	return len(parts) > 0
}

func validSize(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, suffix := range []string{"TIB", "GIB", "MIB", "KIB", "TB", "GB", "MB", "KB", "T", "G", "M", "K", "B"} {
		if strings.HasSuffix(value, suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(value, suffix))
			if number == "" {
				return false
			}
			n, err := strconv.ParseUint(number, 10, 64)
			return err == nil && n > 0
		}
	}
	return false
}

func validRetention(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" || value == "forever" {
		return true
	}
	for _, part := range strings.Split(value, ":") {
		if part == "daily" || part == "weekly" || part == "monthly" || part == "yearly" {
			continue
		}
		if part == "" {
			return false
		}
		for _, suffix := range []string{"d", "w", "m"} {
			if strings.HasSuffix(part, suffix) {
				part = strings.TrimSuffix(part, suffix)
				break
			}
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}
