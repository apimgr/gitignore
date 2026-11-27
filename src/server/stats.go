package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apimgr/gitignore/src/db"
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

// newStatsCollector restores the durable request counters when a database is
// available. Test and pre-database servers retain a zero-valued collector.
func newStatsCollector() *statsCollector {
	s := &statsCollector{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stored, err := db.LoadRequestStats(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrConnDone) {
			log.Printf("WARN request statistics restore failed: %v", err)
		}
		return s
	}
	s.total.Store(stored.Total)
	for hour, count := range stored.ByHour {
		idx := hour % int64(len(s.buckets))
		if idx < 0 {
			idx += int64(len(s.buckets))
		}
		s.bucketHour[idx] = hour
		s.buckets[idx] = count
	}
	return s
}

// begin records the start of a request: it increments the lifetime total, the
// current-hour bucket, and the active-connection gauge. The returned function
// must be deferred to decrement the active gauge when the request completes.
func (s *statsCollector) begin(now time.Time) func() {
	return s.beginRequest(now, true)
}

func (s *statsCollector) beginRequest(now time.Time, count bool) func() {
	s.active.Add(1)
	if count {
		s.total.Add(1)
		hour := now.Unix() / 3600
		idx := int(hour % int64(len(s.buckets)))
		s.mu.Lock()
		if s.bucketHour[idx] != hour {
			s.bucketHour[idx] = hour
			s.buckets[idx] = 0
		}
		s.buckets[idx]++
		s.mu.Unlock()
	}
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

func (s *statsCollector) persistentSnapshot(now time.Time) db.RequestStats {
	currentHour := now.Unix() / 3600
	result := db.RequestStats{Total: s.total.Load(), ByHour: make(map[int64]int64, 24)}
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx, hour := range s.bucketHour {
		if hour >= currentHour-23 && hour <= currentHour {
			result.ByHour[hour] = s.buckets[idx]
		}
	}
	return result
}

func (s *statsCollector) flush(ctx context.Context, now time.Time) error {
	return db.SaveRequestStats(ctx, s.persistentSnapshot(now), now.Unix()/3600)
}

// runPersistence writes aggregate counters at most once per minute and once
// more during graceful shutdown.
func (s *statsCollector) runPersistence(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.flush(flushCtx, time.Now().UTC()); err != nil && !errors.Is(err, sql.ErrConnDone) {
				log.Printf("WARN request statistics shutdown flush failed: %v", err)
			}
			cancel()
			return
		case now := <-ticker.C:
			flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := s.flush(flushCtx, now.UTC()); err != nil && !errors.Is(err, sql.ErrConnDone) {
				log.Printf("WARN request statistics flush failed: %v", err)
			}
			cancel()
		}
	}
}

func (s *statsCollector) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := !isHealthOrMetricsRequest(r.URL.Path)
		defer s.beginRequest(time.Now().UTC(), count)()
		next.ServeHTTP(w, r)
	})
}

func isHealthOrMetricsRequest(path string) bool {
	clean := strings.TrimSuffix(path, "/")
	if clean == "" {
		return false
	}
	for _, suffix := range []string{"/healthz", "/healthz.txt", "/server/metrics"} {
		if clean == suffix || strings.HasSuffix(clean, suffix) {
			return true
		}
	}
	if clean == "/metrics" || clean == "/api/metrics" || strings.HasPrefix(clean, "/api/metrics/") || strings.HasSuffix(clean, "/metrics") {
		return true
	}
	return false
}
