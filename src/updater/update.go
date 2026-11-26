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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	apppath "github.com/apimgr/gitignore/src/path"
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
	BuildEpoch     int64
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
	cfg.Branch = normalizeBranch(cfg.Branch)
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
	if u.cfg.Branch == "devel" {
		deferDays = 0
	}
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
	defer func() { _ = resp.Body.Close() }()

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

	// Beta/devel: releases are newest-first; channels are cumulative, so the
	// first eligible match is the newest across this channel and every
	// more-stable channel (AI.md PART 22 "Channel Semantics").
	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	for i := range releases {
		r := releases[i]
		if r.TagName == "devel" && u.cfg.Branch == "devel" && u.cfg.BuildEpoch > 0 && r.PublishedAt.Unix() <= u.cfg.BuildEpoch {
			continue
		}
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
	defer func() { _ = os.RemoveAll(filepath.Dir(tmpPath)) }()

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

// Stage downloads and verifies a release into a service-user-owned update
// directory. The checksum stored with the staged binary is informational; the
// privileged startup path must fetch it again before replacing the executable.
func (u *Updater) Stage(ctx context.Context, release *Release, updateDir string) error {
	if release == nil || !validReleaseTag(release.TagName) {
		return fmt.Errorf("release version is invalid")
	}
	tmpPath, err := u.downloadVerified(ctx, release)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(tmpPath)) }()
	if err := os.MkdirAll(updateDir, 0o750); err != nil {
		return fmt.Errorf("failed to create update directory: %w", err)
	}
	assetPath := filepath.Join(updateDir, u.cfg.BinaryName+".new")
	if err := copyFile(tmpPath, assetPath, 0o755); err != nil {
		return fmt.Errorf("failed to stage update binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(assetPath, 0o755); err != nil {
			_ = os.Remove(assetPath)
			return fmt.Errorf("failed to set staged binary permissions: %w", err)
		}
	}
	stagedHash, err := fileSHA256(assetPath)
	if err != nil {
		_ = os.Remove(assetPath)
		return err
	}
	if err := os.WriteFile(filepath.Join(updateDir, "sha256"), []byte(stagedHash+"\n"), 0o600); err != nil {
		_ = os.Remove(assetPath)
		return fmt.Errorf("failed to stage informational checksum: %w", err)
	}
	versionPath := filepath.Join(updateDir, "version")
	if err := os.WriteFile(versionPath, []byte(release.TagName+"\n"), 0o600); err != nil {
		_ = os.Remove(assetPath)
		return fmt.Errorf("failed to stage update version: %w", err)
	}
	return nil
}

