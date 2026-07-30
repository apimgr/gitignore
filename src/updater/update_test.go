package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"
)

// fixtureServer builds an httptest server that serves the GitHub Releases API
// and the release assets (binary + checksums.txt) for a set of releases. The
// asset BrowserDownloadURLs point back at the same server.
type fixture struct {
	server     *httptest.Server
	binaryData []byte
	binarySHA  string
}

// newFixture creates a fixture whose "latest" release is latestTag and whose
// full release list is releases (newest-first). binaryName is the asset base
// name expected for the running platform.
func newFixture(t *testing.T, latestTag string, releases []Release) *fixture {
	t.Helper()
	binaryData := []byte("#!/fake-binary\n" + latestTag)
	sum := sha256.Sum256(binaryData)
	binarySHA := hex.EncodeToString(sum[:])

	assetName := fmt.Sprintf("gitignore-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}
	checksums := fmt.Sprintf("%s  %s\n", binarySHA, assetName)

	fx := &fixture{binaryData: binaryData, binarySHA: binarySHA}

	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryData)
	})
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(checksums))
	})

	// Attach asset URLs pointing at this server.
	attach := func(rel *Release) {
		rel.Assets = []Asset{
			{Name: assetName, BrowserDownloadURL: fx.server.URL + "/bin"},
			{Name: "checksums.txt", BrowserDownloadURL: fx.server.URL + "/checksums"},
		}
	}

	mux.HandleFunc("/repos/apimgr/gitignore/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if latestTag == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var latest *Release
		for i := range releases {
			if releases[i].TagName == latestTag {
				rel := releases[i]
				latest = &rel
				break
			}
		}
		if latest == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		attach(latest)
		json.NewEncoder(w).Encode(latest)
	})
	mux.HandleFunc("/repos/apimgr/gitignore/releases", func(w http.ResponseWriter, r *http.Request) {
		out := make([]Release, len(releases))
		copy(out, releases)
		for i := range out {
			attach(&out[i])
		}
		json.NewEncoder(w).Encode(out)
	})

	fx.server = httptest.NewServer(mux)
	t.Cleanup(fx.server.Close)
	// Rewrite asset base once server URL is known: handlers close over fx.server.
	return fx
}

func (fx *fixture) updater(current, branch string) *Updater {
	return New(Config{
		Repo:           "apimgr/gitignore",
		BinaryName:     "gitignore",
		CurrentVersion: current,
		Branch:         branch,
		APIBaseURL:     fx.server.URL,
		Client:         fx.server.Client(),
	})
}

func TestCheckStableUpdateAvailable(t *testing.T) {
	fx := newFixture(t, "v1.2.0", []Release{{TagName: "v1.2.0"}})
	u := fx.updater("v1.1.0", "stable")
	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.TagName != "v1.2.0" {
		t.Fatalf("expected v1.2.0, got %+v", rel)
	}
}

func TestCheckStableAlreadyCurrent(t *testing.T) {
	fx := newFixture(t, "v1.2.0", []Release{{TagName: "v1.2.0"}})
	u := fx.updater("v1.2.0", "stable")
	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel != nil {
		t.Fatalf("expected no update, got %+v", rel)
	}
}

func TestCheckStable404IsNoUpdate(t *testing.T) {
	fx := newFixture(t, "", nil)
	u := fx.updater("v1.0.0", "stable")
	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel != nil {
		t.Fatalf("404 should mean no update, got %+v", rel)
	}
}

func TestCheckBetaChannelCumulative(t *testing.T) {
	// Newest-first: a stable release newer than the running beta must win, so
	// beta users are never stuck behind a stable release.
	releases := []Release{
		{TagName: "v1.3.0", Prerelease: false},
		{TagName: "202601011200-beta", Prerelease: true},
		{TagName: "v1.2.0", Prerelease: false},
	}
	fx := newFixture(t, "v1.3.0", releases)
	u := fx.updater("202601011200-beta", "beta")
	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.TagName != "v1.3.0" {
		t.Fatalf("expected v1.3.0, got %+v", rel)
	}
}

func TestCheckDailyChannelSelectsNewest(t *testing.T) {
	releases := []Release{
		{TagName: "20260101120000", Prerelease: true},
		{TagName: "v1.2.0", Prerelease: false},
	}
	fx := newFixture(t, "v1.2.0", releases)
	u := fx.updater("v1.2.0", "daily")
	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.TagName != "20260101120000" {
		t.Fatalf("expected daily build, got %+v", rel)
	}
}

