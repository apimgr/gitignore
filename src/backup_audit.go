package main

import (
	"log"
	"os"
	"strings"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/config"
)

func appendBackupAudit(event, severity, result string, details map[string]any) {
	dir := config.AuditLogDir()
	if dir == "" {
		return
	}
	entry := audit.Entry{
		Event: event, Category: "system", Severity: severity,
		Actor:  audit.Actor{Type: "system"},
		Target: &audit.Target{Type: "backup"}, Details: details, Result: result,
	}
	if err := audit.Append(dir, entry); err != nil {
		log.Printf("audit event %s could not be appended", event)
	}
}

func recordBackupRetentionAudit(directory string, deleted []backupDeletion) {
	if len(deleted) == 0 {
		return
	}
	filenames := make([]string, 0, len(deleted))
	reasons := make(map[string][]string)
	for _, item := range deleted {
		filenames = append(filenames, item.filename)
		reasons[item.reason] = append(reasons[item.reason], item.filename)
		appendBackupAudit("backup.deleted", "info", "success", map[string]any{"filename": item.filename, "reason": item.reason})
	}
	remaining := 0
	if entries, err := os.ReadDir(directory); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && (strings.HasPrefix(name, projectName+"_backup_") || (strings.HasPrefix(name, projectName+"-") && strings.Contains(name, ".tar.gz"))) {
				remaining++
			}
		}
	}
	appendBackupAudit("backup.retention_cleanup", "info", "success", map[string]any{
		"deleted_files": filenames, "reasons": reasons, "remaining_count": remaining,
	})
}
