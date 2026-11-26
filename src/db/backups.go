package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"
)

// RecordBackup stores verified backup metadata in the server database.
func RecordBackup(path string, size int64, checksum string) error {
	return RecordBackupAs(path, size, checksum, "manual")
}

// RecordBackupAs stores verified backup metadata with its creation type.
func RecordBackupAs(path string, size int64, checksum, backupType string) error {
	filename := filepath.Base(path)
	return Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO backups(filename, filepath, size_bytes, type, created_at, checksum)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(filename) DO UPDATE SET filepath = excluded.filepath, size_bytes = excluded.size_bytes,
type = excluded.type, created_at = excluded.created_at, checksum = excluded.checksum`, filename, path, size, backupType, time.Now().UTC().Unix(), checksum)
		return err
	})
}

// DeleteBackupMetadata removes the metadata row for a deleted archive.
func DeleteBackupMetadata(filename string) error {
	return Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM backups WHERE filename = ?", filepath.Base(filename))
		return err
	})
}
