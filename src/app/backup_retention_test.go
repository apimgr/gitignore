package app

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

func TestApplyBackupRetentionKeepsNewestAndWeekly(t *testing.T) {
	directory := t.TempDir()
	names := []string{
		"gitignore_backup_2026-01-04.tar.gz",
		"gitignore_backup_2026-01-11.tar.gz",
		"gitignore_backup_2026-01-12.tar.gz",
		"gitignore_backup_2026-01-13.tar.gz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1, KeepWeekly: 1}, ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		_, err := os.Stat(filepath.Join(directory, name))
		wantKept := name == "gitignore_backup_2026-01-11.tar.gz" || name == "gitignore_backup_2026-01-13.tar.gz"
		if wantKept && err != nil {
			t.Errorf("expected %s to be retained: %v", name, err)
		}
		if !wantKept && !os.IsNotExist(err) {
			t.Errorf("expected %s to be pruned, stat error=%v", name, err)
		}
	}
}

func TestBackupNameTimeSupportsManualTimestamp(t *testing.T) {
	got, ok := backupNameTime("gitignore_backup_2026-04-05_060708.tar.gz.enc")
	want := time.Date(2026, time.April, 5, 6, 7, 8, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("backupNameTime() = %v, %v; want %v", got, ok, want)
	}
}

func TestApplyBackupRetentionPrunesUnclassifiedApplicationArchives(t *testing.T) {
	directory := t.TempDir()
	oldPath := filepath.Join(directory, "gitignore-custom.tar.gz.enc")
	newPath := filepath.Join(directory, "gitignore_backup_"+time.Now().UTC().Format("2006-01-02")+".tar.gz")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("unclassified application archive was not pruned: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("newest full archive was pruned: %v", err)
	}
}

func TestBackupSizeCapDoesNotCountFilesRemovedByCountRetention(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"gitignore_backup_2026-01-01.tar.gz", "gitignore_backup_2026-01-02.tar.gz"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("0123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "15"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].filename != "gitignore_backup_2026-01-01.tar.gz" {
		t.Fatalf("deleted = %#v, want only the count-pruned oldest archive", deleted)
	}
	if _, err := os.Stat(filepath.Join(directory, "gitignore_backup_2026-01-02.tar.gz")); err != nil {
		t.Fatalf("count-retained archive was unnecessarily removed by size cap: %v", err)
	}
}

func TestApplyBackupRetentionProtectsOnlyReferencedIncrementalBase(t *testing.T) {
	directory := t.TempDir()
	baseName := "gitignore_backup_2026-01-01.tar.gz"
	otherName := "gitignore_backup_2026-01-02.tar.gz"
	newestName := "gitignore_backup_2026-01-03.tar.gz"
	baseChecksum := writeRetentionArchive(t, filepath.Join(directory, baseName), "full", "", "", false, "")
	writeRetentionArchive(t, filepath.Join(directory, otherName), "full", "", "", false, "")
	writeRetentionArchive(t, filepath.Join(directory, newestName), "full", "", "", false, "")
	writeRetentionArchive(t, filepath.Join(directory, "gitignore-daily.tar.gz"), "incremental", baseName, baseChecksum, false, "")

	deleted, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].filename != otherName {
		t.Fatalf("deleted = %#v, want only the unrelated middle full", deleted)
	}
	for _, name := range []string{baseName, newestName, "gitignore-daily.tar.gz"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("required archive %s was removed: %v", name, err)
		}
	}

	if err := os.Remove(filepath.Join(directory, "gitignore-daily.tar.gz")); err != nil {
		t.Fatal(err)
	}
	if _, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, baseName)); !os.IsNotExist(err) {
		t.Errorf("unreferenced base was retained: stat error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, newestName)); err != nil {
		t.Errorf("newest full was not retained: %v", err)
	}
}

