package logging

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Entry is one plain-text log event.
type Entry struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]string
}

// AccessEntry contains fields used by the supported access log formats.
type AccessEntry struct {
	Time       time.Time
	RemoteIP   string
	Method     string
	Path       string
	Query      string
	Status     int
	Bytes      int64
	Latency    time.Duration
	UserAgent  string
	Referer    string
	RequestID  string
	Host       string
	Protocol   string
	TLSVersion string
	Country    string
	ASN        string
}

type fileLog struct {
	mu       sync.Mutex
	path     string
	settings Kind
	file     *os.File
}

// Manager owns the configured files and applies size/time rotation on writes.
type Manager struct {
	dir      string
	settings Settings
	files    map[string]*fileLog
}

// Open creates the configured log directory and active files.
func Open(dir string, settings Settings) (*Manager, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("log directory is empty")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	manager := &Manager{dir: dir, settings: settings, files: make(map[string]*fileLog, len(settings.Kinds))}
	for name, kind := range settings.Kinds {
		if !kind.Enabled {
			continue
		}
		path := filepath.Join(dir, kind.Filename)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode(name))
		if err != nil {
			_ = manager.Close()
			return nil, err
		}
		if err := file.Chmod(logFileMode(name)); err != nil {
			_ = file.Close()
			_ = manager.Close()
			return nil, err
		}
		manager.files[name] = &fileLog{path: path, settings: kind, file: file}
	}
	return manager, nil
}

// Writer returns the active file writer for a configured kind.
func (m *Manager) Writer(kind string) io.Writer {
	if m == nil {
		return io.Discard
	}
	return kindWriter{manager: m, kind: kind}
}

type kindWriter struct {
	manager *Manager
	kind    string
}

