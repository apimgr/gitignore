package cve

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultNVDSource = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	trivyLayerType   = "application/vnd.aquasec.trivy.db.layer.v1.tar+gzip"
	pageSize         = 2000
	maxNVDWindow     = 120 * 24 * time.Hour
)

// Config selects the CVE feeds and on-disk database locations.
type Config struct {
	DataDir      string
	SecurityDir  string
	NVDSource    string
	NVDAPIKey    string
	TrivyEnabled bool
	TrivySource  string
	HTTPClient   *http.Client
	Now          func() time.Time
}

// Update refreshes NVD and, when enabled, the Trivy vulnerability database.
// Feed failures are returned to the scheduler; they never affect startup.
func Update(ctx context.Context, cfg Config) error {
	securityDir := strings.TrimSpace(cfg.SecurityDir)
	if securityDir == "" && strings.TrimSpace(cfg.DataDir) != "" {
		securityDir = filepath.Join(cfg.DataDir, "security")
	}
	if securityDir == "" {
		return errors.New("cve: data directory is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	base := securityDir
	if err := updateNVD(ctx, filepath.Join(base, "cve"), cfg); err != nil {
		return err
	}
	if cfg.TrivyEnabled {
		if err := updateTrivy(ctx, filepath.Join(base, "trivy"), cfg); err != nil {
			return err
		}
	}
	return nil
}

// HealthStatus returns the public-safe health state for a downloaded database.
// The daily updater gets one missed run of tolerance before data is stale.
func HealthStatus(securityDir, database string, now time.Time) string {
	if strings.TrimSpace(securityDir) == "" {
		return "error"
	}
	switch database {
	case "nvd":
		dir := filepath.Join(securityDir, "cve")
		stamp, err := readStamp(filepath.Join(dir, ".last_updated"))
		if err != nil {
			return "error"
		}
		records, err := readNVD(filepath.Join(dir, "nvd.json"))
		if err != nil || len(records) == 0 {
			return "error"
		}
		if now.Sub(stamp) > 48*time.Hour || stamp.After(now.Add(5*time.Minute)) {
			return "degraded"
		}
	case "trivy":
		trivyDir := filepath.Join(securityDir, "trivy")
		stamp, err := readStamp(filepath.Join(trivyDir, ".last_updated"))
		if err != nil {
			return "error"
		}
		metadata, err := readTrivyMetadata(filepath.Join(trivyDir, "metadata.json"))
		if err != nil {
			return "error"
		}
		info, err := os.Stat(filepath.Join(trivyDir, "trivy.db"))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return "error"
		}
		if metadata.NextUpdate.Before(now) || now.Sub(stamp) > 48*time.Hour || stamp.After(now.Add(5*time.Minute)) {
			return "degraded"
		}
	default:
		return "error"
	}
	return "ok"
}

type nvdResponse struct {
	TotalResults    int         `json:"totalResults"`
	Vulnerabilities []nvdRecord `json:"vulnerabilities"`
}

type nvdRecord struct {
	CVE json.RawMessage `json:"cve"`
}

type cveIdentity struct {
	ID string `json:"id"`
}

type nvdRateLimiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

var nvdLimiters = map[bool]*nvdRateLimiter{}
var nvdLimitersMu sync.Mutex

func limiterFor(hasKey bool) *nvdRateLimiter {
	nvdLimitersMu.Lock()
	defer nvdLimitersMu.Unlock()
	if limiter := nvdLimiters[hasKey]; limiter != nil {
		return limiter
	}
	requests := 4.0
	if hasKey {
		requests = 49
	}
	limiter := &nvdRateLimiter{interval: 30 * time.Second / time.Duration(requests)}
	nvdLimiters[hasKey] = limiter
	return limiter
}

func (l *nvdRateLimiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	wait := time.Duration(0)
	if l.next.After(now) {
		wait = l.next.Sub(now)
	}
	base := now
	if l.next.After(base) {
		base = l.next
	}
	l.next = base.Add(l.interval)
	l.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func updateNVD(ctx context.Context, dir string, cfg Config) error {
	path := filepath.Join(dir, "nvd.json")
	stampPath := filepath.Join(dir, ".last_updated")
	previous, err := readNVD(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cve: read NVD database: %w", err)
	}
	if previous == nil {
		previous = make(map[string]json.RawMessage)
	}
	lastUpdate, stampErr := readStamp(stampPath)
	if stampErr != nil && !errors.Is(stampErr, os.ErrNotExist) {
		return fmt.Errorf("cve: read NVD update timestamp: %w", stampErr)
	}
	now := cfg.Now().UTC().Truncate(time.Second)
	client := cfg.HTTPClient
	source := strings.TrimSpace(cfg.NVDSource)
	if source == "" {
		source = defaultNVDSource
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("cve: NVD source must be an HTTPS URL")
	}
	updates := make(map[string]json.RawMessage)
	if lastUpdate.IsZero() || len(previous) == 0 {
		if err := fetchNVDWindow(ctx, client, source, cfg.NVDAPIKey, time.Time{}, time.Time{}, updates); err != nil {
			return err
		}
	} else if lastUpdate.Before(now) {
		start := lastUpdate
		for start.Before(now) {
			end := start.Add(maxNVDWindow)
			if end.After(now) {
				end = now
			}
			if err := fetchNVDWindow(ctx, client, source, cfg.NVDAPIKey, start, end, updates); err != nil {
				return err
			}
			start = end
		}
	}
	for id, item := range updates {
		previous[id] = item
	}
	if len(previous) == 0 {
		return errors.New("cve: NVD feed contains no valid CVE records")
	}
	if err := writeNVD(path, previous); err != nil {
		return fmt.Errorf("cve: write NVD database: %w", err)
	}
	if err := writeStamp(stampPath, now); err != nil {
		return fmt.Errorf("cve: write NVD update timestamp: %w", err)
	}
	return nil
}

func readNVD(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("invalid JSON array: %w", err)
	}
	result := make(map[string]json.RawMessage, len(records))
	for _, raw := range records {
		var item cveIdentity
		if json.Unmarshal(raw, &item) != nil || !strings.HasPrefix(item.ID, "CVE-") {
			return nil, errors.New("invalid NVD record without a CVE id")
		}
		result[item.ID] = append(json.RawMessage(nil), raw...)
	}
	return result, nil
}

func fetchNVDWindow(ctx context.Context, client *http.Client, source, apiKey string, start, end time.Time, into map[string]json.RawMessage) error {
	limiter := limiterFor(apiKey != "")
	for offset := 0; ; {
		if err := limiter.wait(ctx); err != nil {
			return err
		}
		u, _ := url.Parse(source)
		query := u.Query()
		query.Set("resultsPerPage", strconv.Itoa(pageSize))
		query.Set("startIndex", strconv.Itoa(offset))
		if !start.IsZero() && !end.IsZero() {
			query.Set("lastModStartDate", start.UTC().Format("2006-01-02T15:04:05.000"))
			query.Set("lastModEndDate", end.UTC().Format("2006-01-02T15:04:05.000"))
		}
		u.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return fmt.Errorf("cve: build NVD request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "gitignore-security-database/1")
		if apiKey != "" {
			req.Header.Set("apiKey", apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("cve: NVD request failed: %w", err)
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			return fmt.Errorf("cve: NVD rate limit response %d; retry on the next scheduled run", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return fmt.Errorf("cve: NVD request returned HTTP %d", resp.StatusCode)
		}
		var page nvdResponse
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(&page)
		closeErr := resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("cve: decode NVD response: %w", decodeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("cve: close NVD response: %w", closeErr)
		}
		for _, record := range page.Vulnerabilities {
			var item cveIdentity
			if len(record.CVE) == 0 || json.Unmarshal(record.CVE, &item) != nil || !strings.HasPrefix(item.ID, "CVE-") {
				continue
			}
			into[item.ID] = append(json.RawMessage(nil), record.CVE...)
		}
		offset += len(page.Vulnerabilities)
		if offset >= page.TotalResults || len(page.Vulnerabilities) == 0 {
			return nil
		}
	}
}

func writeNVD(path string, records map[string]json.RawMessage) error {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		items = append(items, records[id])
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0600)
}

func readStamp(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	value, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, err
	}
	return value.UTC(), nil
}

func writeStamp(path string, value time.Time) error {
	return atomicWrite(path, []byte(value.UTC().Format(time.RFC3339)+"\n"), 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".cve-update-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	backup := tempPath + ".previous"
	hadPrevious := false
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, path)
		}
		return err
	}
	if hadPrevious {
		return os.Remove(backup)
	}
	return nil
}

