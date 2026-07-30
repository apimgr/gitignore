// Package updater implements the self-update mechanism (AI.md PART 22). It
// checks GitHub Releases for a newer build on the configured channel, downloads
// the platform binary, verifies its SHA256 against the release checksums.txt,
// and atomically replaces the running executable (platform-specific, with
// rollback). Network endpoints are injectable so the flow is testable without
// contacting GitHub.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// defaultAPIBase is the GitHub REST API host; overridable via Config.APIBaseURL
// so tests can point at an httptest server.
const defaultAPIBase = "https://api.github.com"

// Release is a subset of the GitHub Releases API response.
type Release struct {
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Asset is a single downloadable release artifact.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Config configures an Updater. Repo is "{org}/{name}"; BinaryName is the
// release-asset base name for the running binary (e.g. "gitignore"), matching
// the {name}-{os}-{arch} convention produced by `make build-all`.
type Config struct {
	Repo           string
	BinaryName     string
	CurrentVersion string
	Branch         string
	APIBaseURL     string
	UserAgent      string
	Client         *http.Client
}

// Updater performs update checks and installs against a single repo/channel.
type Updater struct {
	cfg Config
}

// New returns an Updater, applying defaults for any unset Config field.
func New(cfg Config) *Updater {
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = defaultAPIBase
	}
	if cfg.Branch == "" {
		cfg.Branch = "stable"
	}
	if cfg.BinaryName == "" {
		cfg.BinaryName = "gitignore"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = cfg.BinaryName + "-updater"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &Updater{cfg: cfg}
}

// Branch reports the configured release channel.
func (u *Updater) Branch() string { return u.cfg.Branch }

// Check returns the newest release on the configured channel whose tag differs
// from the current version, or nil when already current. A GitHub 404 (no
// releases / no "latest") is treated as "no update available" per AI.md PART 22.
func (u *Updater) Check(ctx context.Context) (*Release, error) {
	return u.check(ctx, 0)
}

// CheckDeferred behaves like Check but ignores releases published fewer than
// deferDays ago (AI.md PART 22 "Defer Semantics"). It selects the newest
// eligible release. deferDays <= 0 disables the gate.
func (u *Updater) CheckDeferred(ctx context.Context, deferDays int) (*Release, error) {
	return u.check(ctx, deferDays)
}

// check is the shared implementation. deferDays > 0 gates by publish age.
func (u *Updater) check(ctx context.Context, deferDays int) (*Release, error) {
	var url string
	switch u.cfg.Branch {
	case "stable":
		url = fmt.Sprintf("%s/repos/%s/releases/latest", u.cfg.APIBaseURL, u.cfg.Repo)
	default:
		url = fmt.Sprintf("%s/repos/%s/releases", u.cfg.APIBaseURL, u.cfg.Repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", u.cfg.UserAgent)

	resp, err := u.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 404 means no updates available (already current) per AI.md PART 22.
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	cutoffOK := func(r Release) bool {
		if deferDays <= 0 {
			return true
		}
		age := time.Since(r.PublishedAt)
		return age >= time.Duration(deferDays)*24*time.Hour
	}

	if u.cfg.Branch == "stable" {
		var release Release
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return nil, err
		}
		if release.TagName == u.cfg.CurrentVersion || !cutoffOK(release) {
			return nil, nil
		}
		return &release, nil
	}

	// Beta/daily: releases are newest-first; channels are cumulative, so the
	// first eligible match is the newest across this channel and every
	// more-stable channel (AI.md PART 22 "Channel Semantics").
	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	for i := range releases {
		r := releases[i]
		if r.TagName != u.cfg.CurrentVersion && matchesBranch(r, u.cfg.Branch) && cutoffOK(r) {
			return &r, nil
		}
	}
	return nil, nil
}

// Install downloads the platform binary for release, verifies its SHA256
// against the release checksums.txt, and atomically replaces the running
// executable (AI.md PART 22 "Update Flow"). It does not restart; callers that
// need the new binary to take over should call RestartSelf.
func (u *Updater) Install(ctx context.Context, release *Release) error {
	tmpPath, err := u.downloadVerified(ctx, release)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	currentPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	// replaceBinary is platform-specific and rolls back on failure.
	if err := replaceBinary(currentPath, tmpPath); err != nil {
		return err
	}
	return nil
}

// downloadVerified downloads the platform binary for release to a temp file and
// verifies its SHA256 against the release checksums.txt (MANDATORY, AI.md PART
// 22). On success it returns the temp path (caller owns cleanup); on any failure
// the temp file is removed before returning.
func (u *Updater) downloadVerified(ctx context.Context, release *Release) (string, error) {
	assetName := u.binaryAssetName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return "", fmt.Errorf("no binary found for %s/%s (asset %q)", runtime.GOOS, runtime.GOARCH, assetName)
	}

	tmpFile, err := os.CreateTemp("", u.cfg.BinaryName+"-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	// Remove the temp file on any error path below.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tmpFile.Close()
		return "", err
	}
	req.Header.Set("User-Agent", u.cfg.UserAgent)

	resp, err := u.cfg.Client.Do(req)
	if err != nil {
		tmpFile.Close()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return "", fmt.Errorf("binary download failed: %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to download: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	expectedHash, err := u.fetchExpectedChecksum(ctx, release, assetName)
	if err != nil {
		return "", fmt.Errorf("failed to fetch checksum: %w", err)
	}
	if err := verifyChecksum(tmpPath, expectedHash); err != nil {
		return "", err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			return "", fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	cleanup = false
	return tmpPath, nil
}

// binaryAssetName returns the release-asset name for the running platform,
// matching the {name}-{os}-{arch} convention from `make build-all`.
func (u *Updater) binaryAssetName() string {
	name := u.cfg.BinaryName + "-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// fetchExpectedChecksum downloads the release's checksums.txt asset and returns
// the SHA256 recorded for assetName. Each line is "{sha256}  {filename}".
func (u *Updater) fetchExpectedChecksum(ctx context.Context, release *Release, assetName string) (string, error) {
	var checksumsURL string
	for _, asset := range release.Assets {
		if asset.Name == "checksums.txt" {
			checksumsURL = asset.BrowserDownloadURL
			break
		}
	}
	if checksumsURL == "" {
		return "", fmt.Errorf("release has no checksums.txt asset")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", u.cfg.UserAgent)
	resp, err := u.cfg.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download failed: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", assetName)
}

// verifyChecksum verifies the SHA256 of filePath against expectedHash.
func verifyChecksum(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actualHash, strings.TrimSpace(expectedHash)) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

// matchesBranch implements cumulative channels: each channel also accepts every
// release from all more-stable channels (AI.md PART 22).
func matchesBranch(r Release, branch string) bool {
	// Stable releases match every channel.
	if !r.Prerelease {
		return true
	}
	isBeta := strings.HasSuffix(r.TagName, "-beta")
	// Daily builds are 14-digit timestamps: YYYYMMDDHHMMSS.
	isDaily := len(r.TagName) == 14 && !strings.Contains(r.TagName, ".")
	switch branch {
	case "beta":
		return isBeta
	case "daily":
		return isBeta || isDaily
	default:
		return false
	}
}
