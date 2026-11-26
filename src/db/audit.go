package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
)

// AppendAuditEntry stores a JSONL audit event in the relational audit table.
func AppendAuditEntry(entry audit.Entry) error {
	stamp, err := time.Parse("2006-01-02T15:04:05.000Z", entry.Time)
	if err != nil {
		stamp, err = time.Parse(time.RFC3339Nano, entry.Time)
		if err != nil {
			return err
		}
	}
	var details any
	if len(entry.Details) > 0 {
		encoded, err := json.Marshal(entry.Details)
		if err != nil {
			return err
		}
		details = string(encoded)
	}
	level := "info"
	if entry.Category == "security" {
		level = "security"
	} else if entry.Severity == "warning" || entry.Severity == "warn" {
		level = "warning"
	} else if entry.Severity == "error" || entry.Severity == "critical" {
		level = "error"
	}
	success := 0
	if entry.Result == "success" {
		success = 1
	}
	return Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO audit_log(timestamp, level, category, action, actor_ip, target_type, target_id, details, success)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, stamp.UTC().Unix(), level, entry.Category, entry.Event,
			nullableAuditValue(entry.Actor.IP), nullableTargetType(entry.Target), nullableTargetID(entry.Target), details, success)
		return err
	})
}

func nullableAuditValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTargetType(target *audit.Target) any {
	if target == nil || target.Type == "" {
		return nil
	}
	return target.Type
}

func nullableTargetID(target *audit.Target) any {
	if target == nil || target.ID == "" {
		return nil
	}
	return target.ID
}
