package server

import "github.com/apimgr/gitignore/src/db"

func (s *Server) refreshCacheMetrics() {
	if s == nil || s.metrics == nil {
		return
	}
	s.metrics.SetCacheSnapshot("remote_db", 0, 0)
	s.metrics.SetCacheSnapshot("rate_limit", 0, 0)
	for _, snapshot := range db.CacheMetricsSnapshot() {
		s.metrics.SetCacheSnapshot(snapshot.Cache, snapshot.Size, snapshot.Bytes)
	}
}
