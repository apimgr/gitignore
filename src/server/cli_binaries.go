package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/go-chi/chi/v5"
)

const cliBinaryPrefix = "gitignore-cli-"

type cliBinaryVersion struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type cliCatalogEntry struct {
	fingerprint string
	versions    map[string]cliBinaryVersion
}

var cliCatalogCache = struct {
	sync.RWMutex
	entries map[string]cliCatalogEntry
}{entries: make(map[string]cliCatalogEntry)}

func (s *Server) cliBinaryDir() string {
	cfg := s.currentConfig()
	if cfg == nil {
		return ""
	}
	dataDir := ""
	if s.config != nil && s.config.Paths != nil {
		dataDir = s.config.Paths.GetDataDir()
	}
	dir := strings.TrimSpace(cfg.CLI.BinaryDownload.Dir)
	if dir == "" {
		return filepath.Join(dataDir, "cli")
	}
	dir = strings.NewReplacer("${DATA_DIR}", dataDir, "$DATA_DIR", dataDir, "{data_dir}", dataDir).Replace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(dataDir, dir)
	}
	return filepath.Clean(dir)
}

func (s *Server) cliBinaryVersions() map[string]cliBinaryVersion {
	dir := s.cliBinaryDir()
	if dir == "" {
		return map[string]cliBinaryVersion{}
	}
	versions, err := loadCLIBinaryVersions(dir)
	if err != nil {
		log.Printf("WARN: unable to inspect CLI release directory %s: %v", filepath.Base(dir), err)
		return map[string]cliBinaryVersion{}
	}
	return versions
}

func loadCLIBinaryVersions(dir string) (map[string]cliBinaryVersion, error) {
	versionData, err := os.ReadFile(filepath.Join(dir, "version.txt"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]cliBinaryVersion{}, nil
		}
		return nil, err
	}
	version := strings.TrimSpace(string(versionData))
	if version == "" || strings.ContainsAny(version, "\r\n") {
		return map[string]cliBinaryVersion{}, nil
	}
	checksums, err := readCLIChecksums(filepath.Join(dir, "sha256.txt"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]cliBinaryVersion{}, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fingerprint := cliCatalogFingerprint(dir, entries)
	cliCatalogCache.RLock()
	cached, ok := cliCatalogCache.entries[dir]
	cliCatalogCache.RUnlock()
	if ok && cached.fingerprint == fingerprint {
		return cloneCLICatalog(cached.versions), nil
	}
	versions := make(map[string]cliBinaryVersion)
	for _, entry := range entries {
		name := entry.Name()
		platform, supported := cliPlatformFromName(name)
		if !supported || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		contentHash, err := fileSHA256(path)
		if err != nil {
			log.Printf("WARN: unable to hash CLI release file %s: %v", name, err)
			continue
		}
		expected := checksums[name]
		if expected == "" || !strings.EqualFold(expected, contentHash) {
			log.Printf("WARN: CLI release file checksum mismatch: %s", name)
			continue
		}
		versions[platform] = cliBinaryVersion{Version: version, SHA256: contentHash}
	}
	cliCatalogCache.Lock()
	cliCatalogCache.entries[dir] = cliCatalogEntry{fingerprint: fingerprint, versions: cloneCLICatalog(versions)}
	cliCatalogCache.Unlock()
	return versions, nil
}

func cliPlatformFromName(name string) (string, bool) {
	isWindowsExecutable := strings.HasSuffix(name, ".exe")
	if isWindowsExecutable {
		name = strings.TrimSuffix(name, ".exe")
	} else if strings.HasSuffix(name, ".EXE") {
		return "", false
	}
	if !strings.HasPrefix(name, cliBinaryPrefix) {
		return "", false
	}
	platform := strings.TrimPrefix(name, cliBinaryPrefix)
	parts := strings.Split(platform, "-")
	if len(parts) != 2 || (parts[0] != "linux" && parts[0] != "darwin" && parts[0] != "windows" && parts[0] != "freebsd") || (parts[1] != "amd64" && parts[1] != "arm64") {
		return "", false
	}
	if parts[0] == "windows" && !isWindowsExecutable {
		return "", false
	}
	if parts[0] != "windows" && isWindowsExecutable {
		return "", false
	}
	return platform, true
}

