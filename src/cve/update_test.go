package cve

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateNVDStoresAndMergesRecords(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("resultsPerPage") != "2000" {
			t.Errorf("resultsPerPage = %q", r.URL.Query().Get("resultsPerPage"))
		}
		if requests > 2 && r.URL.Query().Get("lastModStartDate") == "" {
			t.Error("incremental request omitted lastModStartDate")
		}
		if r.URL.Query().Get("startIndex") == "0" {
			_, _ = w.Write([]byte(`{"totalResults":2,"vulnerabilities":[{"cve":{"id":"CVE-2026-0001","summary":"sample"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"totalResults":2,"vulnerabilities":[{"cve":{"id":"CVE-2026-0002","summary":"second page"}}]}`))
	}))
	defer server.Close()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "security")
	cfg := Config{SecurityDir: dir, NVDSource: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	if err := Update(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cve", "nvd.json")
	records, err := readNVD(path)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(records) != 2 || !bytes.Contains(records["CVE-2026-0001"], []byte("sample")) || !bytes.Contains(records["CVE-2026-0002"], []byte("second page")) {
		t.Fatalf("requests=%d records=%v", requests, records)
	}
	now = now.Add(time.Hour)
	if err := Update(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if requests != 4 {
		t.Fatalf("incremental update requests = %d", requests)
	}
}

func TestUpdateTrivyPullsAndSkipsFreshDatabase(t *testing.T) {
	var layer bytes.Buffer
	compressed := gzip.NewWriter(&layer)
	tarWriter := tar.NewWriter(compressed)
	nextUpdate := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	for name, content := range map[string][]byte{
		"trivy.db":      []byte("database"),
		"metadata.json": []byte(`{"NextUpdate":"` + nextUpdate + `"}`),
	} {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/v2/aquasecurity/trivy-db/manifests/2":
			_, _ = w.Write([]byte(`{"layers":[{"mediaType":"` + trivyLayerType + `","digest":"sha256:fixture"}]}`))
		case "/v2/aquasecurity/trivy-db/blobs/sha256:fixture":
			_, _ = w.Write(layer.Bytes())
		default:
			t.Errorf("unexpected OCI path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "trivy")
	registry := strings.TrimPrefix(server.URL, "https://") + "/aquasecurity/trivy-db:2"
	cfg := Config{HTTPClient: server.Client(), TrivySource: registry, Now: time.Now}
	if err := updateTrivy(context.Background(), dir, cfg); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "trivy.db")); err != nil || string(got) != "database" {
		t.Fatalf("pulled Trivy DB = %q, %v", got, err)
	}
	if got := HealthStatus(filepath.Dir(dir), "trivy", time.Now().UTC()); got != "ok" {
		t.Fatalf("fresh Trivy DB health = %q", got)
	}
	if err := updateTrivy(context.Background(), dir, cfg); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("fresh database triggered additional OCI requests: %d", requests)
	}
}

func TestUpdateNVDRateLimitKeepsLastGoodData(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "security")
	cveDir := filepath.Join(dir, "cve")
	if err := os.MkdirAll(cveDir, 0700); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(cveDir, "nvd.json")
	stampPath := filepath.Join(cveDir, ".last_updated")
	if err := os.WriteFile(dataPath, []byte(`[{"id":"CVE-2026-9000","summary":"last good"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeStamp(stampPath, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	beforeData, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeStamp, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{SecurityDir: dir, NVDSource: server.URL, NVDAPIKey: "test-key", HTTPClient: server.Client(), Now: func() time.Time { return now }}
	if err := Update(context.Background(), cfg); err == nil {
		t.Fatal("NVD rate-limit response unexpectedly succeeded")
	}
	afterData, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	afterStamp, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeData, afterData) || !bytes.Equal(beforeStamp, afterStamp) {
		t.Fatal("rate-limited update changed the last good NVD data or timestamp")
	}
}

func TestFetchTrivyManifestAuthenticatesWithBearerToken(t *testing.T) {
	requests := 0
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("service") != "registry" || r.URL.Query().Get("scope") != "repository:repo:pull" {
				t.Errorf("token query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"access_token":"fixture-token"}`))
		case "/v2/repo/manifests/2":
			if r.Header.Get("Authorization") != "Bearer fixture-token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+server.URL+`/token",service="registry",scope="repository:repo:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"layers":[]}`))
		default:
			t.Errorf("unexpected registry request %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ref, err := parseTrivyReference(strings.TrimPrefix(server.URL, "https://") + "/repo:2")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := fetchTrivyManifest(context.Background(), server.Client(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Layers) != 0 || requests != 3 {
		t.Fatalf("manifest=%+v requests=%d", manifest, requests)
	}
}

func TestParseTrivyReferenceAndLayerExtraction(t *testing.T) {
	ref, err := parseTrivyReference("ghcr.io/aquasecurity/trivy-db:2")
	if err != nil || ref.registry != "ghcr.io" || ref.repo != "aquasecurity/trivy-db" || ref.tag != "2" {
		t.Fatalf("reference = %+v, err=%v", ref, err)
	}
	if _, err := parseTrivyReference("https://example.test/image:tag"); err == nil {
		t.Fatal("URL scheme unexpectedly accepted as OCI reference")
	}
	var layer bytes.Buffer
	compressed := gzip.NewWriter(&layer)
	tarWriter := tar.NewWriter(compressed)
	for name, content := range map[string][]byte{"trivy.db": []byte("database"), "metadata.json": []byte(`{"NextUpdate":"2026-11-23T00:00:00Z"}`)} {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := extractTrivyLayer(dir, bytes.NewReader(layer.Bytes())); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "trivy.db")); err != nil || string(data) != "database" {
		t.Fatalf("Trivy DB = %q, err=%v", data, err)
	}
	metadata, err := readTrivyMetadata(filepath.Join(dir, "metadata.json"))
	if err != nil || metadata.NextUpdate.IsZero() {
		t.Fatalf("metadata = %+v, err=%v", metadata, err)
	}
}

func TestReadNVDRejectsMalformedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nvd.json")
	if err := os.WriteFile(path, []byte(`[{}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNVD(path); err == nil {
		t.Fatal("malformed record unexpectedly accepted")
	}
}

func TestHealthStatusForNVDDatabase(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	securityDir := t.TempDir()
	cveDir := filepath.Join(securityDir, "cve")
	if err := os.MkdirAll(cveDir, 0700); err != nil {
		t.Fatal(err)
	}
	if got := HealthStatus(securityDir, "nvd", now); got != "error" {
		t.Fatalf("missing NVD database state = %q, want error", got)
	}
	if err := os.WriteFile(filepath.Join(cveDir, "nvd.json"), []byte(`[{"id":"CVE-2026-0001"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeStamp(filepath.Join(cveDir, ".last_updated"), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := HealthStatus(securityDir, "nvd", now); got != "ok" {
		t.Fatalf("fresh NVD database state = %q, want ok", got)
	}
	if err := writeStamp(filepath.Join(cveDir, ".last_updated"), now.Add(-49*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := HealthStatus(securityDir, "nvd", now); got != "degraded" {
		t.Fatalf("stale NVD database state = %q, want degraded", got)
	}
}