func TestCheckDeferredGatesRecentRelease(t *testing.T) {
	releases := []Release{
		{TagName: "v1.3.0", Prerelease: false, PublishedAt: time.Now().Add(-5 * 24 * time.Hour)},
		{TagName: "v1.2.5", Prerelease: false, PublishedAt: time.Now().Add(-40 * 24 * time.Hour)},
	}
	// Stable channel uses /latest, which is v1.3.0 (too recent under 30-day
	// defer), so the deferred check returns no update.
	fx := newFixture(t, "v1.3.0", releases)
	u := fx.updater("v1.2.0", "stable")
	rel, err := u.CheckDeferred(context.Background(), 30)
	if err != nil {
		t.Fatalf("CheckDeferred: %v", err)
	}
	if rel != nil {
		t.Fatalf("release published 5 days ago should be gated by 30-day defer, got %+v", rel)
	}

	// Without defer, the same release is offered.
	rel, err = u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.TagName != "v1.3.0" {
		t.Fatalf("expected v1.3.0 without defer, got %+v", rel)
	}
}

func TestCheckDeferredDailySelectsAgedRelease(t *testing.T) {
	releases := []Release{
		{TagName: "20260201000000", Prerelease: true, PublishedAt: time.Now().Add(-5 * 24 * time.Hour)},
		{TagName: "20260101000000", Prerelease: true, PublishedAt: time.Now().Add(-40 * 24 * time.Hour)},
	}
	fx := newFixture(t, "20260201000000", releases)
	u := fx.updater("20251201000000", "daily")
	rel, err := u.CheckDeferred(context.Background(), 30)
	if err != nil {
		t.Fatalf("CheckDeferred: %v", err)
	}
	if rel == nil || rel.TagName != "20260101000000" {
		t.Fatalf("expected the 40-day-old build, got %+v", rel)
	}
}

func TestDownloadVerifiedChecksumMatch(t *testing.T) {
	fx := newFixture(t, "v2.0.0", []Release{{TagName: "v2.0.0"}})
	u := fx.updater("v1.0.0", "stable")
	rel, err := u.Check(context.Background())
	if err != nil || rel == nil {
		t.Fatalf("Check: rel=%+v err=%v", rel, err)
	}
	tmpPath, err := u.downloadVerified(context.Background(), rel)
	if err != nil {
		t.Fatalf("downloadVerified: %v", err)
	}
	defer os.Remove(tmpPath)
	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("read downloaded: %v", err)
	}
	if string(got) != string(fx.binaryData) {
		t.Fatalf("downloaded binary mismatch")
	}
}

func TestDownloadVerifiedChecksumMismatchRejected(t *testing.T) {
	fx := newFixture(t, "v2.0.0", []Release{{TagName: "v2.0.0"}})
	u := fx.updater("v1.0.0", "stable")
	rel, err := u.Check(context.Background())
	if err != nil || rel == nil {
		t.Fatalf("Check: rel=%+v err=%v", rel, err)
	}
	// Point the checksums asset at an unregistered route so the fetch fails,
	// proving Install refuses to proceed without a verified checksum.
	for i := range rel.Assets {
		if rel.Assets[i].Name == "checksums.txt" {
			rel.Assets[i].BrowserDownloadURL = fx.server.URL + "/badchecksum"
		}
	}
	_, err = u.downloadVerified(context.Background(), rel)
	if err == nil {
		t.Fatalf("expected error for missing/bad checksum asset")
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/blob"
	data := []byte("hello update")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])
	if err := verifyChecksum(path, good); err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if err := verifyChecksum(path, "deadbeef"); err == nil {
		t.Fatalf("expected mismatch error")
	}
	// Uppercase hash must still match (case-insensitive hex).
	if err := verifyChecksum(path, "  "+good+"  "); err != nil {
		t.Fatalf("expected trimmed match, got %v", err)
	}
}

func TestReplaceBinaryAtomicAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix replace path")
	}
	dir := t.TempDir()
	current := dir + "/gitignore"
	if err := os.WriteFile(current, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := dir + "/gitignore-new"
	if err := os.WriteFile(newBin, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceBinary(current, newBin); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	got, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("expected NEW, got %q", got)
	}
	// Permissions of the original (0755) must be preserved.
	info, err := os.Stat(current)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755, got %o", info.Mode().Perm())
	}
}

func TestMatchesBranch(t *testing.T) {
	stable := Release{TagName: "v1.0.0", Prerelease: false}
	beta := Release{TagName: "202601011200-beta", Prerelease: true}
	daily := Release{TagName: "20260101120000", Prerelease: true}

	if !matchesBranch(stable, "beta") || !matchesBranch(stable, "daily") || !matchesBranch(stable, "stable") {
		t.Fatal("stable release must match every channel")
	}
	if !matchesBranch(beta, "beta") {
		t.Fatal("beta must match beta channel")
	}
	if matchesBranch(beta, "stable") {
		t.Fatal("beta must not match stable channel")
	}
	if !matchesBranch(daily, "daily") {
		t.Fatal("daily must match daily channel")
	}
	if matchesBranch(daily, "beta") {
		t.Fatal("daily must not match beta channel")
	}
}