func readCLIChecksums(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	checksums := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			continue
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) == name && name != "." {
			checksums[name] = fields[0]
		}
	}
	return checksums, nil
}

func cliCatalogFingerprint(dir string, entries []os.DirEntry) string {
	var builder strings.Builder
	for _, name := range []string{"version.txt", "sha256.txt"} {
		appendCLIFingerprint(&builder, filepath.Join(dir, name))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, ok := cliPlatformFromName(entry.Name()); ok {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		appendCLIFingerprint(&builder, filepath.Join(dir, name))
	}
	return builder.String()
}

func appendCLIFingerprint(builder *strings.Builder, path string) {
	info, err := os.Stat(path)
	if err != nil {
		builder.WriteString(filepath.Base(path) + ":missing;")
		return
	}
	fmt.Fprintf(builder, "%s:%d:%d;", filepath.Base(path), info.Size(), info.ModTime().UnixNano())
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func cloneCLICatalog(source map[string]cliBinaryVersion) map[string]cliBinaryVersion {
	clone := make(map[string]cliBinaryVersion, len(source))
	for platform, version := range source {
		clone[platform] = version
	}
	return clone
}

func (s *Server) handleCLIBinary(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	platform, ok := cliPlatformFromName(filename)
	if !ok || filepath.Base(filename) != filename {
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	cfg := s.currentConfig()
	if cfg == nil {
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	if cfg.CLI.BinaryDownload.RequireAuth {
		_, err := s.authenticateRequest(r, false)
		if err != nil {
			if strings.HasPrefix(r.UserAgent(), "gitignore-cli/") {
				details := map[string]interface{}{"os_arch": platform}
				if prefix := bearerPrefix(r.Header.Get("Authorization")); prefix != "" {
					details["token_prefix"] = prefix
				}
				s.logCLIAudit("cli.token_rejected", s.clientIP(r), details, "failure")
			}
			sendAPIResponseErrorLocalized(w, r, "UNAUTHORIZED")
			return
		}
	}
	versions := s.cliBinaryVersions()
	published, ok := versions[platform]
	if !ok {
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	path := filepath.Join(s.cliBinaryDir(), filename)
	file, err := os.Open(path)
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), published.SHA256) {
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
		return
	}
	s.logCLIAudit("cli.update_started", s.clientIP(r), map[string]interface{}{"os_arch": platform, "version": published.Version}, "success")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, file); err != nil {
		log.Printf("ERROR: failed to stream CLI binary %s: %v", filename, err)
	}
}

func bearerPrefix(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	if len(parts[1]) > 12 {
		return parts[1][:12]
	}
	return parts[1]
}

func (s *Server) logCLIAudit(event, ip string, details map[string]interface{}, result string) {
	if s.config == nil || s.config.Paths == nil {
		log.Printf("WARN: cannot record audit event %s because log path is unavailable", event)
		return
	}
	category, severity := "server", "info"
	if event == "cli.token_rejected" {
		category, severity = "security", "warning"
	}
	if err := audit.Append(s.config.Paths.GetLogsDir(), audit.Entry{
		Event: event, Category: category, Severity: severity, Actor: audit.Actor{Type: "api", IP: ip},
		Details: details, Result: result,
	}); err != nil {
		log.Printf("ERROR: unable to record CLI audit event %s: %v", event, err)
	}
}

func (s *Server) addCLIDiscovery(data map[string]interface{}) {
	cfg := s.currentConfig()
	data["cli_versions"] = s.cliBinaryVersions()
	if cfg != nil {
		data["cli_min_version"] = cfg.Server.Update.CLIMinVersion
	} else {
		data["cli_min_version"] = ""
	}
}
