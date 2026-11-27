package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var rateLimitRedis atomic.Pointer[redis.Client]
var rateLimitCachePrefix atomic.Value
var rateLimitCacheWarnOnce sync.Once
var rateLimitFallbackMu sync.Mutex
var rateLimitFallback = make(map[string][]int64)
var rateLimitSequence atomic.Uint64
var rateLimitFallbackPrunedAt time.Time

const maxRateLimitFallbackKeys = 100000

// ConfigureRateLimitCache selects Valkey/Redis as the counter store. Passing
// nil keeps the normal SQLite/libSQL-backed implementation.
func ConfigureRateLimitCache(client *redis.Client, prefix string) {
	if client == nil {
		rateLimitRedis.Store(nil)
		return
	}
	rateLimitCachePrefix.Store(prefix)
	rateLimitRedis.Store(client)
}

// RateLimitCacheHealthy reports whether the configured external counter store
// has answered a recent rate-limit operation.
func RateLimitCacheHealthy() bool {
	client := rateLimitRedis.Load()
	if client == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		warnRateLimitCache(err)
		return false
	}
	return true
}

var rateLimitCacheWarned atomic.Bool

// AllowRateLimit consumes a slot in the in-memory sliding window; a background
// flusher persists request counters to the database every 30 seconds.
func AllowRateLimit(key string, limit, windowSeconds int) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" || limit < 1 || windowSeconds < 1 {
		return false, errors.New("invalid rate-limit key or window")
	}
	if client := rateLimitRedis.Load(); client != nil {
		if rateLimitCacheWarned.Load() {
			if err := restoreRateLimitFallback(client); err != nil {
				warnRateLimitCache(err)
				return fallbackRateLimit(key, limit, windowSeconds, true), nil
			}
		}
		allowed, err := externalRateLimit(client, key, limit, windowSeconds, true)
		if err == nil {
			rateLimitCacheWarned.Store(false)
			return allowed, nil
		}
		warnRateLimitCache(err)
		return fallbackRateLimit(key, limit, windowSeconds, true), nil
	}
	started := time.Now()
	events, err := appendHotRateEvent(key, started.UTC().UnixMilli(), windowSeconds)
	observeCacheLabel("rate_limit", "memory", len(events) > 1 && err == nil, started)
	return len(events) <= limit, err
}

// RateLimitBlocked reports whether a client's current counter has reached its limit.
func RateLimitBlocked(key string, limit, windowSeconds int) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" || limit < 1 || windowSeconds < 1 {
		return false, errors.New("invalid rate-limit key or window")
	}
	if client := rateLimitRedis.Load(); client != nil {
		if rateLimitCacheWarned.Load() {
			if err := restoreRateLimitFallback(client); err != nil {
				warnRateLimitCache(err)
				return fallbackRateLimit(key, limit, windowSeconds, false), nil
			}
		}
		blocked, err := externalRateLimit(client, key, limit, windowSeconds, false)
		if err == nil {
			rateLimitCacheWarned.Store(false)
			return blocked, nil
		}
		warnRateLimitCache(err)
		return fallbackRateLimit(key, limit, windowSeconds, false), nil
	}
	started := time.Now()
	events, err := activeHotRateEvents(key, started.UTC().UnixMilli(), windowSeconds)
	observeCacheLabel("rate_limit", "memory", len(events) > 0 && err == nil, started)
	return len(events) >= limit, err
}

// RateLimitRetryAfter returns the seconds until the oldest active request expires.
func RateLimitRetryAfter(key string, windowSeconds int) (int, error) {
	key = strings.TrimSpace(key)
	if key == "" || windowSeconds < 1 {
		return 0, errors.New("invalid rate-limit key or window")
	}
	if client := rateLimitRedis.Load(); client != nil {
		if rateLimitCacheWarned.Load() {
			if err := restoreRateLimitFallback(client); err != nil {
				warnRateLimitCache(err)
				return fallbackRateLimitRetry(key, windowSeconds), nil
			}
		}
		wait, err := externalRateLimitRetry(client, key, windowSeconds)
		if err == nil {
			rateLimitCacheWarned.Store(false)
			return wait, nil
		}
		warnRateLimitCache(err)
		return fallbackRateLimitRetry(key, windowSeconds), nil
	}
	started := time.Now()
	events, err := activeHotRateEvents(key, started.UTC().UnixMilli(), windowSeconds)
	observeCacheLabel("rate_limit", "memory", len(events) > 0 && err == nil, started)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return windowSeconds, nil
	}
	remaining := int((events[0] + int64(windowSeconds)*1000 - time.Now().UTC().UnixMilli() + 999) / 1000)
	if remaining < 1 {
		remaining = 1
	}
	return remaining, nil
}

