package audit

import (
	"bufio"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Actor struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	IP   string `json:"ip,omitempty"`
}

type Target struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

type Entry struct {
	ID       string         `json:"id"`
	Time     string         `json:"time"`
	Event    string         `json:"event"`
	Category string         `json:"category"`
	Severity string         `json:"severity"`
	Actor    Actor          `json:"actor"`
	Target   *Target        `json:"target,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
	Result   string         `json:"result"`
	Prev     string         `json:"prev"`
	Source   string         `json:"source,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

var appendMu sync.Mutex
var databaseSink func(Entry) error
var pendingDatabaseEntries []Entry

// SetDatabaseSink enables the relational audit copy and drains events emitted
// before the database became available.
func SetDatabaseSink(sink func(Entry) error) error {
	appendMu.Lock()
	defer appendMu.Unlock()
	if databaseSink != nil {
		_ = flushPendingDatabaseEntries(databaseSink)
	}
	databaseSink = sink
	if sink == nil {
		return nil
	}
	for len(pendingDatabaseEntries) > 0 {
		if err := sink(pendingDatabaseEntries[0]); err != nil {
			if errors.Is(err, sql.ErrConnDone) {
				return nil
			}
			return err
		}
		pendingDatabaseEntries = pendingDatabaseEntries[1:]
	}
	return nil
}

// Append writes one AI.md-compliant JSON Lines event to the audit log.
func Append(logsDir string, entry Entry) error {
	if strings.TrimSpace(logsDir) == "" {
		return fmt.Errorf("audit log directory is empty")
	}
	if entry.Event == "" || entry.Category == "" || entry.Severity == "" || entry.Result == "" {
		return fmt.Errorf("audit event is missing a required field")
	}
	now := time.Now().UTC()
	if entry.ID == "" {
		id, err := newID(now)
		if err != nil {
			return fmt.Errorf("generate audit ID: %w", err)
		}
		entry.ID = id
	}
	if entry.Time == "" {
		entry.Time = now.Format("2006-01-02T15:04:05.000Z")
	}
	if entry.Actor.Type == "" {
		entry.Actor.Type = "system"
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return fmt.Errorf("create audit log directory: %w", err)
	}
	path := filepath.Join(logsDir, "audit.log")
	prev, err := previousHash(path, filepath.Join(logsDir, "audit.anchor"))
	if err != nil {
		return fmt.Errorf("read prior audit entry: %w", err)
	}
	entry.Prev = prev
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Chmod(0640); err != nil {
		return fmt.Errorf("protect audit log: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync audit event: %w", err)
	}
	if databaseSink == nil {
		pendingDatabaseEntries = append(pendingDatabaseEntries, entry)
		return nil
	}
	if len(pendingDatabaseEntries) > 0 {
		if err := flushPendingDatabaseEntries(databaseSink); err != nil {
			pendingDatabaseEntries = append(pendingDatabaseEntries, entry)
			return fmt.Errorf("persist audit event to database: %w", err)
		}
	}
	if err := databaseSink(entry); err != nil {
		pendingDatabaseEntries = append(pendingDatabaseEntries, entry)
		return fmt.Errorf("persist audit event to database: %w", err)
	}
	return nil
}

func flushPendingDatabaseEntries(sink func(Entry) error) error {
	for len(pendingDatabaseEntries) > 0 {
		if err := sink(pendingDatabaseEntries[0]); err != nil {
			return err
		}
		pendingDatabaseEntries = pendingDatabaseEntries[1:]
	}
	return nil
}

// Verify checks the current JSONL file's exact-line hash chain. The returned
// error names only the file and line number, never the event contents.
func Verify(logsDir string) (count int, resultErr error) {
	path := filepath.Join(logsDir, "audit.log")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	anchor, err := readAnchor(filepath.Join(logsDir, "audit.anchor"))
	if err != nil {
		return 0, err
	}
	expected := anchor
	if expected == "" {
		expected = strings.Repeat("0", 64)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var item struct {
			Prev string `json:"prev"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil || item.Prev != expected {
			return count, fmt.Errorf("%s:%d", filepath.Base(path), line)
		}
		digest := sha256.Sum256(scanner.Bytes())
		expected = hex.EncodeToString(digest[:])
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, err
	}
	return count, nil
}

// VerifyAndReport appends one critical chain-integrity event for a distinct
// broken file/line finding while allowing callers to keep the server running.
func VerifyAndReport(logsDir string) (int, error) {
	count, err := Verify(logsDir)
	if err == nil {
		if count == 0 {
			_, logErr := os.Stat(filepath.Join(logsDir, "audit.log"))
			_, anchorErr := os.Stat(filepath.Join(logsDir, "audit.anchor"))
			entries, readErr := os.ReadDir(logsDir)
			if os.IsNotExist(logErr) && os.IsNotExist(anchorErr) && readErr == nil {
				for _, entry := range entries {
					if !entry.IsDir() && strings.HasPrefix(entry.Name(), "audit.log.") {
						_ = Append(logsDir, Entry{Event: "security.audit_chain_reset", Category: "security", Severity: "info", Actor: Actor{Type: "system"}, Details: map[string]any{"reason": "operator_reset"}, Result: "success", Source: "system"})
						break
					}
				}
			}
		}
		return count, nil
	}
	parts := strings.SplitN(err.Error(), ":", 2)
	fileName, lineNumber := "audit.log", 0
	if len(parts) == 2 {
		fileName = parts[0]
		_, _ = fmt.Sscanf(parts[1], "%d", &lineNumber)
	}
	reason := "chain_mismatch"
	if lineNumber == 1 && fileName == "audit.log" {
		anchor, anchorErr := readAnchor(filepath.Join(logsDir, "audit.anchor"))
		if anchorErr == nil && anchor == "" {
			if data, readErr := os.ReadFile(filepath.Join(logsDir, "audit.log")); readErr == nil {
				line := []byte(strings.SplitN(string(data), "\n", 2)[0])
				var first struct {
					Prev string `json:"prev"`
				}
				if json.Unmarshal(line, &first) == nil && first.Prev != strings.Repeat("0", 64) {
					reason = "anchor_missing"
				}
			}
		} else if anchorErr == nil {
			reason = "anchor_mismatch"
		}
	}
	if data, readErr := os.ReadFile(filepath.Join(logsDir, "audit.log")); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			var item Entry
			if json.Unmarshal([]byte(line), &item) != nil || item.Event != "security.audit_chain_broken" {
				continue
			}
			if item.Details["file"] == fileName && fmt.Sprint(item.Details["line"]) == fmt.Sprint(lineNumber) && item.Details["reason"] == reason {
				return count, err
			}
		}
	}
	_ = Append(logsDir, Entry{Event: "security.audit_chain_broken", Category: "security", Severity: "critical", Actor: Actor{Type: "system"}, Details: map[string]any{"reason": reason, "file": fileName, "line": lineNumber}, Result: "failure", Source: "system"})
	return count, err
}

// CountEventsSince returns compliance events newer than since, grouped by
// event name, without exposing event payloads.
func CountEventsSince(logsDir string, since time.Time) (map[string]int, error) {
	counts := map[string]int{}
	entries, err := os.ReadDir(logsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name != "audit.log" && !strings.HasPrefix(name, "audit.log.") {
			continue
		}
		path := filepath.Join(logsDir, name)
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		var reader io.Reader = file
		var zipped *gzip.Reader
		if strings.HasSuffix(name, ".gz") {
			zipped, err = gzip.NewReader(file)
			if err != nil {
				if closeErr := file.Close(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
				return nil, err
			}
			reader = zipped
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var item Entry
			if json.Unmarshal(scanner.Bytes(), &item) != nil || !strings.HasPrefix(item.Event, "compliance.") {
				continue
			}
			stamp, parseErr := time.Parse("2006-01-02T15:04:05.000Z", item.Time)
			if parseErr == nil && !stamp.Before(since) {
				counts[item.Event]++
			}
		}
		scanErr := scanner.Err()
		if zipped != nil {
			if closeErr := zipped.Close(); closeErr != nil && scanErr == nil {
				scanErr = closeErr
			}
		}
		if closeErr := file.Close(); closeErr != nil && scanErr == nil {
			scanErr = closeErr
		}
		if scanErr != nil {
			return nil, scanErr
		}
	}
	return counts, nil
}

// Rotate verifies and closes the active audit log, records its final hash in
// the durable anchor, then applies the configured retention to the closed log.
func Rotate(logsDir, keep string, compress bool) error {
	if _, err := Verify(logsDir); err != nil {
		_, _ = VerifyAndReport(logsDir)
		return err
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	path := filepath.Join(logsDir, "audit.log")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	lastLine := strings.TrimSuffix(string(data), "\n")
	if index := strings.LastIndex(lastLine, "\n"); index >= 0 {
		lastLine = lastLine[index+1:]
	}
	lastHash := sha256.Sum256([]byte(lastLine))
	anchor := filepath.Join(logsDir, "audit.anchor")
	archive := filepath.Join(logsDir, "audit.log."+time.Now().UTC().Format("2006-01-02"))
	if _, err := os.Stat(archive); err == nil {
		archive += "." + time.Now().UTC().Format("150405")
	}
	if err := os.Rename(path, archive); err != nil {
		return err
	}
	if err := writeAnchor(anchor, hex.EncodeToString(lastHash[:])); err != nil {
		_ = os.Rename(archive, path)
		return err
	}
	if compress && keep != "none" && keep != "" {
		if err := compressAuditArchive(archive); err != nil {
			return err
		}
		archive += ".gz"
	}
	duration, maxFiles, retain := auditRetention(keep)
	if !retain {
		return os.Remove(archive)
	}
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return err
	}
	var archives []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "audit.log.") {
			archives = append(archives, entry)
		}
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].Name() > archives[j].Name() })
	for index, entry := range archives {
		if maxFiles > 0 && index >= maxFiles {
			_ = os.Remove(filepath.Join(logsDir, entry.Name()))
			continue
		}
		if duration == 0 {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(time.Now().Add(-duration)) {
			_ = os.Remove(filepath.Join(logsDir, entry.Name()))
		}
	}
	return nil
}

func writeAnchor(path, value string) error {
	temporary := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(value + "\n")
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Chmod(temporary, 0600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func auditRetention(value string) (time.Duration, int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "keep:")
	if value == "" || value == "none" {
		return 0, 0, false
	}
	if value == "forever" {
		return 0, 0, true
	}
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		if len(parts) == 2 && (parts[0] == "weekly" || parts[0] == "monthly") {
			var count int
			if _, err := fmt.Sscanf(parts[1], "%d", &count); err == nil && count > 0 {
				return 0, count, true
			}
		}
	}
	var count int
	if _, err := fmt.Sscanf(value, "%d", &count); err == nil && count > 0 && fmt.Sprint(count) == value {
		return 0, count, true
	}
	if strings.HasSuffix(value, "d") {
		var days int
		if _, err := fmt.Sscanf(strings.TrimSuffix(value, "d"), "%d", &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, 0, true
		}
	}
	if strings.HasSuffix(value, "w") {
		var weeks int
		if _, err := fmt.Sscanf(strings.TrimSuffix(value, "w"), "%d", &weeks); err == nil && weeks > 0 {
			return time.Duration(weeks) * 7 * 24 * time.Hour, 0, true
		}
	}
	if strings.HasSuffix(value, "m") {
		var months int
		if _, err := fmt.Sscanf(strings.TrimSuffix(value, "m"), "%d", &months); err == nil && months > 0 {
			return time.Duration(months) * 30 * 24 * time.Hour, 0, true
		}
	}
	return 0, 0, true
}

func compressAuditArchive(path string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(path+".gz", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(output)
	_, copyErr := io.Copy(writer, input)
	closeGzipErr := writer.Close()
	closeFileErr := output.Close()
	closeInputErr := input.Close()
	if copyErr != nil {
		_ = os.Remove(path + ".gz")
		return copyErr
	}
	if closeGzipErr != nil {
		_ = os.Remove(path + ".gz")
		return closeGzipErr
	}
	if closeFileErr != nil {
		_ = os.Remove(path + ".gz")
		return closeFileErr
	}
	if closeInputErr != nil {
		_ = os.Remove(path + ".gz")
		return closeInputErr
	}
	return os.Remove(path)
}

func previousHash(path, anchorPath string) (previousHash string, resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		anchor, anchorErr := readAnchor(anchorPath)
		if anchorErr != nil {
			return "", anchorErr
		}
		if anchor != "" {
			return anchor, nil
		}
		return strings.Repeat("0", 64), nil
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	reader := bufio.NewReader(file)
	var previous []byte
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			previous = line
		}
		if readErr != nil {
			if readErr != io.EOF {
				return "", readErr
			}
			break
		}
	}
	if len(previous) == 0 {
		anchor, anchorErr := readAnchor(anchorPath)
		if anchorErr != nil {
			return "", anchorErr
		}
		if anchor != "" {
			return anchor, nil
		}
		return strings.Repeat("0", 64), nil
	}
	previous = []byte(strings.TrimSuffix(string(previous), "\n"))
	digest := sha256.Sum256(previous)
	return hex.EncodeToString(digest[:]), nil
}

func readAnchor(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if len(value) != 64 || value != strings.ToLower(value) {
		return "", fmt.Errorf("%s: invalid audit anchor", filepath.Base(path))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("%s: invalid audit anchor", filepath.Base(path))
	}
	return value, nil
}

func newID(now time.Time) (string, error) {
	var raw [16]byte
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}
	// A 26-character Crockford base32 representation is the canonical ULID form.
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var out [26]byte
	// ULID is a 128-bit value represented in 130 bits; the leading two bits are zero.
	for i := 0; i < len(out); i++ {
		var chunk byte
		for bit := 0; bit < 5; bit++ {
			index := i*5 + bit
			chunk <<= 1
			if index >= 2 {
				index -= 2
				if raw[index/8]&(1<<uint(7-index%8)) != 0 {
					chunk |= 1
				}
			}
		}
		out[i] = alphabet[chunk]
	}
	return "audit_" + string(out[:]), nil
}