type trivyMetadata struct {
	NextUpdate time.Time `json:"NextUpdate"`
}

type trivyReference struct {
	registry string
	repo     string
	tag      string
}

type trivyManifest struct {
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

func updateTrivy(ctx context.Context, dir string, cfg Config) error {
	metadataPath := filepath.Join(dir, "metadata.json")
	if metadata, err := readTrivyMetadata(metadataPath); err == nil && metadata.NextUpdate.After(cfg.Now().UTC()) {
		return nil
	}
	source := strings.TrimSpace(cfg.TrivySource)
	if source == "" {
		source = "ghcr.io/aquasecurity/trivy-db:2"
	}
	ref, err := parseTrivyReference(source)
	if err != nil {
		return err
	}
	manifest, err := fetchTrivyManifest(ctx, cfg.HTTPClient, ref)
	if err != nil {
		return err
	}
	digest := ""
	for _, layer := range manifest.Layers {
		if layer.MediaType == trivyLayerType {
			digest = layer.Digest
			break
		}
	}
	if digest == "" {
		return errors.New("cve: Trivy manifest has no vulnerability database layer")
	}
	blob, err := fetchTrivyBlob(ctx, cfg.HTTPClient, ref, digest)
	if err != nil {
		return err
	}
	defer func() { _ = blob.Close() }()
	return replaceTrivyDirectory(dir, blob)
}

func readTrivyMetadata(path string) (trivyMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return trivyMetadata{}, err
	}
	var metadata trivyMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return trivyMetadata{}, err
	}
	if metadata.NextUpdate.IsZero() {
		return trivyMetadata{}, errors.New("cve: Trivy metadata has no NextUpdate")
	}
	return metadata, nil
}