func restoreRateLimitFallback(client *redis.Client) error {
	rateLimitFallbackMu.Lock()
	defer rateLimitFallbackMu.Unlock()
	prefix, _ := rateLimitCachePrefix.Load().(string)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pipe := client.TxPipeline()
	now := time.Now().UTC().UnixMilli()
	for key, events := range rateLimitFallback {
		valid := events[:0]
		for _, event := range events {
			if now-event <= 24*60*60*1000 {
				valid = append(valid, event)
			}
		}
		if len(valid) == 0 {
			delete(rateLimitFallback, key)
			continue
		}
		redisKey := prefix + "ratelimit:" + key
		members := make([]redis.Z, 0, len(valid))
		for _, event := range valid {
			members = append(members, redis.Z{Score: float64(event), Member: fmt.Sprintf("fallback-%d-%d", event, rateLimitSequence.Add(1))})
		}
		pipe.ZAdd(ctx, redisKey, members...)
		pipe.PExpire(ctx, redisKey, 24*time.Hour)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	clear(rateLimitFallback)
	rateLimitCacheWarned.Store(false)
	return nil
}

func externalRateLimitKey(key string) string {
	prefix, _ := rateLimitCachePrefix.Load().(string)
	return prefix + "ratelimit:" + key
}

func externalRateLimit(client *redis.Client, key string, limit, windowSeconds int, add bool) (allowed bool, err error) {
	started := time.Now()
	defer func() { observeCacheLabel("rate_limit", "external", err == nil, started) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := time.Now().UTC().UnixMilli()
	redisKey := externalRateLimitKey(key)
	cutoff := now - int64(windowSeconds)*1000
	pipe := client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, redisKey, "-inf", fmt.Sprint(cutoff))
	if add {
		member := fmt.Sprintf("%d-%d", now, rateLimitSequence.Add(1))
		pipe.ZAdd(ctx, redisKey, redis.Z{Score: float64(now), Member: member})
	}
	count := pipe.ZCard(ctx, redisKey)
	pipe.PExpire(ctx, redisKey, time.Duration(windowSeconds)*time.Second)
	if _, execErr := pipe.Exec(ctx); execErr != nil {
		return false, execErr
	}
	if add {
		return count.Val() <= int64(limit), nil
	}
	return count.Val() >= int64(limit), nil
}

func externalRateLimitRetry(client *redis.Client, key string, windowSeconds int) (wait int, err error) {
	started := time.Now()
	defer func() { observeCacheLabel("rate_limit_retry", "external", err == nil, started) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := time.Now().UTC().UnixMilli()
	redisKey := externalRateLimitKey(key)
	cutoff := now - int64(windowSeconds)*1000
	if execErr := client.ZRemRangeByScore(ctx, redisKey, "-inf", fmt.Sprint(cutoff)).Err(); execErr != nil {
		return 0, execErr
	}
	items, queryErr := client.ZRangeWithScores(ctx, redisKey, 0, 0).Result()
	if queryErr != nil || len(items) == 0 {
		return 0, queryErr
	}
	remaining := int((int64(items[0].Score) + int64(windowSeconds)*1000 - now + 999) / 1000)
	if remaining < 1 {
		remaining = 1
	}
	return remaining, nil
}

func warnRateLimitCache(err error) {
	rateLimitCacheWarnOnce.Do(func() { log.Printf("rate-limit cache unavailable; using in-process fallback: %v", err) })
	rateLimitCacheWarned.Store(true)
}

func fallbackRateLimit(key string, limit, windowSeconds int, add bool) bool {
	started := time.Now()
	rateLimitFallbackMu.Lock()
	hit := false
	defer func() {
		rateLimitFallbackMu.Unlock()
		observeCacheLabel("rate_limit", "memory_fallback", hit, started)
	}()
	now := time.Now().UTC().UnixMilli()
	pruneRateLimitFallbackLocked(now)
	cutoff := now - int64(windowSeconds)*1000
	events := rateLimitFallback[key]
	first := 0
	for first < len(events) && events[first] <= cutoff {
		first++
	}
	events = append([]int64(nil), events[first:]...)
	hit = len(events) > 0
	if add {
		if len(events) == 0 && len(rateLimitFallback) >= maxRateLimitFallbackKeys {
			return false
		}
		events = append(events, now)
		sort.Slice(events, func(i, j int) bool { return events[i] < events[j] })
	}
	if len(events) == 0 {
		delete(rateLimitFallback, key)
	} else {
		rateLimitFallback[key] = events
	}
	if add {
		return len(events) <= limit
	}
	return len(events) >= limit
}

func fallbackRateLimitRetry(key string, windowSeconds int) int {
	started := time.Now()
	rateLimitFallbackMu.Lock()
	hit := false
	defer func() {
		rateLimitFallbackMu.Unlock()
		observeCacheLabel("rate_limit", "memory_fallback", hit, started)
	}()
	now := time.Now().UTC().UnixMilli()
	pruneRateLimitFallbackLocked(now)
	cutoff := now - int64(windowSeconds)*1000
	events := rateLimitFallback[key]
	first := 0
	for first < len(events) && events[first] <= cutoff {
		first++
	}
	events = append([]int64(nil), events[first:]...)
	hit = len(events) > 0
	if len(events) == 0 {
		delete(rateLimitFallback, key)
		return 0
	}
	rateLimitFallback[key] = events
	remaining := int((events[0] + int64(windowSeconds)*1000 - now + 999) / 1000)
	if remaining < 1 {
		remaining = 1
	}
	return remaining
}

func pruneRateLimitFallbackLocked(now int64) {
	if time.Since(rateLimitFallbackPrunedAt) < time.Minute {
		return
	}
	rateLimitFallbackPrunedAt = time.Now()
	cutoff := now - int64(24*time.Hour/time.Millisecond)
	var evicted uint64
	for key, events := range rateLimitFallback {
		retained := events[:0]
		for _, occurred := range events {
			if occurred > cutoff {
				retained = append(retained, occurred)
			}
		}
		if len(retained) == 0 {
			delete(rateLimitFallback, key)
			evicted++
		} else {
			rateLimitFallback[key] = retained
		}
	}
	observeCacheEvictions("rate_limit", evicted)
}

func rateLimitFallbackSnapshot() (int64, int64) {
	rateLimitFallbackMu.Lock()
	defer rateLimitFallbackMu.Unlock()
	var size, bytes int64
	for key, events := range rateLimitFallback {
		size++
		bytes += int64(len(key)) + int64(len(events))*8
	}
	return size, bytes
}
