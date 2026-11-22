package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/apimgr/gitignore/src/db"
)

func (s *Server) visiblePGPMetadata() (db.PGPKeyMetadata, bool) {
	if s == nil || s.config == nil || s.currentConfig() == nil || !s.currentConfig().Web.Security.PublishPGPKey {
		return db.PGPKeyMetadata{}, false
	}
	metadata, err := db.GetPGPKeyMetadata()
	if err != nil || metadata.Revoked || metadata.Fingerprint == "" {
		return db.PGPKeyMetadata{}, false
	}
	return metadata, true
}

func (s *Server) handlePGPKey(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || s.config.Paths == nil {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.visiblePGPMetadata(); !ok {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.config.Paths.GetConfigDir(), "security", "pgp.pub.asc")
	key, err := os.ReadFile(path)
	if err != nil || len(key) == 0 || len(key) > 128*1024 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/pgp-keys; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(key)
}