func parseTrivyReference(value string) (trivyReference, error) {
	value = strings.TrimPrefix(value, "oci://")
	if strings.Contains(value, "://") {
		return trivyReference{}, errors.New("cve: Trivy source must be an OCI image reference")
	}
	slash := strings.IndexByte(value, '/')
	if slash <= 0 {
		return trivyReference{}, errors.New("cve: invalid Trivy OCI reference")
	}
	registry, remainder := value[:slash], value[slash+1:]
	lastSlash := strings.LastIndexByte(remainder, '/')
	colon := strings.LastIndexByte(remainder, ':')
	if colon <= lastSlash || colon == len(remainder)-1 {
		return trivyReference{}, errors.New("cve: Trivy OCI reference must include a tag")
	}
	return trivyReference{registry: registry, repo: remainder[:colon], tag: remainder[colon+1:]}, nil
}

var bearerParams = regexp.MustCompile(`([A-Za-z]+)="([^"]*)"`)

func fetchTrivyManifest(ctx context.Context, client *http.Client, ref trivyReference) (trivyManifest, error) {
	endpoint := "https://" + ref.registry + "/v2/" + ref.repo + "/manifests/" + url.PathEscape(ref.tag)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	response, err := client.Do(request)
	if err != nil {
		return trivyManifest{}, fmt.Errorf("cve: fetch Trivy manifest: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = response.Body.Close()
		token, tokenErr := trivyBearerToken(ctx, client, response.Header.Get("WWW-Authenticate"))
		if tokenErr != nil {
			return trivyManifest{}, tokenErr
		}
		request, _ = http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
		request.Header.Set("Authorization", "Bearer "+token)
		response, err = client.Do(request)
		if err != nil {
			return trivyManifest{}, fmt.Errorf("cve: fetch authenticated Trivy manifest: %w", err)
		}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return trivyManifest{}, fmt.Errorf("cve: Trivy manifest returned HTTP %d", response.StatusCode)
	}
	var manifest trivyManifest
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&manifest); err != nil {
		return trivyManifest{}, fmt.Errorf("cve: decode Trivy manifest: %w", err)
	}
	return manifest, nil
}

