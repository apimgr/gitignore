package audit

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Source   string         `json:"source,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

var appendMu sync.Mutex

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
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return fmt.Errorf("create audit log directory: %w", err)
	}
	path := filepath.Join(logsDir, "audit.log")
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
	return nil
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
