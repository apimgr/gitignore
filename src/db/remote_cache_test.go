package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteSQLiteCacheSyncAndReadThrough(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	remotePath := filepath.Join(t.TempDir(), "remote.db")
	remote, err := sql.Open("sqlite", remotePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range cacheTables {
		rows, err := conn.Query("PRAGMA table_info(\"" + table + "\")")
		if err != nil {
			t.Fatal(err)
		}
		var columns []string
		for rows.Next() {
			var cid int
			var name, kind string
			var notnull, pk int
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
				t.Fatal(err)
			}
			columns = append(columns, `"`+strings.ReplaceAll(name, `"`, `""`)+`" BLOB`)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if len(columns) == 0 {
			t.Fatalf("table %s has no schema", table)
		}
		if table == "rate_limit_events" {
			columns = append(columns, `"extra_secret" BLOB`)
		}
		if _, err := remote.Exec("CREATE TABLE srv_" + table + " (" + strings.Join(columns, ",") + ")"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := remote.Exec("INSERT INTO srv_rate_limit_events (key, occurred_at) VALUES (?, ?)", "initial", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	cache, _, err := openRemoteSQLiteCache(context.Background(), remote, remotePath, CacheConfig{Enabled: true, Path: filepath.Join(t.TempDir(), "cache.db"), SyncInterval: time.Hour, MaxAge: time.Hour, OfflineMode: true})
	if err != nil {
		t.Fatal(err)
	}
	cacheInfo, err := os.Stat(cache.path)
	if err != nil {
		t.Fatal(err)
	}
	if cacheInfo.Mode().Perm() != 0o600 {
		t.Fatalf("cache database permissions = %04o, want 0600", cacheInfo.Mode().Perm())
	}
	oldCache := localCache
	localCache = cache
	t.Cleanup(func() { localCache = oldCache; _ = cache.close() })
	cache.mu.Lock()
	cache.syncing = true
	cache.mu.Unlock()
	if cache.isExpired() {
		t.Fatal("a clean cache expired while a background snapshot was being fetched")
	}
	fallbackRows, err := cache.fallbackQuery(context.Background(), "SELECT COUNT(*) FROM rate_limit_events", nil)
	if err != nil {
		t.Fatalf("valid offline cache unavailable during background fetch: %v", err)
	}
	if err := fallbackRows.Close(); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.syncing = false
	cache.mu.Unlock()
	var count int
	if err := cache.reader.QueryRow("SELECT COUNT(*) FROM rate_limit_events WHERE key = ?", "initial").Scan(&count); err != nil || count != 1 {
		t.Fatalf("synced cache count=%d err=%v", count, err)
	}
	if _, err := remote.Exec("INSERT INTO srv_rate_limit_events (key, occurred_at) VALUES (?, ?)", "read-through", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var found string
	if err := cache.reader.QueryRow("SELECT key FROM rate_limit_events WHERE key = ?", "read-through").Scan(&found); err != nil || found != "read-through" {
		t.Fatalf("read-through result=%q err=%v", found, err)
	}
	if _, err := remote.Exec("INSERT INTO srv_rate_limit_events (key, occurred_at) VALUES (?, ?)", "syncing-remote", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.syncing = true
	cache.mu.Unlock()
	if err := cache.reader.QueryRow("SELECT key FROM rate_limit_events WHERE key = ?", "syncing-remote").Scan(&found); err != nil || found != "syncing-remote" {
		t.Fatalf("syncing cache remote result=%q err=%v", found, err)
	}
	cache.mu.Lock()
	cache.syncing = false
	cache.mu.Unlock()
	if _, err := remote.Exec("INSERT INTO srv_rate_limit_events (key, occurred_at) VALUES (?, ?)", "dirty-remote", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.dirty = true
	cache.mu.Unlock()
	if err := cache.reader.QueryRow("SELECT key FROM rate_limit_events WHERE key = ?", "dirty-remote").Scan(&found); err != nil || found != "dirty-remote" {
		t.Fatalf("dirty cache remote result=%q err=%v", found, err)
	}
}
