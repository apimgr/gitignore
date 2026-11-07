package server

import (
	"testing"
	"time"
)

func TestNewStatsCollectorStartsEmpty(t *testing.T) {
	s := newStatsCollector()
	total, last24h, active := s.snapshot(time.Now())
	if total != 0 || last24h != 0 || active != 0 {
		t.Errorf("fresh collector snapshot = (%d, %d, %d), want all zero", total, last24h, active)
	}
}

func TestStatsCollectorBeginIncrementsAndDecrements(t *testing.T) {
	s := newStatsCollector()
	now := time.Now()

	end1 := s.begin(now)
	end2 := s.begin(now)

	total, last24h, active := s.snapshot(now)
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if last24h != 2 {
		t.Errorf("last24h = %d, want 2", last24h)
	}
	if active != 2 {
		t.Errorf("active = %d, want 2", active)
	}

	end1()
	_, _, active = s.snapshot(now)
	if active != 1 {
		t.Errorf("active after one end() = %d, want 1", active)
	}

	end2()
	_, _, active = s.snapshot(now)
	if active != 0 {
		t.Errorf("active after both end() = %d, want 0", active)
	}

	// total is a lifetime counter and must not decrease.
	total, _, _ = s.snapshot(now)
	if total != 2 {
		t.Errorf("total after requests completed = %d, want still 2", total)
	}
}

func TestStatsCollectorLast24hWindowExcludesOldBuckets(t *testing.T) {
	s := newStatsCollector()
	base := time.Now()

	// Record a request 30 hours in the past: outside the rolling 24h window.
	s.begin(base.Add(-30 * time.Hour))
	// Record a request 1 hour in the past: inside the window.
	s.begin(base.Add(-1 * time.Hour))

	total, last24h, _ := s.snapshot(base)
	if total != 2 {
		t.Errorf("total = %d, want 2 (lifetime counts everything)", total)
	}
	if last24h != 1 {
		t.Errorf("last24h = %d, want 1 (stale 30h-old bucket excluded)", last24h)
	}
}

func TestStatsCollectorBucketReuseAcrossHours(t *testing.T) {
	s := newStatsCollector()
	base := time.Now().Truncate(time.Hour)

	// 24 hours later lands on the same bucket index; it must reset the stale
	// count rather than accumulate with the old hour's value.
	s.begin(base)
	s.begin(base.Add(24 * time.Hour))

	_, last24h, _ := s.snapshot(base.Add(24 * time.Hour))
	if last24h != 1 {
		t.Errorf("last24h after bucket reuse = %d, want 1 (old hour's count discarded)", last24h)
	}
}