func (w kindWriter) Write(data []byte) (int, error) {
	if err := w.manager.writeRaw(w.kind, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

// Write emits a formatted text, JSON, or security event to one log kind.
func (m *Manager) Write(kind string, event Entry) error {
	settings, ok := m.settings.Kinds[kind]
	if !ok || !settings.Enabled {
		return nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	line, err := formatEntry(settings.Format, event)
	if err != nil {
		return err
	}
	return m.writeRaw(kind, line)
}

// Access writes one HTTP request event using the selected access format.
func (m *Manager) Access(event AccessEntry) error {
	settings, ok := m.settings.Kinds["access"]
	if !ok || !settings.Enabled {
		return nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	line, err := formatAccess(settings, event)
	if err != nil {
		return err
	}
	return m.writeRaw("access", line)
}

func (m *Manager) writeRaw(kind string, data []byte) error {
	file := m.files[kind]
	if file == nil {
		return nil
	}
	data = cleanLogLine(data)
	file.mu.Lock()
	defer file.mu.Unlock()
	if shouldRotate(file.path, file.settings.Rotate, data, time.Now()) {
		if err := rotateFile(file.path, file.settings); err != nil {
			return err
		}
		if err := file.file.Close(); err != nil {
			return err
		}
		opened, err := os.OpenFile(file.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode(file.settings.Name))
		if err != nil {
			return err
		}
		file.file = opened
	}
	if _, err := file.file.Write(data); err != nil {
		return err
	}
	return nil
}

func cleanLogLine(data []byte) []byte {
	clean := append([]byte(nil), data...)
	for index, value := range clean {
		if value == '\n' && index == len(clean)-1 {
			continue
		}
		if value < 0x20 || value > 0x7e {
			clean[index] = '?'
		}
	}
	if len(clean) == 0 || clean[len(clean)-1] != '\n' {
		clean = append(clean, '\n')
	}
	return clean
}

// Rotate applies configured time-based rotation to every active log.
func (m *Manager) Rotate(now time.Time) error {
	for name, file := range m.files {
		if name == "audit" {
			continue
		}
		file.mu.Lock()
		if shouldRotate(file.path, file.settings.Rotate, nil, now) {
			if err := rotateFile(file.path, file.settings); err != nil {
				file.mu.Unlock()
				return err
			}
			if err := file.file.Close(); err != nil {
				file.mu.Unlock()
				return err
			}
			opened, err := os.OpenFile(file.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode(file.settings.Name))
			if err != nil {
				file.mu.Unlock()
				return err
			}
			file.file = opened
		}
		file.mu.Unlock()
	}
	return nil
}

// Reopen closes and reopens active files after an external rotation request.
func (m *Manager) Reopen() error {
	for _, file := range m.files {
		file.mu.Lock()
		if err := file.file.Close(); err != nil {
			file.mu.Unlock()
			return err
		}
		opened, err := os.OpenFile(file.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode(file.settings.Name))
		if err != nil {
			file.mu.Unlock()
			return err
		}
		if err := opened.Chmod(logFileMode(file.settings.Name)); err != nil {
			_ = opened.Close()
			file.mu.Unlock()
			return err
		}
		file.file = opened
		file.mu.Unlock()
	}
	return nil
}

// Close closes every active log file.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var result error
	for _, file := range m.files {
		file.mu.Lock()
		if err := file.file.Close(); err != nil && result == nil {
			result = err
		}
		file.mu.Unlock()
	}
	return result
}

func formatEntry(format string, event Entry) ([]byte, error) {
	timestamp := event.Time.Format(time.RFC3339)
	level := strings.ToUpper(safe(event.Level))
	message := safe(event.Message)
	switch format {
	case "json":
		value := map[string]interface{}{"time": timestamp, "level": level, "msg": message}
		for key, field := range event.Fields {
			key = safe(key)
			if key == "time" || key == "level" || key == "msg" {
				continue
			}
			value[key] = safe(field)
		}
		encoded, err := json.Marshal(value)
		return append(encoded, '\n'), err
	case "syslog":
		return []byte(fmt.Sprintf("<134>1 %s localhost gitignore - - - %s %s\n", event.Time.UTC().Format(time.RFC3339), level, message)), nil
	case "cef":
		return []byte(fmt.Sprintf("CEF:0|gitignore|server|1|%s|%s|5|msg=%s\n", cefEscape(level), cefEscape(message), cefEscape(message))), nil
	case "fail2ban":
		return []byte(fmt.Sprintf("%s [security] %s\n", event.Time.Format("2006-01-02T15:04:05-07:00"), message)), nil
	default:
		return []byte(fmt.Sprintf("%s [%s] %s\n", event.Time.Format("2006-01-02T15:04:05-07:00"), level, message)), nil
	}
}

func formatAccess(settings Kind, event AccessEntry) ([]byte, error) {
	remote, method, path := safe(event.RemoteIP), safe(event.Method), safe(event.Path)
	userAgent, referer := safe(event.UserAgent), safe(event.Referer)
	timestamp := event.Time.Format("02/Jan/2006:15:04:05 -0700")
	switch settings.Format {
	case "json":
		value := map[string]interface{}{"ip": remote, "time": event.Time.UTC().Format(time.RFC3339), "method": method, "path": path, "status": event.Status, "size": event.Bytes, "ua": userAgent}
		encoded, err := json.Marshal(value)
		return append(encoded, '\n'), err
	case "nginx":
		return []byte(fmt.Sprintf("%s - - [%s] %q %d %d\n", remote, timestamp, method+" "+path+" HTTP/1.1", event.Status, event.Bytes)), nil
	case "custom":
		return []byte(formatCustom(settings.Custom, event)), nil
	default:
		return []byte(fmt.Sprintf("%s - - [%s] %q %d %d %q %q\n", remote, timestamp, method+" "+path+" HTTP/1.1", event.Status, event.Bytes, referer, userAgent)), nil
	}
}

func formatCustom(format string, event AccessEntry) string {
	values := map[string]string{
		"time": event.Time.Format("15:04:05"), "date": event.Time.Format("2006-01-02"),
		"datetime": event.Time.Format(time.RFC3339), "remote_ip": event.RemoteIP,
		"method": event.Method, "path": event.Path, "query": event.Query,
		"status": strconv.Itoa(event.Status), "bytes": strconv.FormatInt(event.Bytes, 10),
		"latency": event.Latency.String(), "latency_ms": strconv.FormatInt(event.Latency.Milliseconds(), 10),
		"user_agent": event.UserAgent, "referer": event.Referer, "request_id": event.RequestID,
		"fqdn": event.Host, "protocol": event.Protocol, "tls_version": event.TLSVersion,
		"country": event.Country, "asn": event.ASN,
	}
	var out strings.Builder
	for index := 0; index < len(format); {
		if format[index] == '{' {
			end := strings.IndexByte(format[index:], '}')
			if end > 0 {
				key := format[index+1 : index+end]
				if value, ok := values[key]; ok {
					out.WriteString(safe(value))
				} else {
					out.WriteString("?")
				}
				index += end + 1
				continue
			}
		}
		out.WriteByte(format[index])
		index++
	}
	out.WriteByte('\n')
	return out.String()
}

func safe(value string) string {
	var out strings.Builder
	for _, char := range value {
		if char >= 0x20 && char <= 0x7e {
			out.WriteRune(char)
		} else {
			out.WriteByte('?')
		}
	}
	return out.String()
}

func cefEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "=", "\\=")
}

func shouldRotate(path, policy string, next []byte, now time.Time) bool {
	if policy == "never" {
		return false
	}
	info, err := os.Stat(path)
	if err == nil {
		for _, part := range strings.Split(policy, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			switch part {
			case "daily":
				if !sameDay(info.ModTime(), now) {
					return true
				}
			case "weekly":
				iy, iw := info.ModTime().UTC().ISOWeek()
				ny, nw := now.UTC().ISOWeek()
				if iy != ny || iw != nw {
					return true
				}
			case "monthly":
				if info.ModTime().UTC().Year() != now.UTC().Year() || info.ModTime().UTC().Month() != now.UTC().Month() {
					return true
				}
			case "yearly":
				if info.ModTime().UTC().Year() != now.UTC().Year() {
					return true
				}
			default:
				if limit := parseSize(part); limit > 0 && info.Size()+int64(len(next)) >= limit {
					return true
				}
			}
		}
	}
	return false
}

func sameDay(left, right time.Time) bool {
	ly, lm, ld := left.UTC().Date()
	ry, rm, rd := right.UTC().Date()
	return ly == ry && lm == rm && ld == rd
}

func parseSize(value string) int64 {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, unit := range []struct {
		suffix string
		factor int64
	}{{"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10}, {"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(value, unit.suffix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(value, unit.suffix)), 10, 64)
			if err == nil && n <= int64(^uint64(0)>>1)/unit.factor {
				return n * unit.factor
			}
			return 0
		}
	}
	return 0
}

