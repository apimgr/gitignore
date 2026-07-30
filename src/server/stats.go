package server

import (
	"sync"
	"sync/atomic"
	"time"
)

// statsCollector tracks public-safe aggregate request statistics for the health
// endpoint (AI.md PART 13 "stats"). It stores only counters — never any
// per-request identifying data — so every value it exposes is safe to return in
// an unauthenticated health response.
type statsCollector struct {
	total  atomic.Int64
	active atomic.Int64

	mu sync.Mutex
	// buckets holds one request count per hour for a rolling 24-hour window.
	buckets [24]int64
	// bucketHour records which absolute unix-hour each bucket currently counts,
	// so a stale bucket is reset before reuse rather than double-counted.
	bucketHour [24]int64
}

// newStatsCollector returns an empty collector.
func newStatsCollector() *statsCollector { return &statsCollector{} }

// begin records the start of a request: it increments the lifetime total, the
// current-hour bucket, and the active-connection gauge. The returned function
// must be deferred to decrement the active gauge when the request completes.
func (s *statsCollector) begin(now time.Time) func() {
	s.total.Add(1)
	s.active.Add(1)

	hour := now.Unix() / 3600
	idx := int(hour % 24)
	s.mu.Lock()
	if s.bucketHour[idx] != hour {
		s.bucketHour[idx] = hour
		s.buckets[idx] = 0
	}
	s.buckets[idx]++
	s.mu.Unlock()

	return func() { s.active.Add(-1) }
}

// snapshot returns the lifetime total, the rolling 24-hour request count, and
// the current active-connection count.
func (s *statsCollector) snapshot(now time.Time) (total, last24h int64, active int) {
	total = s.total.Load()
	active = int(s.active.Load())

	currentHour := now.Unix() / 3600
	s.mu.Lock()
	for i := 0; i < 24; i++ {
		if currentHour-s.bucketHour[i] < 24 {
			last24h += s.buckets[i]
		}
	}
	s.mu.Unlock()
	return total, last24h, active
}