// ApplyStaged verifies a staged binary against a checksum fetched from the
// release API during this privileged startup, then replaces the running binary.
func (u *Updater) ApplyStaged(ctx context.Context, updateDir string) (bool, error) {
	versionData, err := os.ReadFile(filepath.Join(updateDir, "version"))
	if errors.Is(err, os.ErrNotExist) {
		_ = os.RemoveAll(updateDir)
		return false, nil
	}
	if err != nil {
		_ = os.RemoveAll(updateDir)
		return false, err
	}
	tag := strings.TrimSpace(string(versionData))
	cleanup := func() { _ = os.RemoveAll(updateDir) }
	if !validReleaseTag(tag) {
		cleanup()
		return false, fmt.Errorf("staged update version is invalid or not newer")
	}
	release, err := u.releaseByTag(ctx, tag)
	if err != nil {
		cleanup()
		return false, err
	}
	if !u.isNewerRelease(release) {
		cleanup()
		return false, fmt.Errorf("staged update version is invalid or not newer")
	}
	assetPath := filepath.Join(updateDir, u.cfg.BinaryName+".new")
	expectedHash, err := u.fetchExpectedChecksum(ctx, release, u.binaryAssetName())
	if err != nil {
		cleanup()
		return false, fmt.Errorf("failed to fetch trusted release checksum: %w", err)
	}
	trustedDir, err := apppath.TempDir()
	if err != nil {
		cleanup()
		return false, fmt.Errorf("failed to create trusted update staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(trustedDir) }()
	trustedPath := filepath.Join(trustedDir, u.cfg.BinaryName+"-verified.new")
	if err := copyFile(assetPath, trustedPath, 0o700); err != nil {
		cleanup()
		return false, fmt.Errorf("failed to copy staged binary for verification: %w", err)
	}
	if err := verifyChecksum(trustedPath, expectedHash); err != nil {
		cleanup()
		return false, err
	}
	currentPath, err := os.Executable()
	if err != nil {
		cleanup()
		return false, err
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		cleanup()
		return false, err
	}
	if err := replaceBinary(currentPath, trustedPath); err != nil {
		cleanup()
		return false, err
	}
	cleanup()
	return true, nil
}

func validReleaseTag(tag string) bool {
	if len(tag) == 0 || len(tag) > 128 {
		return false
	}
	for i, value := range tag {
		if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || (i > 0 && strings.ContainsRune("._+-", value)) {
			continue
		}
		return false
	}
	return true
}

func (u *Updater) isNewerRelease(release *Release) bool {
	if release == nil {
		return false
	}
	if u.cfg.Branch == "devel" && release.TagName == "devel" && u.cfg.BuildEpoch > 0 {
		return release.PublishedAt.Unix() > u.cfg.BuildEpoch
	}
	return newerVersion(release.TagName, u.cfg.CurrentVersion)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile copies src to dst and applies mode on every supported platform.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func (u *Updater) releaseByTag(ctx context.Context, tag string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases", u.cfg.APIBaseURL, u.cfg.Repo)
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release lookup failed: %d", resp.StatusCode)
	}
	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	for i := range releases {
		if releases[i].TagName == tag {
			return &releases[i], nil
		}
	}
	return nil, fmt.Errorf("staged release %q is no longer available", tag)
}

func newerVersion(candidate, current string) bool {
	parse := func(value string) ([]uint64, string, bool) {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		parts := strings.SplitN(value, "-", 2)
		numbers := strings.Split(parts[0], ".")
		if len(numbers) < 2 || len(numbers) > 3 {
			return nil, "", false
		}
		parsed := make([]uint64, 3)
		for i, number := range numbers {
			n, err := strconv.ParseUint(number, 10, 64)
			if err != nil {
				return nil, "", false
			}
			parsed[i] = n
		}
		prerelease := ""
		if len(parts) == 2 {
			prerelease = parts[1]
		}
		return parsed, prerelease, true
	}
	newParts, newPre, newOK := parse(candidate)
	oldParts, oldPre, oldOK := parse(current)
	if !newOK {
		return false
	}
	if !oldOK {
		return true
	}
	for i := range newParts {
		if newParts[i] != oldParts[i] {
			return newParts[i] > oldParts[i]
		}
	}
	if oldPre != "" && newPre == "" {
		return true
	}
	if newPre == "" || oldPre == "" {
		return false
	}
	newIDs := strings.Split(newPre, ".")
	oldIDs := strings.Split(oldPre, ".")
	for i := 0; i < len(newIDs) && i < len(oldIDs); i++ {
		newNumber, newErr := strconv.ParseUint(newIDs[i], 10, 64)
		oldNumber, oldErr := strconv.ParseUint(oldIDs[i], 10, 64)
		switch {
		case newErr == nil && oldErr == nil && newNumber != oldNumber:
			return newNumber > oldNumber
		case newErr == nil && oldErr != nil:
			return true
		case newErr != nil && oldErr == nil:
			return false
		case newIDs[i] != oldIDs[i]:
			return newIDs[i] > oldIDs[i]
		}
	}
	return len(newIDs) > len(oldIDs)
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

	tempDir, err := apppath.TempDir()
	if err != nil {
		return "", fmt.Errorf("failed to create project temp directory: %w", err)
	}
	tmpFile, err := os.CreateTemp(tempDir, u.cfg.BinaryName+"-update-*")
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	// Remove the temp file on any error path below.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempDir)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		_ = tmpFile.Close()
		return "", err
	}
	req.Header.Set("User-Agent", u.cfg.UserAgent)

	resp, err := u.cfg.Client.Do(req)
	if err != nil {
		_ = tmpFile.Close()
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_ = tmpFile.Close()
		return "", fmt.Errorf("binary download failed: %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = f.Close() }()

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
	isDevel := r.TagName == "devel"
	switch branch {
	case "beta":
		return isBeta
	case "devel":
		return isBeta || isDevel
	default:
		return false
	}
}

func normalizeBranch(branch string) string {
	switch strings.ToLower(strings.TrimSpace(branch)) {
	case "", "stable":
		return "stable"
	case "beta":
		return "beta"
	case "devel", "daily", "rolling", "dev", "development":
		return "devel"
	default:
		return strings.ToLower(strings.TrimSpace(branch))
	}
}
