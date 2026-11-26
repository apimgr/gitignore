package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/diskcheck"
)

type retainedBackup struct {
	path string
	name string
	date time.Time
	size int64
}

type backupDeletion struct {
	filename string
	reason   string
}

func applyBackupRetention(directory string, retention config.BackupRetentionConfig, password string) ([]backupDeletion, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]retainedBackup, 0)
	var incremental []retainedBackup
	for _, entry := range entries {
		name := entry.Name()
		appBackup := strings.HasPrefix(name, projectName+"_backup_") || (strings.HasPrefix(name, projectName+"-") && strings.Contains(name, ".tar.gz"))
		if entry.IsDir() || !appBackup {
			continue
		}
		path := filepath.Join(directory, name)
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		item := retainedBackup{path: path, name: name, date: info.ModTime(), size: info.Size()}
		if strings.HasPrefix(name, projectName+"-daily.tar.gz") || strings.HasPrefix(name, projectName+"-hourly.tar.gz") {
			incremental = append(incremental, item)
			continue
		}
		if stamp, ok := backupNameTime(name); ok {
			item.date = stamp
		}
		files = append(files, item)
	}
	protectedBases := make(map[string]int)
	incrementalBases := make(map[string]string, len(incremental))
	unknownBase := false
	for _, item := range incremental {
		base, err := incrementalBaseBackup(item.path, password)
		if err != nil {
			unknownBase = true
			continue
		}
		protectedBases[base]++
		incrementalBases[item.path] = base
	}
	// Keep all archives until an unreadable legacy incremental is replaced. Its
	// base cannot be safely identified without its historical password.
	if unknownBase {
		return nil, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].date.After(files[j].date) })
	keep := make(map[string]bool)
	keepPeriod(files, keep, retention.KeepYearly, func(item retainedBackup) (string, bool) {
		if item.date.Month() != time.January || item.date.Day() != 1 {
			return "", false
		}
		return strconv.Itoa(item.date.Year()), true
	})
	keepPeriod(files, keep, retention.KeepMonthly, func(item retainedBackup) (string, bool) {
		if item.date.Day() != 1 {
			return "", false
		}
		return item.date.Format("2006-01"), true
	})
	keepPeriod(files, keep, retention.KeepWeekly, func(item retainedBackup) (string, bool) {
		if item.date.Weekday() != time.Sunday {
			return "", false
		}
		year, week := item.date.ISOWeek()
		return fmt.Sprintf("%04d-%02d", year, week), true
	})
	remaining := retention.MaxBackups
	if remaining < 1 {
		remaining = 1
	}
	var deleted []backupDeletion
	defer func() { recordBackupRetentionAudit(directory, deleted) }()
	for _, item := range files {
		if keep[item.name] {
			continue
		}
		if protectedBases[item.name] > 0 {
			keep[item.name] = true
			continue
		}
		if remaining > 0 {
			keep[item.name] = true
			remaining--
		} else if err := removeBackupArchive(item.path); err != nil {
			return deleted, err
		} else {
			deleted = append(deleted, backupDeletion{filename: item.name, reason: "count"})
		}
	}

	if capBytes, enabled, err := backupSizeCap(retention.MaxTotalSize, directory); err != nil {
		return deleted, err
	} else if enabled {
		all := make([]retainedBackup, 0, len(files)+len(incremental))
		for _, item := range append(files, incremental...) {
			if _, err := os.Stat(item.path); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return deleted, err
			}
			all = append(all, item)
		}
		var total int64
		for _, item := range all {
			total += item.size
		}
		// The hard size limit overrides period and count retention, but a full
		// archive remains while a retained incremental names it as its base.
		sort.Slice(all, func(i, j int) bool { return all[i].date.Before(all[j].date) })
		protectedBaseRetained := false
		for total > int64(capBytes) {
			removed := false
			for _, item := range all {
				if total <= int64(capBytes) {
					break
				}
				if _, err := os.Stat(item.path); os.IsNotExist(err) {
					continue
				}
				isIncremental := strings.HasPrefix(item.name, projectName+"-daily.tar.gz") || strings.HasPrefix(item.name, projectName+"-hourly.tar.gz")
				if !isIncremental && protectedBases[item.name] > 0 {
					protectedBaseRetained = true
					continue
				}
				if isIncremental && protectedBases[incrementalBases[item.path]] > 0 {
					protectedBaseRetained = true
					continue
				}
				if err := removeBackupArchive(item.path); err != nil {
					return deleted, err
				}
				total -= item.size
				if isIncremental {
					incremental = removeRetainedBackup(incremental, item.path)
					base := incrementalBases[item.path]
					if protectedBases[base] > 0 {
						protectedBases[base]--
					}
				}
				deleted = append(deleted, backupDeletion{filename: item.name, reason: "size"})
				removed = true
			}
			if !removed {
				break
			}
		}
		if total > int64(capBytes) && protectedBaseRetained {
			log.Printf("WARN backup.cap_exceeded_protected_base: cap_bytes=%d total_bytes=%d", capBytes, total)
		}
	}
	return deleted, nil
}

