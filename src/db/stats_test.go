package db

import (
	"context"
	"testing"
	"time"
)

func TestRequestStatsPersistAcrossDatabaseLifecycle(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	dataDir := t.TempDir()
	if err := Init(dataDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	currentHour := time.Now().UTC().Unix() / 3600
	want := RequestStats{Total: 42, ByHour: map[int64]int64{currentHour - 1: 4, currentHour: 7}}
	if err := SaveRequestStats(context.Background(), want, currentHour); err != nil {
		t.Fatalf("SaveRequestStats: %v", err)
	}
	got, err := LoadRequestStats(context.Background())
	if err != nil {
		t.Fatalf("LoadRequestStats: %v", err)
	}
	if got.Total != want.Total || len(got.ByHour) != len(want.ByHour) {
		t.Fatalf("loaded request stats = %+v, want %+v", got, want)
	}
	for hour, count := range want.ByHour {
		if got.ByHour[hour] != count {
			t.Errorf("hour %d count = %d, want %d", hour, got.ByHour[hour], count)
		}
	}
}

func TestSaveRequestStatsRejectsInvalidValues(t *testing.T) {
	currentHour := time.Now().UTC().Unix() / 3600
	for _, stats := range []RequestStats{
		{Total: -1, ByHour: map[int64]int64{}},
		{Total: 1, ByHour: map[int64]int64{currentHour: -1}},
		{Total: 1, ByHour: map[int64]int64{currentHour - 24: 1}},
	} {
		if err := SaveRequestStats(context.Background(), stats, currentHour); err == nil {
			t.Errorf("SaveRequestStats(%+v) succeeded, want validation error", stats)
		}
	}
}
