package geoip

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

// dbSource describes one downloadable MMDB database.
type dbSource struct {
	file    string
	url     string
	enabled func(config.GeoIPDatabasesConfig) bool
}

// sources lists the databases to download with their canonical URLs (AI.md
// PART 19 "Database Sources"). AI.md is the source of truth for these URLs.
//
// NOTE: ~/.claude/memory/security_conventions.md records that the jsDelivr CDN
// was deprecated 2026-06-18 in favor of GitHub Releases
// (https://github.com/sapics/ip-location-db/releases/download/latest/{file}).
// The AI.md table still lists jsDelivr, so those URLs remain the default here;
// switching mirrors is a single-line edit, and Manager.mirrorBase lets an
// operator (or a test) point downloads at any base host.
func sources() []dbSource {
	return []dbSource{
		{fileASN, "https://cdn.jsdelivr.net/npm/@ip-location-db/asn-mmdb/asn.mmdb",
			func(d config.GeoIPDatabasesConfig) bool { return d.ASN }},
		{fileCountry, "https://cdn.jsdelivr.net/npm/@ip-location-db/geo-whois-asn-country-mmdb/geo-whois-asn-country.mmdb",
			func(d config.GeoIPDatabasesConfig) bool { return d.Country }},
		{fileCityV4, "https://cdn.jsdelivr.net/npm/@ip-location-db/dbip-city-mmdb/dbip-city-ipv4.mmdb",
			func(d config.GeoIPDatabasesConfig) bool { return d.City }},
		{fileCityV6, "https://cdn.jsdelivr.net/npm/@ip-location-db/dbip-city-mmdb/dbip-city-ipv6.mmdb",
			func(d config.GeoIPDatabasesConfig) bool { return d.City }},
	}
}

// resolveURL returns the download URL for src, honoring a configured mirror
// base host when set.
func (m *Manager) resolveURL(src dbSource) string {
	if m.mirrorBase != "" {
		return m.mirrorBase + "/" + src.file
	}
	return src.url
}

// Update downloads every enabled database into the GeoIP directory and reloads
// the open readers. It implements the scheduler's geoip_update task (AI.md
// PART 18/19). Downloads are atomic (temp file + rename); a per-file failure is
// logged and does not abort the remaining databases. It returns an error only
// when nothing downloaded and at least one attempt failed.
func (m *Manager) Update(ctx context.Context) error {
	if !m.cfg.Enabled {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("create geoip dir: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Minute}

	var firstErr error
	downloaded := 0
	for _, src := range sources() {
		if !src.enabled(m.cfg.Databases) {
			continue
		}
		dest := filepath.Join(m.dir, src.file)
		if err := downloadFile(ctx, client, m.resolveURL(src), dest); err != nil {
			log.Printf("geoip: update %s failed: %v", src.file, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		downloaded++
	}

	m.Load()

	if downloaded == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}

// downloadFile fetches url and writes it to dest atomically (temp file in the
// same directory, then rename). The temp file is removed on any failure.
func downloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".geoip-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}