func removeBackupArchive(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := db.DeleteBackupMetadata(filepath.Base(path)); err != nil && !errors.Is(err, sql.ErrConnDone) {
		return err
	}
	return nil
}

func incrementalBaseBackup(path, password string) (string, error) {
	if blob, err := os.ReadFile(path); err == nil && bytes.HasPrefix(blob, []byte(backupMagic)) {
		header, headerErr := parseBackupHeader(blob)
		if headerErr == nil && header.baseBackup != "" {
			return header.baseBackup, nil
		}
	}
	archive, err := readBackupArchive(path, password)
	if err != nil {
		return "", err
	}
	staging, err := extractToStaging(archive)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	manifest, err := loadAndVerifyManifest(staging)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(manifest.BaseBackup)
	if manifest.Type != "incremental" || base == "" || filepath.Base(base) != base {
		return "", fmt.Errorf("incremental manifest has an invalid base_backup")
	}
	return base, nil
}

func removeRetainedBackup(items []retainedBackup, path string) []retainedBackup {
	for i, item := range items {
		if item.path == path {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func keepPeriod(files []retainedBackup, keep map[string]bool, limit int, keyFn func(retainedBackup) (string, bool)) {
	if limit <= 0 {
		return
	}
	seen := make(map[string]bool)
	for _, item := range files {
		key, ok := keyFn(item)
		if !ok || seen[key] {
			continue
		}
		if limit == 0 {
			return
		}
		keep[item.name] = true
		seen[key] = true
		limit--
	}
}

func backupNameTime(name string) (time.Time, bool) {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".enc"), ".tar.gz")
	stamp := strings.TrimPrefix(base, projectName+"_backup_")
	for _, layout := range []string{"2006-01-02_150405", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, stamp, time.UTC); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func backupSizeCap(value, directory string) (uint64, bool, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "0", "false", "no", "none", "disable", "disabled", "off":
		return 0, false, nil
	}
	volume, err := diskcheck.MeasureVolume(directory)
	if err != nil {
		return 0, false, err
	}
	if strings.HasSuffix(value, "%") {
		percent, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
		if err != nil || percent <= 0 || percent > 100 {
			return 0, false, fmt.Errorf("invalid server.backup.retention.max_total_size %q", value)
		}
		return uint64(float64(volume.TotalBytes) * percent / 100), true, nil
	}
	units := map[byte]uint64{'K': 1 << 10, 'M': 1 << 20, 'G': 1 << 30, 'T': 1 << 40}
	last := value[len(value)-1]
	if multiplier, ok := units[last]; ok {
		number, parseErr := strconv.ParseFloat(strings.TrimSpace(value[:len(value)-1]), 64)
		if parseErr != nil || number <= 0 {
			return 0, false, fmt.Errorf("invalid server.backup.retention.max_total_size %q", value)
		}
		return uint64(number * float64(multiplier)), true, nil
	}
	bytes, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid server.backup.retention.max_total_size %q", value)
	}
	return bytes, true, nil
}
