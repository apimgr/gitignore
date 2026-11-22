package diskcheck

import (
	"context"
	"testing"
	"time"
)

func TestDisabledAndCancelledChecks(t *testing.T) {
	checker := New(Config{})
	volumes, err := checker.Check(context.Background())
	if err != nil || len(volumes) != 0 {
		t.Fatalf("disabled check = %v, %v", volumes, err)
	}
	checker = New(Config{Enabled: true, DataDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := checker.Check(ctx); err == nil {
		t.Fatal("cancelled check unexpectedly succeeded")
	}
}

func TestReportDeduplicatesRepeatsAndReportsRecovery(t *testing.T) {
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	notifications := 0
	var logs []string
	checker := New(Config{
		Enabled: true, ThresholdPercent: 85, Repeat: time.Hour,
		Now:    func() time.Time { return now },
		Notify: func(Event) { notifications++ },
		Log:    func(level, _ string, _ map[string]interface{}) { logs = append(logs, level) },
	})
	low := VolumeUsage{Path: "/data", VolumeID: "volume", UsedPercent: 90, FreeBytes: 10}
	checker.report([]VolumeUsage{low})
	checker.report([]VolumeUsage{low})
	if notifications != 1 || len(logs) != 1 || logs[0] != "WARN" {
		t.Fatalf("initial notification/logs = %d/%v", notifications, logs)
	}
	now = now.Add(2 * time.Hour)
	checker.report([]VolumeUsage{low})
	if notifications != 2 {
		t.Fatalf("repeat notifications = %d", notifications)
	}
	checker.report([]VolumeUsage{{Path: "/data", VolumeID: "volume", UsedPercent: 80}})
	if len(logs) != 3 || logs[2] != "INFO" {
		t.Fatalf("recovery logs = %v", logs)
	}
	checker.report([]VolumeUsage{low})
	if notifications != 3 {
		t.Fatalf("notification after recovery = %d", notifications)
	}
}

func TestEnabledCheckMeasuresUniqueVolumes(t *testing.T) {
	dir := t.TempDir()
	checker := New(Config{Enabled: true, DataDir: dir, LogDir: dir, BackupDir: dir, ThresholdPercent: 100})
	volumes, err := checker.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 1 || volumes[0].TotalBytes == 0 || volumes[0].VolumeID == "" {
		t.Fatalf("volume measurement = %+v", volumes)
	}
}
