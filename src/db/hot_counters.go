package db

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

const hotCounterFlushInterval = 30 * time.Second

type hotRateEvent struct {
	key      string
	occurred int64
}

var hotCounters = struct {
	sync.Mutex
	events           map[string][]int64
	pending          map[string][]int64
	tokenUsed        map[int64]int64
	pendingTokenUsed map[int64]int64
	stop             chan struct{}
	done             chan struct{}
	pruneAt          time.Time
}{events: map[string][]int64{}, pending: map[string][]int64{}, tokenUsed: map[int64]int64{}, pendingTokenUsed: map[int64]int64{}}

func loadRateLimitEvents(ctx context.Context, reader *sql.DB) error {
	cutoff := time.Now().UTC().Add(-time.Hour).UnixMilli()
	rows, err := reader.QueryContext(ctx, "SELECT key, occurred_at FROM rate_limit_events WHERE occurred_at > ? ORDER BY key, occurred_at", cutoff)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	events := map[string][]int64{}
	for rows.Next() {
		var key string
		var occurred int64
		if err := rows.Scan(&key, &occurred); err != nil {
			return err
		}
		events[key] = append(events[key], occurred)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	hotCounters.Lock()
	hotCounters.events = events
	hotCounters.pending = map[string][]int64{}
	hotCounters.Unlock()
	return nil
}

func loadHotTokenUses(ctx context.Context, reader *sql.DB) error {
	rows, err := reader.QueryContext(ctx, "SELECT id, COALESCE(last_used_at, 0) FROM api_tokens")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	used := map[int64]int64{}
	for rows.Next() {
		var id, stamp int64
		if err := rows.Scan(&id, &stamp); err != nil {
			return err
		}
		used[id] = stamp
	}
	if err := rows.Err(); err != nil {
		return err
	}
	hotCounters.Lock()
	hotCounters.tokenUsed = used
	hotCounters.pendingTokenUsed = map[int64]int64{}
	hotCounters.Unlock()
	return nil
}

func startHotCounterFlusher() {
	hotCounters.Lock()
	if hotCounters.stop != nil {
		hotCounters.Unlock()
		return
	}
	hotCounters.stop = make(chan struct{})
	hotCounters.done = make(chan struct{})
	stop, done := hotCounters.stop, hotCounters.done
	hotCounters.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(hotCounterFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := FlushHotCounters(context.Background()); err != nil {
					log.Printf("database: failed to flush hot counters: %v", err)
				}
			case <-stop:
				if err := FlushHotCounters(context.Background()); err != nil {
					log.Printf("database: failed to flush hot counters at shutdown: %v", err)
				}
				return
			}
		}
	}()
}

