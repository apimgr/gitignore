package geoip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dbSource describes one downloadable MMDB database.
type dbSource struct {
	file        string
	url         string
	checksumURL string
}

// sources lists the databases to download with their canonical URLs (AI.md
// PART 19 "Database Sources"). AI.md is the source of truth for these URLs.
// Manager.mirrorBase lets tests use a local host.
func sources() []dbSource {
	return []dbSource{
		{fileASN, "https://github.com/sapics/ip-location-db/releases/download/latest/iptoasn-asn.mmdb", "https://github.com/sapics/ip-location-db/releases/download/checksum/iptoasn-asn.mmdb.sha256"},
		{fileCountry, "https://github.com/sapics/ip-location-db/releases/download/latest/iptoasn-country.mmdb", "https://github.com/sapics/ip-location-db/releases/download/checksum/iptoasn-country.mmdb.sha256"},
		{fileCityV4, "https://github.com/sapics/ip-location-db/releases/download/latest/dbip-city-ipv4.mmdb", "https://github.com/sapics/ip-location-db/releases/download/checksum/dbip-city-ipv4.mmdb.sha256"},
		{fileCityV6, "https://github.com/sapics/ip-location-db/releases/download/latest/dbip-city-ipv6.mmdb", "https://github.com/sapics/ip-location-db/releases/download/checksum/dbip-city-ipv6.mmdb.sha256"},
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
		dest := filepath.Join(m.dir, src.file)
		checksumURL := src.checksumURL
		if m.mirrorBase != "" {
			checksumURL = m.mirrorBase + "/checksum/" + src.file + ".sha256"
		}
		if err := downloadVerifiedFile(ctx, client, m.resolveURL(src), checksumURL, dest, m.openReader); err != nil {
			log.Printf("geoip: update %s failed: %v", src.file, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		downloaded++
	}

	var stampErr error
	if downloaded == len(sources()) {
		stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
		stampErr = atomicWrite(filepath.Join(m.dir, ".last_updated"), stamp, 0o600)
	}
	m.Load()
	if stampErr != nil {
		return fmt.Errorf("write geoip update timestamp: %w", stampErr)
	}

	if downloaded == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}

func downloadVerifiedFile(ctx context.Context, client *http.Client, url, checksumURL, dest string, validate func(string) (mmReader, error)) error {
	checksum, err := fetchChecksum(ctx, client, checksumURL)
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}
	return downloadFileWithChecksum(ctx, client, url, dest, checksum, validate)
}

func fetchChecksum(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected checksum status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("invalid SHA-256 checksum")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", fmt.Errorf("invalid SHA-256 checksum: %w", err)
	}
	return strings.ToLower(fields[0]), nil
}

// downloadFile fetches url and writes it to dest atomically (temp file in the
// same directory, then rename). The temp file is removed on any failure.
func downloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	return downloadFileWithChecksum(ctx, client, url, dest, "", nil)
}

func downloadFileWithChecksum(ctx context.Context, client *http.Client, url, dest, expected string, validate func(string) (mmReader, error)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".geoip-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if expected != "" && hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("SHA-256 checksum mismatch")
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("downloaded database is empty")
	}
	if validate != nil {
		reader, err := validate(tmpName)
		if err != nil {
			return fmt.Errorf("invalid MMDB database: %w", err)
		}
		if err := reader.Close(); err != nil {
			return fmt.Errorf("close validated MMDB database: %w", err)
		}
	}
	return os.Rename(tmpName, dest)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".geoip-stamp-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
