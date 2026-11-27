package server

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	apppath "github.com/apimgr/gitignore/src/path"
)

func TestLoadCLIBinaryVersionsRequiresMatchingPublishedChecksum(t *testing.T) {
	directory := t.TempDir()
	name := "gitignore-cli-linux-amd64"
	binary := []byte("test CLI binary")
	if err := os.WriteFile(filepath.Join(directory, name), binary, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	if err := os.WriteFile(filepath.Join(directory, "version.txt"), []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "sha256.txt"), []byte(fmt.Sprintf("%x  %s\n", digest, name)), 0o600); err != nil {
		t.Fatal(err)
	}
	versions, err := loadCLIBinaryVersions(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := versions["linux-amd64"]; got.Version != "1.2.3" || got.SHA256 != fmt.Sprintf("%x", digest) {
		t.Fatalf("published version = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte("tampered CLI binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	versions, err = loadCLIBinaryVersions(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 0 {
		t.Fatalf("tampered binary was published: %+v", versions)
	}
}

func TestCLIBinaryPlatformNames(t *testing.T) {
	valid := []string{
		"gitignore-cli-linux-amd64", "gitignore-cli-linux-arm64",
		"gitignore-cli-darwin-amd64", "gitignore-cli-darwin-arm64",
		"gitignore-cli-windows-amd64.exe", "gitignore-cli-windows-arm64.exe",
		"gitignore-cli-freebsd-amd64", "gitignore-cli-freebsd-arm64",
	}
	for _, name := range valid {
		if _, ok := cliPlatformFromName(name); !ok {
			t.Errorf("cliPlatformFromName(%q) rejected supported artifact", name)
		}
	}
	for _, name := range []string{"gitignore-cli-windows-amd64", "gitignore-cli-linux-amd64.exe", "gitignore-cli-plan9-amd64", "../gitignore-cli-linux-amd64"} {
		if _, ok := cliPlatformFromName(name); ok {
			t.Errorf("cliPlatformFromName(%q) accepted invalid artifact", name)
		}
	}
}

func TestCLIBinaryRouteStreamsOnlyVerifiedArtifacts(t *testing.T) {
	directory := t.TempDir()
	name := "gitignore-cli-linux-amd64"
	binary := []byte("published CLI")
	if err := os.WriteFile(filepath.Join(directory, name), binary, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	if err := os.WriteFile(filepath.Join(directory, "version.txt"), []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "sha256.txt"), []byte(fmt.Sprintf("%x  %s\n", digest, name)), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.CLI.BinaryDownload.Dir = directory
	paths := apppath.New()
	paths.SetDataDir(t.TempDir())
	paths.SetLogsDir(t.TempDir())
	s := New(&ServerOptions{Version: "test", Cfg: cfg, Paths: paths})
	request := httptest.NewRequest(http.MethodGet, "/cli/binaries/"+name, nil)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.EqualFold(response.Header().Get("Content-Type"), "application/octet-stream") || !strings.EqualFold(response.Body.String(), string(binary)) {
		t.Fatalf("binary response = status %d, content-type %q, body %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}