func TestBackupSizeCapLogsWhenProtectedPairExceedsCap(t *testing.T) {
	directory := t.TempDir()
	baseName := "gitignore_backup_2026-02-01.tar.gz"
	baseChecksum := writeRetentionArchive(t, filepath.Join(directory, baseName), "full", "", "", false, "")
	incrementalName := "gitignore-daily.tar.gz"
	writeRetentionArchive(t, filepath.Join(directory, incrementalName), "incremental", baseName, baseChecksum, false, "")
	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	deleted, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %#v, want protected base and incremental retained", deleted)
	}
	if !strings.Contains(output.String(), "WARN backup.cap_exceeded_protected_base") {
		t.Fatalf("retention warning = %q", output.String())
	}
	for _, name := range []string{baseName, incrementalName} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("protected archive %s missing: %v", name, err)
		}
	}
}

func TestApplyBackupRetentionReadsEncryptedIncrementalBase(t *testing.T) {
	directory := t.TempDir()
	password := "correct horse battery staple"
	baseName := "gitignore_backup_2026-02-01.tar.gz.enc"
	baseChecksum := writeRetentionArchive(t, filepath.Join(directory, baseName), "full", "", "", true, password)
	writeRetentionArchive(t, filepath.Join(directory, "gitignore-daily.tar.gz.enc"), "incremental", baseName, baseChecksum, true, password)
	if _, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1}, password); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, baseName)); err != nil {
		t.Fatalf("encrypted incremental base was not protected: %v", err)
	}
}

func TestApplyBackupRetentionPasswordRotationUsesHeaderReference(t *testing.T) {
	directory := t.TempDir()
	oldPassword := "old password"
	baseName := "gitignore_backup_2026-02-01.tar.gz.enc"
	baseChecksum := writeRetentionArchive(t, filepath.Join(directory, baseName), "full", "", "", true, oldPassword)
	unrelatedName := "gitignore_backup_2026-02-02.tar.gz.enc"
	writeRetentionArchive(t, filepath.Join(directory, unrelatedName), "full", "", "", true, oldPassword)
	newestName := "gitignore_backup_2026-02-03.tar.gz.enc"
	writeRetentionArchive(t, filepath.Join(directory, newestName), "full", "", "", true, oldPassword)
	writeRetentionArchive(t, filepath.Join(directory, "gitignore-daily.tar.gz.enc"), "incremental", baseName, baseChecksum, true, oldPassword)
	deleted, err := applyBackupRetention(directory, config.BackupRetentionConfig{MaxBackups: 1}, "new password")
	if err != nil {
		t.Fatalf("retention blocked password rotation: %v", err)
	}
	if len(deleted) != 1 || deleted[0].filename != unrelatedName {
		t.Fatalf("rotation retention deletions = %#v, want only %s", deleted, unrelatedName)
	}
	if _, err := os.Stat(filepath.Join(directory, baseName)); err != nil {
		t.Fatalf("old incremental base was pruned during rotation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, newestName)); err != nil {
		t.Fatalf("newest full backup was pruned during rotation: %v", err)
	}
}

func TestScheduledFullBackupNameNeverReusesSameDayName(t *testing.T) {
	directory := t.TempDir()
	when := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	first := scheduledFullBackupName(directory, when)
	if first != "gitignore_backup_2026-06-01.tar.gz" {
		t.Fatalf("first name = %q", first)
	}
	if err := os.WriteFile(filepath.Join(directory, first), []byte("manual backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := scheduledFullBackupName(directory, when)
	if second == first || !strings.Contains(second, "2026-06-01_120000") {
		t.Fatalf("second same-day full name = %q", second)
	}
}

func writeRetentionArchive(t *testing.T, path, archiveType, baseName, baseChecksum string, encrypted bool, password string) string {
	t.Helper()
	archive, err := buildBackupArchiveAt(nil, encrypted, time.Now().UTC(), archiveType, baseName, baseChecksum, nil)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted {
		if archiveType == "incremental" {
			archive, err = encryptBackup(archive, password, baseName)
		} else {
			archive, err = encryptBackup(archive, password)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	checksum, err := checksumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return checksum
}