func trivyBearerToken(ctx context.Context, client *http.Client, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", errors.New("cve: Trivy registry did not provide a Bearer challenge")
	}
	params := map[string]string{}
	for _, match := range bearerParams.FindAllStringSubmatch(strings.TrimSpace(challenge[len("Bearer "):]), -1) {
		params[strings.ToLower(match[1])] = match[2]
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return "", errors.New("cve: Trivy registry returned an invalid token realm")
	}
	query := realm.Query()
	if params["service"] != "" {
		query.Set("service", params["service"])
	}
	if params["scope"] != "" {
		query.Set("scope", params["scope"])
	}
	realm.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("cve: request Trivy registry token: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cve: Trivy registry token endpoint returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("cve: decode Trivy registry token: %w", err)
	}
	if result.Token == "" {
		result.Token = result.AccessToken
	}
	if result.Token == "" {
		return "", errors.New("cve: Trivy registry returned an empty token")
	}
	return result.Token, nil
}

func fetchTrivyBlob(ctx context.Context, client *http.Client, ref trivyReference, digest string) (io.ReadCloser, error) {
	endpoint := "https://" + ref.registry + "/v2/" + ref.repo + "/blobs/" + url.PathEscape(digest)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cve: fetch Trivy database layer: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = response.Body.Close()
		token, tokenErr := trivyBearerToken(ctx, client, response.Header.Get("WWW-Authenticate"))
		if tokenErr != nil {
			return nil, tokenErr
		}
		request, _ = http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err = client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("cve: fetch authenticated Trivy database layer: %w", err)
		}
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("cve: Trivy database layer returned HTTP %d", response.StatusCode)
	}
	return response.Body, nil
}

func replaceTrivyDirectory(target string, layer io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(filepath.Dir(target), ".trivy-update-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	if err := os.Chmod(tempDir, 0700); err != nil {
		return err
	}
	if err := extractTrivyLayer(tempDir, layer); err != nil {
		return err
	}
	if err := writeStamp(filepath.Join(tempDir, ".last_updated"), time.Now().UTC()); err != nil {
		return err
	}
	backup := tempDir + ".previous"
	hadPrevious := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("cve: preserve existing Trivy database: %w", err)
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tempDir, target); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("cve: install Trivy database: %w", err)
	}
	if hadPrevious {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("cve: remove previous Trivy database: %w", err)
		}
	}
	return nil
}

func extractTrivyLayer(target string, layer io.Reader) error {
	compressed, err := gzip.NewReader(layer)
	if err != nil {
		return fmt.Errorf("cve: open compressed Trivy layer: %w", err)
	}
	defer func() { _ = compressed.Close() }()
	reader := tar.NewReader(compressed)
	seenDB, seenMetadata := false, false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("cve: read Trivy layer: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			continue
		}
		name := filepath.Base(filepath.Clean(header.Name))
		if name != "trivy.db" && name != "metadata.json" {
			continue
		}
		if header.Size <= 0 || header.Size > 2<<30 {
			return fmt.Errorf("cve: invalid Trivy layer file size for %s", name)
		}
		file, err := os.OpenFile(filepath.Join(target, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if name == "trivy.db" {
			seenDB = true
		} else {
			seenMetadata = true
		}
	}
	if !seenDB || !seenMetadata {
		return errors.New("cve: Trivy layer did not contain both trivy.db and metadata.json")
	}
	if _, err := readTrivyMetadata(filepath.Join(target, "metadata.json")); err != nil {
		return fmt.Errorf("cve: invalid Trivy metadata: %w", err)
	}
	databaseInfo, err := os.Stat(filepath.Join(target, "trivy.db"))
	if err != nil || databaseInfo.Size() == 0 {
		return errors.New("cve: Trivy database is empty")
	}
	return nil
}