func rotateFile(path string, settings Kind) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	archive := path + "." + time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.Rename(path, archive); err != nil {
		return err
	}
	if settings.Keep == "none" {
		if err := os.Remove(archive); err != nil {
			return err
		}
		return nil
	}
	if settings.Compress {
		if err := compressFile(archive, logFileMode(settings.Name)); err != nil {
			return err
		}
	}
	return pruneArchives(path, settings.Keep)
}

func compressFile(path string, mode os.FileMode) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(path+".gz", os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(output)
	_, copyErr := io.Copy(writer, input)
	closeErr := writer.Close()
	fileErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if fileErr != nil {
		return fileErr
	}
	return os.Remove(path)
}

func logFileMode(kind string) os.FileMode {
	if kind == "security" {
		return 0600
	}
	return 0640
}

func pruneArchives(path, keep string) error {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	prefix := filepath.Base(path) + "."
	archives := make([]os.FileInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		archives = append(archives, info)
	}
	if keep == "forever" {
		return nil
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].ModTime().After(archives[j].ModTime()) })
	maxCount := -1
	maxAge := time.Duration(0)
	value := strings.ToLower(strings.TrimSpace(keep))
	parts := strings.Split(value, ":")
	for _, part := range parts {
		if part == "daily" || part == "weekly" || part == "monthly" || part == "yearly" {
			continue
		}
		if strings.HasSuffix(part, "d") || strings.HasSuffix(part, "w") || strings.HasSuffix(part, "m") {
			n, _ := strconv.Atoi(part[:len(part)-1])
			unit := part[len(part)-1]
			if unit == 'd' {
				maxAge = time.Duration(n) * 24 * time.Hour
			}
			if unit == 'w' {
				maxAge = time.Duration(n) * 7 * 24 * time.Hour
			}
			if unit == 'm' {
				maxAge = time.Duration(n) * 30 * 24 * time.Hour
			}
		} else {
			maxCount, _ = strconv.Atoi(part)
		}
	}
	now := time.Now()
	for index, info := range archives {
		remove := maxCount >= 0 && index >= maxCount
		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge {
			remove = true
		}
		if remove {
			if err := os.Remove(filepath.Join(filepath.Dir(path), info.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