func stopHotCounterFlusher() {
	hotCounters.Lock()
	stop, done := hotCounters.stop, hotCounters.done
	hotCounters.stop, hotCounters.done = nil, nil
	hotCounters.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

func appendHotRateEvent(key string, now int64, windowSeconds int) ([]int64, error) {
	mu.RLock()
	ready := conn != nil
	mu.RUnlock()
	if !ready {
		return nil, sql.ErrConnDone
	}
	retentionCutoff := now - int64(time.Hour/time.Millisecond)
	hotCounters.Lock()
	defer hotCounters.Unlock()
	pruneHotRateEventsLocked(now)
	events := hotCounters.events[key]
	retained := events[:0]
	for _, occurred := range events {
		if occurred > retentionCutoff {
			retained = append(retained, occurred)
		}
	}
	retained = append(retained, now)
	hotCounters.events[key] = retained
	hotCounters.pending[key] = append(hotCounters.pending[key], now)
	cutoff := now - int64(windowSeconds)*1000
	active := make([]int64, 0, len(retained))
	for _, occurred := range retained {
		if occurred > cutoff {
			active = append(active, occurred)
		}
	}
	return active, nil
}

func queueOwnerTokenUse(tokenID int64, now time.Time) error {
	mu.RLock()
	ready := conn != nil
	mu.RUnlock()
	if !ready {
		return sql.ErrConnDone
	}
	stamp := now.UTC().Unix()
	hotCounters.Lock()
	defer hotCounters.Unlock()
	last := hotCounters.tokenUsed[tokenID]
	if pending := hotCounters.pendingTokenUsed[tokenID]; pending > last {
		last = pending
	}
	if last > 0 && last > stamp-60 {
		return nil
	}
	hotCounters.tokenUsed[tokenID] = stamp
	hotCounters.pendingTokenUsed[tokenID] = stamp
	return nil
}

func activeHotRateEvents(key string, now int64, windowSeconds int) ([]int64, error) {
	mu.RLock()
	ready := conn != nil
	mu.RUnlock()
	if !ready {
		return nil, sql.ErrConnDone
	}
	cutoff := now - int64(windowSeconds)*1000
	hotCounters.Lock()
	defer hotCounters.Unlock()
	pruneHotRateEventsLocked(now)
	events := hotCounters.events[key]
	active := events[:0]
	for _, occurred := range events {
		if occurred > cutoff {
			active = append(active, occurred)
		}
	}
	if len(active) == 0 {
		delete(hotCounters.events, key)
	} else {
		hotCounters.events[key] = active
	}
	return append([]int64(nil), active...), nil
}

func pruneHotRateEventsLocked(now int64) {
	if current := time.UnixMilli(now); current.Before(hotCounters.pruneAt) {
		return
	}
	hotCounters.pruneAt = time.UnixMilli(now).Add(hotCounterFlushInterval)
	cutoff := now - int64(time.Hour/time.Millisecond)
	for key, events := range hotCounters.events {
		retained := pruneRateEvents(events, cutoff)
		if len(retained) == 0 {
			delete(hotCounters.events, key)
		} else {
			hotCounters.events[key] = retained
		}
	}
	for key, events := range hotCounters.pending {
		retained := pruneRateEvents(events, cutoff)
		if len(retained) == 0 {
			delete(hotCounters.pending, key)
		} else {
			hotCounters.pending[key] = retained
		}
	}
}

func pruneRateEvents(events []int64, cutoff int64) []int64 {
	retained := events[:0]
	for _, occurred := range events {
		if occurred > cutoff {
			retained = append(retained, occurred)
		}
	}
	return retained
}

// FlushHotCounters persists buffered rate-limit events in batches of at most
// 500 writes per transaction.
func FlushHotCounters(ctx context.Context) error {
	mu.RLock()
	ready := conn != nil
	mu.RUnlock()
	if !ready {
		return sql.ErrConnDone
	}
	hotCounters.Lock()
	pruneHotRateEventsLocked(time.Now().UTC().UnixMilli())
	batch := hotCounters.pending
	tokenBatch := hotCounters.pendingTokenUsed
	if len(batch) == 0 {
		if len(tokenBatch) == 0 {
			hotCounters.Unlock()
			return nil
		}
	}
	hotCounters.pending = map[string][]int64{}
	hotCounters.pendingTokenUsed = map[int64]int64{}
	hotCounters.Unlock()
	tokenBatchCommitted := false
	defer func() {
		if tokenBatchCommitted {
			return
		}
		hotCounters.Lock()
		for id, stamp := range tokenBatch {
			if hotCounters.pendingTokenUsed[id] < stamp {
				hotCounters.pendingTokenUsed[id] = stamp
			}
		}
		hotCounters.Unlock()
	}()
	flat := make([]hotRateEvent, 0)
	for key, events := range batch {
		for _, occurred := range events {
			flat = append(flat, hotRateEvent{key: key, occurred: occurred})
		}
	}
	for start := 0; start < len(flat); start += 500 {
		end := min(start+500, len(flat))
		err := Write(ctx, func(tx *sql.Tx) error {
			for _, item := range flat[start:end] {
				if _, err := tx.Exec("INSERT INTO rate_limit_events(key, occurred_at) VALUES (?, ?)", item.key, item.occurred); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return requeueHotRateEvents(flat[start:], err)
		}
	}
	ids := make([]int64, 0, len(tokenBatch))
	for id := range tokenBatch {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		err := Write(ctx, func(tx *sql.Tx) error {
			for _, id := range ids[start:end] {
				stamp := tokenBatch[id]
				if _, err := tx.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE id = ? AND revoked_at IS NULL
AND (expires_at IS NULL OR expires_at > ?)`, stamp, id, stamp); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE owners SET last_seen_at = ? WHERE id = (SELECT owner_id FROM api_tokens WHERE id = ?)`, stamp, id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	tokenBatchCommitted = true
	return nil
}

func requeueHotRateEvents(events []hotRateEvent, cause error) error {
	hotCounters.Lock()
	for _, event := range events {
		hotCounters.pending[event.key] = append(hotCounters.pending[event.key], event.occurred)
	}
	hotCounters.Unlock()
	return cause
}
