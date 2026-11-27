package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/diskcheck"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/pgp"
	"github.com/apimgr/gitignore/src/scheduler"
)

// Backup encryption parameters (AI.md PART 21). Argon2id derives the AES-256
// key from the operator password; the key is never persisted and the password
// is never stored. The authenticated cleartext header carries the salt, KDF
// parameters, nonce, and non-secret key identifier.
const (
	backupManifestVersion      = "1.0.0"
	backupArgonTime            = 3
	backupArgonMemory          = 64 * 1024
	backupArgonThreads         = 4
	backupKeyLen               = 32
	backupSaltLen              = 16
	backupNonceLen             = 12
	backupMagic                = "BKUP1"
	backupKeyIDLen             = 8
	backupBaseNameMax          = 255
	backupHeaderLen            = len(backupMagic) + backupSaltLen + 4 + 4 + 4 + backupNonceLen + backupKeyIDLen + 2
	legacyBackupExtensionMagic = "BKX1"
)

// backupManifest is the manifest.json embedded at the root of every archive.
// Checksums maps each archived file path to its SHA-256, letting restore
// verify integrity before any file is installed (AI.md PART 21 → "Manifest").
type backupManifest struct {
	Version          string               `json:"version"`
	CreatedAt        string               `json:"created_at"`
	SnapshotAt       string               `json:"snapshot_at"`
	CreatedBy        string               `json:"created_by"`
	AppVersion       string               `json:"app_version"`
	Type             string               `json:"type"`
	SHA256           string               `json:"sha256"`
	BaseBackup       string               `json:"base_backup,omitempty"`
	BaseSHA256       string               `json:"base_sha256,omitempty"`
	Deleted          []string             `json:"deleted,omitempty"`
	Contents         []string             `json:"contents"`
	Checksums        map[string]string    `json:"checksums"`
	Files            []backupManifestFile `json:"files"`
	Encrypted        bool                 `json:"encrypted"`
	EncryptionMethod string               `json:"encryption_method,omitempty"`
}

type backupManifestFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified int64  `json:"mtime"`
	SHA256   string `json:"sha256"`
}

// backupSource pairs an on-disk file with the path it takes inside the archive.
type backupSource struct {
	archivePath string
	diskPath    string
}

// collectBackupSources gathers every file the backup must contain: the config
// file, the database, and any custom template/ or theme/ directories under the
// config dir (AI.md PART 21 → "Backup Contents").
func collectBackupSources(configDir, dataDir string) ([]backupSource, error) {
	var sources []backupSource

	add := func(archivePath, diskPath string) {
		if fi, err := os.Stat(diskPath); err == nil && !fi.IsDir() {
			sources = append(sources, backupSource{archivePath: archivePath, diskPath: diskPath})
		}
	}

	add("server.yml", filepath.Join(configDir, "server.yml"))
	add("secrets.yml", filepath.Join(configDir, "secrets.yml"))
	add("db/server.db", filepath.Join(dataDir, "db", "server.db"))

	for _, sub := range []string{"yaml.d", "assets", "template", "theme", "security"} {
		root := filepath.Join(configDir, sub)
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(configDir, p)
			if err != nil {
				return err
			}
			sources = append(sources, backupSource{
				archivePath: filepath.ToSlash(rel),
				diskPath:    p,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return sources, nil
}

func collectExternalSSL(configDir string, sources *[]backupSource) error {
	configPath := filepath.Join(configDir, "server.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if len(root.Content) == 0 {
		return errors.New("server.yml is empty")
	}
	server := yamlMappingValue(root.Content[0], "server")
	ssl := yamlMappingValue(server, "ssl")
	for _, field := range []string{"cert", "key"} {
		node := yamlMappingValue(ssl, field)
		if node == nil || strings.TrimSpace(node.Value) == "" {
			continue
		}
		path := node.Value
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, path)
		}
		sslDir, _ := filepath.Abs(filepath.Join(configDir, "ssl"))
		absPath, _ := filepath.Abs(path)
		if rel, relErr := filepath.Rel(sslDir, absPath); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("referenced server.ssl.%s file %s: %w", field, node.Value, err)
		}
		*sources = append(*sources, backupSource{archivePath: "ssl-external/" + field, diskPath: path})
	}
	return nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// buildBackupArchive builds a gzip-compressed tar in memory containing the
// manifest and all source files, returning the raw archive bytes.
func buildBackupArchive(sources []backupSource, encrypted bool) ([]byte, error) {
	return buildBackupArchiveAt(sources, encrypted, time.Now().UTC(), "full", "", "", nil)
}

func buildBackupArchiveAt(sources []backupSource, encrypted bool, snapshotAt time.Time, backupType, baseBackup, baseSHA256 string, deleted []string) ([]byte, error) {
	manifest := backupManifest{
		Version:    backupManifestVersion,
		CreatedAt:  snapshotAt.UTC().Format(time.RFC3339),
		SnapshotAt: snapshotAt.UTC().Format(time.RFC3339),
		CreatedBy:  "operator",
		AppVersion: Version,
		Type:       backupType,
		BaseBackup: baseBackup,
		BaseSHA256: baseSHA256,
		Deleted:    append([]string(nil), deleted...),
		Checksums:  make(map[string]string),
		Files:      make([]backupManifestFile, 0, len(sources)),
		Encrypted:  encrypted,
	}
	if encrypted {
		manifest.EncryptionMethod = "AES-256-GCM"
	}

	type payload struct {
		name string
		data []byte
	}
	var files []payload

	for _, src := range sources {
		data, err := os.ReadFile(src.diskPath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", cliText("cli.backup.source_read_failed", "path", src.diskPath), err)
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])
		manifest.Checksums[src.archivePath] = checksum
		manifest.Contents = append(manifest.Contents, src.archivePath)
		info, err := os.Stat(src.diskPath)
		if err != nil {
			return nil, err
		}
		manifest.Files = append(manifest.Files, backupManifestFile{Path: src.archivePath, Size: int64(len(data)), Modified: info.ModTime().Unix(), SHA256: checksum})
		files = append(files, payload{name: src.archivePath, data: data})
	}
	manifest.SHA256 = backupFilesChecksum(manifest.Files)

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	writeEntry := func(name string, data []byte) error {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o600,
			Size:    int64(len(data)),
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}

	if err := writeEntry("manifest.json", manifestJSON); err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := writeEntry(f.name, f.data); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encryptBackup derives a key from password via Argon2id and encrypts the
// archive with AES-256-GCM. The cleartext header is authenticated as GCM AAD.
// The plaintext archive never touches disk (AI.md PART 21).
func encryptBackup(plaintext []byte, password string, baseBackup ...string) ([]byte, error) {
	salt := make([]byte, backupSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, backupArgonTime, backupArgonMemory, backupArgonThreads, backupKeyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, backupNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	header := make([]byte, 0, backupHeaderLen)
	header = append(header, backupMagic...)
	header = append(header, salt...)
	var encoded [12]byte
	binary.BigEndian.PutUint32(encoded[:4], backupArgonMemory)
	binary.BigEndian.PutUint32(encoded[4:8], backupArgonTime)
	binary.BigEndian.PutUint32(encoded[8:12], backupArgonThreads)
	header = append(header, encoded[:]...)
	header = append(header, nonce...)
	header = append(header, backupKeyID(key)...)
	base := ""
	if len(baseBackup) > 0 {
		base = baseBackup[0]
	}
	if len(base) > backupBaseNameMax || !utf8.ValidString(base) || strings.ContainsAny(base, `/\\`) || (base != "" && filepath.Base(base) != base) {
		return nil, errors.New("incremental base reference is invalid")
	}
	var baseLength [2]byte
	binary.BigEndian.PutUint16(baseLength[:], uint16(len(base)))
	header = append(header, baseLength[:]...)
	header = append(header, base...)
	ciphertext := gcm.Seal(nil, nonce, plaintext, header)
	out := make([]byte, 0, len(header)+len(ciphertext))
	out = append(out, header...)
	out = append(out, ciphertext...)
	return out, nil
}

// decryptBackup reverses encryptBackup: it splits salt/nonce/ciphertext,
// re-derives the key, and authenticates + decrypts with AES-256-GCM. A wrong
// password fails the GCM tag check and returns an error (AI.md PART 21
// "Decrypt test").
func decryptBackup(blob []byte, password string) ([]byte, error) {
	if bytes.HasPrefix(blob, []byte(backupMagic)) {
		header, err := parseBackupHeader(blob)
		if err != nil {
			return nil, err
		}
		key := argon2.IDKey([]byte(password), header.salt, header.time, header.memory, uint8(header.threads), backupKeyLen)
		if !hmac.Equal(header.keyID, backupKeyID(key)) {
			return nil, errors.New(cliText("cli.backup.wrong_password"))
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		return gcm.Open(nil, header.nonce, blob[header.headerLength:], blob[:header.headerLength])
	}
	// Backward compatibility for archives created before the authenticated
	// BKUP1 header was introduced.
	if len(blob) < backupSaltLen+backupNonceLen {
		return nil, errors.New(cliText("cli.backup.encrypted_truncated"))
	}
	salt := blob[:backupSaltLen]
	nonce := blob[backupSaltLen : backupSaltLen+backupNonceLen]
	ciphertext := blob[backupSaltLen+backupNonceLen:]

	key := argon2.IDKey([]byte(password), salt, backupArgonTime, backupArgonMemory, backupArgonThreads, backupKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New(cliText("cli.backup.decryption_failed"))
	}
	return plaintext, nil
}

type parsedBackupHeader struct {
	salt         []byte
	memory       uint32
	time         uint32
	threads      uint32
	nonce        []byte
	keyID        []byte
	baseBackup   string
	headerLength int
}

func parseBackupHeader(blob []byte) (parsedBackupHeader, error) {
	if len(blob) < backupHeaderLen+16 || string(blob[:len(backupMagic)]) != backupMagic {
		return parsedBackupHeader{}, errors.New("encrypted backup header is invalid")
	}
	pos := len(backupMagic)
	header := parsedBackupHeader{salt: blob[pos : pos+backupSaltLen]}
	pos += backupSaltLen
	// Read headers emitted by the earlier BKUP1 draft without changing the
	// canonical uint32-parameter format written by current backups.
	legacyMarker := pos + 4 + 4 + 1 + backupNonceLen + backupKeyIDLen
	if len(blob) >= legacyMarker+len(legacyBackupExtensionMagic)+2+16 && string(blob[legacyMarker:legacyMarker+len(legacyBackupExtensionMagic)]) == legacyBackupExtensionMagic {
		header.memory = binary.BigEndian.Uint32(blob[pos : pos+4])
		pos += 4
		header.time = binary.BigEndian.Uint32(blob[pos : pos+4])
		pos += 4
		header.threads = uint32(blob[pos])
		pos++
		header.nonce = blob[pos : pos+backupNonceLen]
		pos += backupNonceLen
		header.keyID = blob[pos : pos+backupKeyIDLen]
		pos += backupKeyIDLen
		pos += len(legacyBackupExtensionMagic)
		baseLength := int(binary.BigEndian.Uint16(blob[pos : pos+2]))
		pos += 2
		if baseLength > backupBaseNameMax || len(blob) < pos+baseLength+16 {
			return parsedBackupHeader{}, errors.New("encrypted backup base reference is invalid")
		}
		header.baseBackup = string(blob[pos : pos+baseLength])
		header.headerLength = pos + baseLength
	} else {
		header.memory = binary.BigEndian.Uint32(blob[pos : pos+4])
		pos += 4
		header.time = binary.BigEndian.Uint32(blob[pos : pos+4])
		pos += 4
		header.threads = binary.BigEndian.Uint32(blob[pos : pos+4])
		pos += 4
		header.nonce = blob[pos : pos+backupNonceLen]
		pos += backupNonceLen
		header.keyID = blob[pos : pos+backupKeyIDLen]
		pos += backupKeyIDLen
		if len(blob) < pos+2 {
			return parsedBackupHeader{}, errors.New("encrypted backup header is truncated")
		}
		baseLength := int(binary.BigEndian.Uint16(blob[pos : pos+2]))
		pos += 2
		if baseLength > backupBaseNameMax {
			return parsedBackupHeader{}, errors.New("encrypted backup base reference is too long")
		}
		if len(blob) < pos+baseLength+16 {
			return parsedBackupHeader{}, errors.New("encrypted backup ciphertext is truncated")
		}
		header.baseBackup = string(blob[pos : pos+baseLength])
		header.headerLength = pos + baseLength
	}
	if !utf8.ValidString(header.baseBackup) || strings.ContainsAny(header.baseBackup, `/\\`) || (header.baseBackup != "" && filepath.Base(header.baseBackup) != header.baseBackup) {
		return parsedBackupHeader{}, errors.New("encrypted backup base reference is invalid")
	}
	if header.memory < 8*1024 || header.memory > 1024*1024 || header.time == 0 || header.time > 10 || header.threads == 0 || header.threads > 32 {
		return parsedBackupHeader{}, errors.New("encrypted backup KDF parameters are invalid")
	}
	if len(blob) < header.headerLength+16 {
		return parsedBackupHeader{}, errors.New("encrypted backup ciphertext is truncated")
	}
	return header, nil
}

func backupKeyID(key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("key-id"))
	return []byte(hex.EncodeToString(mac.Sum(nil))[:backupKeyIDLen])
}

func backupArchiveKeyMatches(path, password string) (bool, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	header, err := parseBackupHeader(blob)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(password), header.salt, header.time, header.memory, uint8(header.threads), backupKeyLen)
	return hmac.Equal(header.keyID, backupKeyID(key)), nil
}

// promptPassword reads a password from the terminal without echoing it. It
// never accepts a password on the command line (AI.md PART 21 note: CLI
// passwords leak via shell history and process lists).
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New(cliText("cli.backup.password_requires_terminal"))
	}
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}

// runBackup implements `--maintenance backup [filename]`.
func runBackup(cfg *config.ServerConfig, configDir, dataDir, backupFile string) (result error) {
	defer func() {
		if result != nil {
			appendBackupAudit("backup.failed", "error", "failure", map[string]any{"filename": filepath.Base(backupFile), "error": result.Error()})
		}
	}()
	password := ""
	complianceEnabled := false
	if cfg != nil {
		password = cfg.Server.Backup.EncryptionPassword
		complianceEnabled = cfg.Server.Compliance.Enabled
	}
	if strings.HasPrefix(password, "enc:v1:") {
		return errors.New("backup.password_unreadable")
	}
	if complianceEnabled && password == "" {
		return errors.New(cliText("cli.backup.compliance_password_required", "setting", "server.backup.encryption_password"))
	}
	encrypt := password != ""

	// Ensure the filename extension matches the encryption decision.
	backupFile = normalizeBackupExt(backupFile, encrypt)

	sources, cleanupSources, err := prepareBackupSources(cfg, configDir, dataDir)
	if err != nil {
		return err
	}
	defer cleanupSources()
	if len(sources) == 0 {
		return errors.New(cliText("cli.backup.nothing_to_back_up"))
	}

	archive, err := buildBackupArchive(sources, encrypt)
	if err != nil {
		return err
	}

	out := archive
	if encrypt {
		out, err = encryptBackup(archive, password)
		if err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(backupFile), 0o755); err != nil {
		return err
	}
	if err := writeVerifiedBackupExclusive(backupFile, out, password, restoreLimits(cfg)); err != nil {
		appendBackupAudit("backup.verification_failed", "error", "failure", map[string]any{"filename": filepath.Base(backupFile), "check": err.Error()})
		return fmt.Errorf("%s: %w", cliText("cli.backup.verification_failed"), err)
	}
	if info, err := os.Stat(backupFile); err == nil {
		checksum, checksumErr := checksumFile(backupFile)
		if checksumErr != nil {
			return checksumErr
		}
		if err := db.RecordBackup(backupFile, info.Size(), checksum); err != nil {
			return fmt.Errorf("record backup metadata: %w", err)
		}
	}
	if cfg != nil {
		if _, err := applyBackupRetention(filepath.Dir(backupFile), cfg.Server.Backup.Retention, password); err != nil {
			return fmt.Errorf("apply backup retention: %w", err)
		}
	}
	if info, err := os.Stat(backupFile); err == nil {
		appendBackupAudit("backup.created", "info", "success", map[string]any{"filename": filepath.Base(backupFile), "size": info.Size(), "encrypted": encrypt, "verification_status": "verified"})
	}

	fmt.Println(cliText("cli.backup.created", "file", backupFile))
	return nil
}

func runScheduledBackup(cfg *config.ServerConfig, configDir, dataDir, backupDir, kind string, snapshotAt time.Time) (result error) {
	filename := fmt.Sprintf("%s-%s.tar.gz", projectName, kind)
	defer func() {
		if result != nil && !errors.Is(result, scheduler.ErrSkipped) {
			appendBackupAudit("backup.failed", "error", "failure", map[string]any{"filename": filename, "error": result.Error()})
		}
	}()
	if strings.HasPrefix(cfg.Server.Backup.EncryptionPassword, "enc:v1:") {
		return errors.New("backup.password_unreadable")
	}
	if _, err := applyBackupRetention(backupDir, cfg.Server.Backup.Retention, cfg.Server.Backup.EncryptionPassword); err != nil {
		return fmt.Errorf("apply pre-backup retention: %w", err)
	}
	usage, err := diskcheck.MeasureVolume(backupDir)
	if err != nil {
		return fmt.Errorf("measure backup volume: %w", err)
	}
	threshold := cfg.Server.Maintenance.Cleanup.DiskThreshold
	if threshold < 1 || threshold > 100 {
		threshold = 90
	}
	lastBackupSize := mostRecentBackupSize(backupDir, snapshotAt)
	if usage.UsedPercent > float64(threshold) || (lastBackupSize > 0 && usage.FreeBytes < uint64(lastBackupSize)*2) {
		appendBackupAudit("backup.skipped_disk_full", "error", "failure", map[string]any{"free_bytes": usage.FreeBytes, "disk_usage_percent": usage.UsedPercent, "threshold_percent": threshold})
		return scheduler.ErrSkipped
	}
	sources, cleanup, err := prepareBackupSources(cfg, configDir, dataDir)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(sources) == 0 {
		return errors.New(cliText("cli.backup.nothing_to_back_up"))
	}
	password := cfg.Server.Backup.EncryptionPassword
	if cfg.Server.Compliance.Enabled && password == "" {
		return errors.New(cliText("cli.backup.compliance_password_required", "setting", "server.backup.encryption_password"))
	}
	basePath := latestFullBackup(backupDir, snapshotAt)
	if basePath != "" && strings.HasSuffix(basePath, ".enc") != (password != "") {
		appendBackupAudit("backup.encryption_key_changed", "info", "success", map[string]any{"filename": filepath.Base(basePath), "reason": "encryption_setting_changed"})
		basePath = ""
	} else if basePath != "" && password != "" {
		matches, matchErr := backupArchiveKeyMatches(basePath, password)
		if matchErr != nil {
			appendBackupAudit("backup.base_header_invalid", "warning", "failure", map[string]any{"filename": filepath.Base(basePath)})
			basePath = ""
		} else if !matches {
			appendBackupAudit("backup.encryption_key_changed", "info", "success", map[string]any{"filename": filepath.Base(basePath)})
			basePath = ""
		}
	}
	if kind == "hourly" {
		if basePath == "" {
			return scheduler.ErrSkipped
		}
		if err := writeIncrementalBackup(cfg, sources, backupDir, basePath, "hourly", snapshotAt); err != nil {
			return err
		}
		_, err = applyBackupRetention(backupDir, cfg.Server.Backup.Retention, password)
		return err
	}
	if kind == "daily" && basePath != "" {
		if err := writeIncrementalBackup(cfg, sources, backupDir, basePath, "daily", snapshotAt); err != nil {
			return err
		}
	}
	name := scheduledFullBackupName(backupDir, snapshotAt)
	if password != "" {
		name += ".enc"
	}
	if err := writeBackupArchive(filepath.Join(backupDir, name), sources, password, snapshotAt, "full", "", "", nil, cfg.Server.Restore); err != nil {
		return err
	}
	if _, err := applyBackupRetention(backupDir, cfg.Server.Backup.Retention, password); err != nil {
		return err
	}
	return nil
}

func mostRecentBackupSize(directory string, before time.Time) int64 {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0
	}
	var size int64
	var latest time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.Contains(entry.Name(), "_backup_") {
			continue
		}
		stamp, ok := backupNameTime(entry.Name())
		if !ok || stamp.After(before) || (!latest.IsZero() && !stamp.After(latest)) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		latest, size = stamp, info.Size()
	}
	return size
}

func latestFullBackup(directory string, before time.Time) string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	var selected string
	var selectedAt time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.Contains(entry.Name(), "_backup_") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		stamp, ok := backupNameTime(entry.Name())
		if !ok || !stamp.Before(before) {
			continue
		}
		if selected == "" || stamp.After(selectedAt) {
			selected, selectedAt = path, stamp
		}
	}
	return selected
}

func scheduledFullBackupName(directory string, snapshotAt time.Time) string {
	base := fmt.Sprintf("%s_backup_%s.tar.gz", projectName, snapshotAt.Format("2006-01-02"))
	if backupNameAvailable(directory, base) {
		return base
	}
	stamp := snapshotAt.UTC()
	for offset := 0; offset < 24*60*60; offset++ {
		candidate := fmt.Sprintf("%s_backup_%s.tar.gz", projectName, stamp.Add(time.Duration(offset)*time.Second).Format("2006-01-02_150405"))
		if backupNameAvailable(directory, candidate) {
			return candidate
		}
	}
	return fmt.Sprintf("%s_backup_%s.tar.gz", projectName, time.Now().UTC().Format("2006-01-02_150405.000000000"))
}

func backupNameAvailable(directory, name string) bool {
	for _, candidate := range []string{name, name + ".enc"} {
		if _, err := os.Lstat(filepath.Join(directory, candidate)); err == nil || !os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func writeIncrementalBackup(cfg *config.ServerConfig, sources []backupSource, directory, basePath, kind string, snapshotAt time.Time) (returnErr error) {
	password := cfg.Server.Backup.EncryptionPassword
	baseSum, err := checksumFile(basePath)
	if err != nil {
		return err
	}
	baseArchive, err := readBackupArchive(basePath, password)
	if err != nil {
		return err
	}
	baseStage, err := extractToStaging(baseArchive)
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(baseStage); cleanupErr != nil {
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()
	baseManifest, err := loadAndVerifyManifest(baseStage)
	if err != nil {
		return err
	}
	baseFiles := make(map[string]backupManifestFile, len(baseManifest.Files))
	for _, file := range baseManifest.Files {
		baseFiles[file.Path] = file
	}
	current := make(map[string]backupManifestFile, len(sources))
	changed := make([]backupSource, 0, len(sources))
	for _, source := range sources {
		data, err := os.ReadFile(source.diskPath)
		if err != nil {
			return err
		}
		info, err := os.Stat(source.diskPath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		file := backupManifestFile{Path: source.archivePath, Size: int64(len(data)), Modified: info.ModTime().Unix(), SHA256: hex.EncodeToString(sum[:])}
		current[source.archivePath] = file
		old, exists := baseFiles[source.archivePath]
		if !exists || old.Size != file.Size || old.Modified != file.Modified || old.SHA256 != file.SHA256 {
			changed = append(changed, source)
		}
	}
	var deleted []string
	for name := range baseFiles {
		if _, ok := current[name]; !ok {
			deleted = append(deleted, name)
		}
	}
	sort.Strings(deleted)
	baseName := filepath.Base(basePath)
	archive, err := buildBackupArchiveAt(changed, password != "", snapshotAt, "incremental", baseName, baseSum, deleted)
	if err != nil {
		return err
	}
	if password != "" {
		archive, err = encryptBackup(archive, password, baseName)
		if err != nil {
			return err
		}
	}
	name := fmt.Sprintf("%s-%s.tar.gz", projectName, kind)
	if password != "" {
		name += ".enc"
	}
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".incremental-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name() + filepath.Ext(path)
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	_ = os.Remove(temp.Name())
	defer os.Remove(tempPath)
	if err := os.WriteFile(tempPath, archive, 0o600); err != nil {
		return err
	}
	if err := verifyBackupFileWithLimits(tempPath, password, cfg.Server.Restore); err != nil {
		appendBackupAudit("backup.verification_failed", "error", "failure", map[string]any{"filename": filepath.Base(path), "check": err.Error()})
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if err := recordBackupMetadata(path, kind); err != nil {
		return fmt.Errorf("record backup metadata: %w", err)
	}
	if kind == "daily" {
		appendBackupAudit("backup.daily_updated", "info", "success", map[string]any{"filename": filepath.Base(path), "changes_since_base": len(changed) + len(deleted)})
	}
	return nil
}

func writeBackupArchive(path string, sources []backupSource, password string, snapshotAt time.Time, backupType, baseName, baseSum string, deleted []string, limits config.RestoreConfig) error {
	archive, err := buildBackupArchiveAt(sources, password != "", snapshotAt, backupType, baseName, baseSum, deleted)
	if err != nil {
		return err
	}
	if password != "" {
		archive, err = encryptBackup(archive, password)
		if err != nil {
			return err
		}
	}
	if err := writeVerifiedBackupExclusive(path, archive, password, limits); err != nil {
		appendBackupAudit("backup.verification_failed", "error", "failure", map[string]any{"filename": filepath.Base(path), "check": err.Error()})
		return err
	}
	if err := recordBackupMetadata(path, "auto"); err != nil {
		return fmt.Errorf("record backup metadata: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		appendBackupAudit("backup.created", "info", "success", map[string]any{"filename": filepath.Base(path), "size": info.Size(), "encrypted": password != "", "verification_status": "verified"})
	}
	return nil
}

func writeVerifiedBackupExclusive(path string, archive []byte, password string, limits config.RestoreConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".backup-verify-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name() + filepath.Ext(path)
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	_ = os.Remove(temp.Name())
	defer os.Remove(tempPath)
	if err := os.WriteFile(tempPath, archive, 0o600); err != nil {
		return err
	}
	if err := verifyBackupFileWithLimits(tempPath, password, limits); err != nil {
		return err
	}
	if err := os.Link(tempPath, path); err != nil {
		return err
	}
	return nil
}

func recordBackupMetadata(path, backupType string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	checksum, err := checksumFile(path)
	if err != nil {
		return err
	}
	return db.RecordBackupAs(path, info.Size(), checksum, backupType)
}

func cfgDatabaseDriver(cfg *config.ServerConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.Server.Database.Driver) == "" {
		return "sqlite"
	}
	return cfg.Server.Database.Driver
}

func prepareBackupSources(cfg *config.ServerConfig, configDir, dataDir string) ([]backupSource, func(), error) {
	sources, err := collectBackupSources(configDir, dataDir)
	if err != nil {
		return nil, func() {}, err
	}
	directory, err := backupTempDir()
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(directory)
	}
	// App secrets are exported from the active database independently of its
	// storage driver. Remote databases without a local cache still need these
	// keys to restore encrypted application data.
	var appSecrets []struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := db.Read(context.Background(), func(ctx context.Context, conn *sql.DB) error {
		rows, err := conn.QueryContext(ctx, "SELECT name, value, COALESCE(updated_at, '') FROM app_secrets WHERE name IN (?, ?, ?, ?, ?, ?)",
			"installation_secret", "installation_secret_previous", "cookie_signing_key", "cookie_signing_key_previous", "csrf_token_secret", "csrf_token_secret_previous")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var secret struct {
				Name      string `json:"name"`
				Value     string `json:"value"`
				UpdatedAt string `json:"updated_at"`
			}
			if err := rows.Scan(&secret.Name, &secret.Value, &secret.UpdatedAt); err != nil {
				return err
			}
			appSecrets = append(appSecrets, secret)
		}
		return rows.Err()
	}); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("export app secrets: %w", err)
	}
	secretPath := filepath.Join(directory, "app_secrets.json")
	secretData, err := json.MarshalIndent(appSecrets, "", "  ")
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	if err := os.WriteFile(secretPath, append(secretData, '\n'), 0o600); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	sources = append(sources, backupSource{archivePath: "app_secrets.json", diskPath: secretPath})
	// Include configured cache connection identity files without pulling in the
	// rest of the SSL directory.
	if cfg != nil {
		for _, item := range []struct{ name, path string }{{"tls_ca", cfg.Server.Cache.TLSCA}, {"tls_cert", cfg.Server.Cache.TLSCert}, {"tls_key", cfg.Server.Cache.TLSKey}} {
			configured := item.path
			if strings.TrimSpace(configured) == "" {
				continue
			}
			path := configured
			if !filepath.IsAbs(path) {
				path = filepath.Join(configDir, path)
			}
			if _, statErr := os.Stat(path); statErr != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("referenced cache TLS file %s: %w", configured, statErr)
			}
			sources = append(sources, backupSource{archivePath: "ssl-connection/" + item.name, diskPath: path})
		}
	}
	if err := collectExternalSSL(configDir, &sources); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	createSnapshot := func(snapshot func(string) error) (string, error) {
		path := filepath.Join(directory, "server.db")
		if err := snapshot(path); err != nil {
			cleanup()
			return "", err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			cleanup()
			return "", err
		}
		return path, nil
	}
	if strings.EqualFold(strings.TrimSpace(cfgDatabaseDriver(cfg)), "sqlite") {
		if _, statErr := os.Stat(db.LocalDatabasePath(dataDir)); statErr == nil {
			path, err := createSnapshot(func(path string) error { return db.SnapshotSQLite(context.Background(), path) })
			if err != nil {
				return nil, func() {}, fmt.Errorf("snapshot server database: %w", err)
			}
			found := false
			for i := range sources {
				if sources[i].archivePath == "db/server.db" {
					sources[i].diskPath = path
					found = true
				}
			}
			if !found {
				sources = append(sources, backupSource{archivePath: "db/server.db", diskPath: path})
			}
		}
	} else {
		filtered := sources[:0]
		for _, source := range sources {
			if source.archivePath != "db/server.db" {
				filtered = append(filtered, source)
			}
		}
		sources = filtered
		if cfg != nil && cfg.Server.Database.Cache.Enabled {
			if _, err := os.Stat(db.LocalCachePath(dataDir)); err == nil {
				path, err := createSnapshot(func(path string) error { return db.SnapshotLocalCache(context.Background(), path) })
				if err != nil {
					return nil, func() {}, fmt.Errorf("snapshot local database cache: %w", err)
				}
				sources = append(sources, backupSource{archivePath: "db/server.db", diskPath: path})
			}
		}
	}
	for i := range sources {
		if sources[i].diskPath == filepath.Join(directory, "server.db") {
			continue
		}
		copyPath := filepath.Join(directory, fmt.Sprintf("source-%04d", i))
		data, readErr := os.ReadFile(sources[i].diskPath)
		if readErr != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("snapshot backup source %s: %w", sources[i].archivePath, readErr)
		}
		info, statErr := os.Stat(sources[i].diskPath)
		if statErr != nil {
			cleanup()
			return nil, func() {}, statErr
		}
		if writeErr := os.WriteFile(copyPath, data, 0o600); writeErr != nil {
			cleanup()
			return nil, func() {}, writeErr
		}
		if chtimesErr := os.Chtimes(copyPath, info.ModTime(), info.ModTime()); chtimesErr != nil {
			cleanup()
			return nil, func() {}, chtimesErr
		}
		sources[i].diskPath = copyPath
	}
	return sources, cleanup, nil
}

func backupTempDir() (string, error) {
	build, ok := debug.ReadBuildInfo()
	if !ok || build.Main.Path == "" {
		return "", errors.New("cannot determine project organization for temporary backup snapshot")
	}
	parts := strings.Split(build.Main.Path, "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("cannot determine project organization for temporary backup snapshot")
	}
	base := filepath.Join(os.TempDir(), parts[1])
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(base, projectName+"-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func checksumFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:]), nil
}

// normalizeBackupExt makes the file extension agree with whether the archive
// is encrypted: .tar.gz.enc when encrypted, .tar.gz otherwise.
func normalizeBackupExt(name string, encrypted bool) string {
	base := strings.TrimSuffix(name, ".enc")
	if !strings.HasSuffix(base, ".tar.gz") {
		base = strings.TrimSuffix(base, ".gz")
		base = strings.TrimSuffix(base, ".tar")
		base += ".tar.gz"
	}
	if encrypted {
		return base + ".enc"
	}
	return base
}

// readBackupArchive reads a backup file, decrypting it if the .enc extension
// is present, and returns the raw gzip-tar bytes.
func readBackupArchive(backupFile, password string) ([]byte, error) {
	blob, err := os.ReadFile(backupFile)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(backupFile, ".enc") {
		return decryptBackup(blob, password)
	}
	return blob, nil
}

// extractToStaging unpacks a gzip-tar archive into a fresh staging directory,
// rejecting any entry that would escape the staging root (zip-slip / path
// traversal). It returns the staging directory path; the caller must remove it.
func extractToStaging(archive []byte) (string, error) {
	return extractToStagingWithLimits(archive, config.DefaultConfig().Server.Restore)
}

func extractToStagingWithLimits(archive []byte, limits config.RestoreConfig) (string, error) {
	maxArchive := parseRestoreSize(limits.MaxArchiveSize, 1<<30)
	maxExpanded := parseRestoreSize(limits.MaxExpandedSize, 4<<30)
	maxFile := parseRestoreSize(limits.MaxFileSize, 512<<20)
	maxFiles, maxRatio := limits.MaxFiles, limits.MaxRatio
	if maxFiles < 1 {
		maxFiles = 10000
	}
	if maxRatio < 1 {
		maxRatio = 100
	}
	if int64(len(archive)) > maxArchive {
		return "", errors.New("restore archive exceeds server.restore.max_archive_size")
	}
	staging, err := apppath.TempDir()
	if err != nil {
		return "", err
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("%s: %w", cliText("cli.backup.invalid_gzip"), err)
	}
	tr := tar.NewReader(gz)

	var expanded int64
	fileCount := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = os.RemoveAll(staging)
			return "", err
		}

		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			_ = os.RemoveAll(staging)
			return "", fmt.Errorf("%s", cliText("cli.backup.unsafe_archive_path", "path", hdr.Name))
		}
		dest := filepath.Join(staging, clean)
		if !strings.HasPrefix(dest, filepath.Clean(staging)+string(os.PathSeparator)) {
			_ = os.RemoveAll(staging)
			return "", fmt.Errorf("%s", cliText("cli.backup.entry_outside_staging", "path", hdr.Name))
		}

		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				_ = os.RemoveAll(staging)
				return "", err
			}
			continue
		}
		fileCount++
		if fileCount > maxFiles || hdr.Size < 0 || hdr.Size > maxFile || expanded+hdr.Size > maxExpanded {
			_ = os.RemoveAll(staging)
			return "", errors.New("restore archive exceeds configured file or expansion limits")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			_ = os.RemoveAll(staging)
			return "", err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			_ = os.RemoveAll(staging)
			return "", err
		}
		written, copyErr := io.Copy(f, io.LimitReader(tr, maxExpanded-expanded+1))
		if copyErr != nil {
			_ = f.Close()
			_ = os.RemoveAll(staging)
			return "", copyErr
		}
		_ = f.Close()
		if written != hdr.Size || written+expanded > maxExpanded {
			_ = os.RemoveAll(staging)
			return "", errors.New("restore archive entry size exceeds configured expansion limit")
		}
		expanded += written
		if float64(expanded)/float64(len(archive)) > float64(maxRatio) {
			_ = os.RemoveAll(staging)
			return "", errors.New("restore archive exceeds server.restore.max_ratio")
		}
	}

	return staging, nil
}

func parseRestoreSize(value string, fallback int64) int64 {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return fallback
	}
	unit := int64(1)
	for suffix, multiplier := range map[string]int64{"KB": 1 << 10, "KIB": 1 << 10, "MB": 1 << 20, "MIB": 1 << 20, "GB": 1 << 30, "GIB": 1 << 30, "TB": 1 << 40, "TIB": 1 << 40, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40} {
		if strings.HasSuffix(value, suffix) {
			unit = multiplier
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
			break
		}
	}
	if strings.HasSuffix(value, "B") {
		value = strings.TrimSpace(strings.TrimSuffix(value, "B"))
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 || n > int64(^uint64(0)>>1)/unit {
		return fallback
	}
	return n * unit
}

// loadAndVerifyManifest reads manifest.json from the staging dir and verifies
// every listed file exists with a matching SHA-256 (AI.md PART 21 → "Restore
// Verification"). Nothing is installed until this passes.
func loadAndVerifyManifest(staging string) (*backupManifest, error) {
	raw, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cliText("cli.backup.invalid_manifest"), err)
	}
	var m backupManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", cliText("cli.backup.invalid_manifest"), err)
	}

	for filePath, want := range m.Checksums {
		clean, safe := safeManifestPath(filePath)
		if !safe {
			return nil, fmt.Errorf("%s", cliText("cli.backup.manifest_unsafe_path", "path", filePath))
		}
		data, err := os.ReadFile(filepath.Join(staging, clean))
		if err != nil {
			return nil, fmt.Errorf("%s", cliText("cli.backup.manifest_file_missing", "path", filePath))
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			return nil, fmt.Errorf("%s", cliText("cli.backup.checksum_mismatch", "path", filePath))
		}
	}
	listedContents := make(map[string]bool, len(m.Contents))
	for _, filePath := range m.Contents {
		clean, safe := safeManifestPath(filePath)
		if !safe || listedContents[clean] {
			return nil, fmt.Errorf("%s", cliText("cli.backup.manifest_unsafe_path", "path", filePath))
		}
		listedContents[clean] = true
		if _, ok := m.Checksums[clean]; !ok {
			return nil, fmt.Errorf("%s", cliText("cli.backup.manifest_checksum_missing", "path", filePath))
		}
		info, err := os.Lstat(filepath.Join(staging, filepath.FromSlash(clean)))
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s", cliText("cli.backup.manifest_file_missing", "path", filePath))
		}
	}
	if len(m.Files) > 0 {
		if m.SHA256 != backupFilesChecksum(m.Files) {
			return nil, errors.New(cliText("cli.backup.checksum_mismatch", "path", "manifest.json"))
		}
		for _, file := range m.Files {
			clean, safe := safeManifestPath(file.Path)
			if !safe {
				return nil, fmt.Errorf("%s", cliText("cli.backup.manifest_unsafe_path", "path", file.Path))
			}
			checksum, ok := m.Checksums[clean]
			if !ok || checksum != file.SHA256 {
				return nil, errors.New(cliText("cli.backup.invalid_manifest"))
			}
			if len(listedContents) > 0 && !listedContents[clean] {
				return nil, errors.New(cliText("cli.backup.invalid_manifest"))
			}
			info, err := os.Lstat(filepath.Join(staging, filepath.FromSlash(clean)))
			if err != nil || info.Size() != file.Size {
				return nil, errors.New(cliText("cli.backup.invalid_manifest"))
			}
		}
	}
	if m.Type == "" {
		m.Type = "full"
	}
	if m.SnapshotAt == "" {
		m.SnapshotAt = m.CreatedAt
	}

	return &m, nil
}

// backupFilesChecksum computes the canonical PART 21 manifest checksum from
// sorted "file SHA-256  path" lines, excluding manifest.json itself.
func backupFilesChecksum(files []backupManifestFile) string {
	ordered := append([]backupManifestFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	var lines strings.Builder
	for _, file := range ordered {
		fmt.Fprintf(&lines, "%s  %s\n", file.SHA256, file.Path)
	}
	sum := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(sum[:])
}

func safeManifestPath(filePath string) (string, bool) {
	if filePath == "" || strings.ContainsAny(filePath, "\\\x00:") || strings.HasPrefix(filePath, "/") {
		return "", false
	}
	clean := pathpkg.Clean(filePath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != filePath {
		return "", false
	}
	return clean, true
}

// verifyBackupFile confirms a backup on disk is non-empty, decryptable (if
// encrypted), extractable, and passes manifest checksum verification.
func verifyBackupFile(backupFile, password string) error {
	return verifyBackupFileWithLimits(backupFile, password, config.DefaultConfig().Server.Restore)
}

func verifyBackupFileWithLimits(backupFile, password string, limits config.RestoreConfig) error {
	fi, err := os.Stat(backupFile)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return errors.New(cliText("cli.backup.file_empty"))
	}
	archive, err := readBackupArchive(backupFile, password)
	if err != nil {
		return err
	}
	staging, err := extractToStagingWithLimits(archive, limits)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	manifest, err := loadAndVerifyManifest(staging)
	if err != nil {
		return err
	}
	if _, ok := manifest.Checksums["db/server.db"]; ok {
		if err := verifySQLiteIntegrity(filepath.Join(staging, "db", "server.db")); err != nil {
			return fmt.Errorf("%s", cliText("cli.main.backup_test_failed", "error", err.Error()))
		}
	}
	if manifest.Type == "incremental" {
		if strings.HasSuffix(backupFile, ".enc") {
			blob, readErr := os.ReadFile(backupFile)
			if readErr != nil {
				return readErr
			}
			header, headerErr := parseBackupHeader(blob)
			if headerErr == nil && header.baseBackup != manifest.BaseBackup {
				return errors.New("encrypted backup base reference does not match manifest")
			}
		}
		base := filepath.Join(filepath.Dir(backupFile), filepath.Base(manifest.BaseBackup))
		if filepath.Base(manifest.BaseBackup) != manifest.BaseBackup {
			return errors.New("ERR_BASE_MISSING")
		}
		if _, err := os.Stat(base); err != nil {
			return errors.New("ERR_BASE_MISSING")
		}
		checksum, err := checksumFile(base)
		if err != nil || checksum != manifest.BaseSHA256 {
			return errors.New("ERR_BASE_CORRUPT")
		}
	}
	return nil
}

func verifySQLiteIntegrity(path string) error {
	failure := errors.New(cliText("cli.main.backup_integrity_failed"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return failure
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return failure
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return failure
		}
		count++
		if result != "ok" {
			return failure
		}
	}
	if err := rows.Err(); err != nil {
		return failure
	}
	if count == 0 {
		return failure
	}
	return nil
}

func verifyBackupPGP(backupFile, password string) error {
	fi, err := os.Stat(backupFile)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return errors.New(cliText("cli.backup.file_empty"))
	}
	archive, err := readBackupArchive(backupFile, password)
	if err != nil {
		return err
	}
	staging, err := extractToStaging(archive)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	manifest, err := loadAndVerifyManifest(staging)
	if err != nil {
		return err
	}
	if manifest.Type == "incremental" {
		incrementalStage := staging
		staging, manifest, err = mergeIncrementalRestore(backupFile, password, incrementalStage, manifest, config.DefaultConfig().Server.Restore)
		if err != nil {
			return err
		}
		_ = os.RemoveAll(incrementalStage)
	}
	if !hasRestoredPGPKeypair(manifest.Contents) {
		return errors.New(cliText("cli.backup.pgp_key_files_missing"))
	}
	for _, required := range []string{"security/pgp.pub.asc", "security/pgp.priv.asc.enc", "db/server.db"} {
		if manifest.Checksums[required] == "" {
			return fmt.Errorf("%s", cliText("cli.backup.manifest_checksum_missing", "path", required))
		}
	}
	return verifyStagedPGP(staging)
}

func mergeIncrementalRestore(incrementalPath, password, incrementalStage string, incremental *backupManifest, limits config.RestoreConfig) (string, *backupManifest, error) {
	basePath := filepath.Join(filepath.Dir(incrementalPath), filepath.Base(incremental.BaseBackup))
	if filepath.Base(incremental.BaseBackup) != incremental.BaseBackup {
		return "", nil, errors.New("ERR_BASE_MISSING")
	}
	baseInfo, err := os.Stat(basePath)
	if err != nil {
		return "", nil, errors.New("ERR_BASE_MISSING")
	}
	incInfo, err := os.Stat(incrementalPath)
	if err != nil || baseInfo.Size()+incInfo.Size() > parseRestoreSize(limits.MaxArchiveSize, 1<<30) {
		return "", nil, errors.New("combined restore exceeds server.restore.max_archive_size")
	}
	sum, err := checksumFile(basePath)
	if err != nil || sum != incremental.BaseSHA256 {
		return "", nil, errors.New("ERR_BASE_CORRUPT")
	}
	baseArchive, err := readBackupArchive(basePath, password)
	if err != nil {
		return "", nil, errors.New("ERR_BASE_CORRUPT")
	}
	baseStage, err := extractToStagingWithLimits(baseArchive, limits)
	if err != nil {
		return "", nil, errors.New("ERR_BASE_CORRUPT")
	}
	baseManifest, err := loadAndVerifyManifest(baseStage)
	if err != nil || baseManifest.Type != "full" {
		_ = os.RemoveAll(baseStage)
		return "", nil, errors.New("ERR_BASE_CORRUPT")
	}
	files := make(map[string]backupManifestFile, len(baseManifest.Files))
	for _, file := range baseManifest.Files {
		files[file.Path] = file
	}
	for _, file := range incremental.Files {
		rel := filepath.Clean(file.Path)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			_ = os.RemoveAll(baseStage)
			return "", nil, errors.New("invalid incremental path")
		}
		src := filepath.Join(incrementalStage, rel)
		dst := filepath.Join(baseStage, rel)
		data, err := os.ReadFile(src)
		if err != nil {
			_ = os.RemoveAll(baseStage)
			return "", nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			_ = os.RemoveAll(baseStage)
			return "", nil, err
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			_ = os.RemoveAll(baseStage)
			return "", nil, err
		}
		files[file.Path] = file
	}
	for _, name := range incremental.Deleted {
		rel := filepath.Clean(name)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			_ = os.RemoveAll(baseStage)
			return "", nil, errors.New("invalid deleted path")
		}
		_ = os.Remove(filepath.Join(baseStage, rel))
		delete(files, name)
	}
	maxExpanded := parseRestoreSize(limits.MaxExpandedSize, 4<<30)
	maxFile := parseRestoreSize(limits.MaxFileSize, 512<<20)
	maxArchive := parseRestoreSize(limits.MaxArchiveSize, 1<<30)
	maxFiles, maxRatio := limits.MaxFiles, limits.MaxRatio
	if maxFiles < 1 {
		maxFiles = 10000
	}
	if maxRatio < 1 {
		maxRatio = 100
	}
	baseInfo, baseStatErr := os.Stat(basePath)
	incInfo, incStatErr := os.Stat(incrementalPath)
	if baseStatErr != nil || incStatErr != nil || baseInfo.Size()+incInfo.Size() > maxArchive {
		_ = os.RemoveAll(baseStage)
		return "", nil, errors.New("combined restore exceeds server.restore.max_archive_size")
	}
	var expanded int64
	if len(files) > maxFiles {
		_ = os.RemoveAll(baseStage)
		return "", nil, errors.New("combined restore exceeds server.restore.max_files")
	}
	for _, file := range files {
		if file.Size < 0 || file.Size > maxFile || expanded > maxExpanded-file.Size {
			_ = os.RemoveAll(baseStage)
			return "", nil, errors.New("combined restore exceeds configured expansion limits")
		}
		expanded += file.Size
	}
	if expanded > 0 && float64(expanded)/float64(baseInfo.Size()+incInfo.Size()) > float64(maxRatio) {
		_ = os.RemoveAll(baseStage)
		return "", nil, errors.New("combined restore exceeds server.restore.max_ratio")
	}
	baseManifest.Files = baseManifest.Files[:0]
	baseManifest.Contents = baseManifest.Contents[:0]
	baseManifest.Checksums = make(map[string]string, len(files))
	for _, file := range files {
		baseManifest.Files = append(baseManifest.Files, file)
		baseManifest.Contents = append(baseManifest.Contents, file.Path)
		baseManifest.Checksums[file.Path] = file.SHA256
	}
	sort.Slice(baseManifest.Files, func(i, j int) bool { return baseManifest.Files[i].Path < baseManifest.Files[j].Path })
	sort.Strings(baseManifest.Contents)
	return baseStage, baseManifest, nil
}

func verifyStagedPGP(staging string) error {
	privatePath := filepath.Join(staging, "security", "pgp.priv.asc.enc")
	publicPath := filepath.Join(staging, "security", "pgp.pub.asc")
	databasePath := filepath.Join(staging, "db", "server.db")
	privateEnvelope, err := os.ReadFile(privatePath)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.pgp_private_key_missing"), err)
	}
	publicKey, err := os.ReadFile(publicPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.pgp_public_key_missing"), err)
	}
	conn, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.database_open_failed"), err)
	}
	defer func() { _ = conn.Close() }()
	var encodedSecret string
	err = conn.QueryRow("SELECT value FROM app_secrets WHERE name = ?", "installation_secret").Scan(&encodedSecret)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such column: name") {
		err = conn.QueryRow("SELECT value FROM app_secrets WHERE key = ?", "installation_secret").Scan(&encodedSecret)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.installation_secret_unavailable"), err)
	}
	secret, err := base64.StdEncoding.DecodeString(encodedSecret)
	if err != nil || len(secret) != 32 {
		return errors.New(cliText("cli.backup.installation_secret_invalid"))
	}
	privateKey, err := pgp.UnprotectPrivateKey(privateEnvelope, secret)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.pgp_private_key_decrypt_failed"), err)
	}
	derivedPublic, err := pgp.PublicFromPrivate(privateKey)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.pgp_private_key_invalid"), err)
	}
	privateFingerprint, err := pgp.Fingerprint(derivedPublic)
	if err != nil {
		return err
	}
	publicFingerprint, err := pgp.Fingerprint(string(publicKey))
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.backup.pgp_public_key_invalid"), err)
	}
	if privateFingerprint != publicFingerprint {
		return errors.New(cliText("cli.backup.pgp_key_mismatch"))
	}
	return nil
}

// runRestore implements `--maintenance restore <backup-file>`. It verifies the
// backup fully in a staging directory before installing any file (AI.md PART
// 21 → "Restore Verification" / "Restore Behavior").
func runRestore(backupFile, configDir, dataDir string) (result error) {
	defer func() {
		if result != nil {
			appendBackupAudit("backup.failed", "error", "failure", map[string]any{"filename": filepath.Base(backupFile), "error": result.Error(), "operation": "restore"})
		}
	}()
	if _, err := os.Stat(backupFile); err != nil {
		return fmt.Errorf("%s", cliText("cli.backup.file_not_found", "file", backupFile))
	}

	password := ""
	if strings.HasSuffix(backupFile, ".enc") {
		pw, err := promptPassword(cliText("cli.backup.password_prompt"))
		if err != nil {
			return err
		}
		password = pw
	}

	fmt.Println(cliText("cli.backup.verifying"))
	restoreCfg, err := config.LoadFile(filepath.Join(configDir, "server.yml"))
	if err != nil {
		return fmt.Errorf("read restore limits: %w", err)
	}
	backupInfo, err := os.Stat(backupFile)
	if err != nil {
		return err
	}
	if backupInfo.Size() > parseRestoreSize(restoreCfg.Server.Restore.MaxArchiveSize, 1<<30) {
		return errors.New("restore archive exceeds server.restore.max_archive_size")
	}
	archive, err := readBackupArchive(backupFile, password)
	if err != nil {
		return err
	}
	staging, err := extractToStagingWithLimits(archive, restoreCfg.Server.Restore)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	manifest, err := loadAndVerifyManifest(staging)
	if err != nil {
		return err
	}
	if manifest.Type == "incremental" {
		incrementalStage := staging
		staging, manifest, err = mergeIncrementalRestore(backupFile, password, incrementalStage, manifest, restoreCfg.Server.Restore)
		if err != nil {
			return err
		}
		_ = os.RemoveAll(incrementalStage)
	}
	if manifest.AppVersion != "" && manifest.AppVersion != Version {
		fmt.Println(cliText("cli.backup.version_mismatch_warning", "backup_version", manifest.AppVersion, "running_version", Version))
	}
	fmt.Println(cliText("cli.backup.verification_ok"))

	// Install each verified file to its live location. server.yml goes to the
	// config dir; everything else is relative to the config dir except the
	// database, which lives under the data dir.
	fmt.Println(cliText("cli.backup.restoring"))
	externalSSLRestored := false
	for _, rel := range manifest.Contents {
		if rel == "app_secrets.json" {
			continue
		}
		src := filepath.Join(staging, filepath.Clean(rel))
		var dest string
		if rel == "db/server.db" {
			dest = filepath.Join(dataDir, "db", "server.db")
		} else if strings.HasPrefix(rel, "ssl-external/") {
			field := strings.TrimPrefix(rel, "ssl-external/")
			if field != "cert" && field != "key" {
				return fmt.Errorf("invalid external SSL backup path %s", rel)
			}
			dest = filepath.Join(configDir, "ssl", "restored-"+field)
			externalSSLRestored = true
		} else {
			dest = filepath.Join(configDir, filepath.Clean(rel))
		}
		if err := installFile(src, dest); err != nil {
			return fmt.Errorf("%s: %w", cliText("cli.backup.install_failed", "path", rel), err)
		}
		if strings.HasPrefix(rel, "ssl-external/") && strings.HasSuffix(rel, "/key") {
			if err := os.Chmod(dest, 0o600); err != nil {
				return err
			}
		}
	}
	if externalSSLRestored {
		if err := rewriteRestoredSSLPaths(filepath.Join(configDir, "server.yml"), configDir, manifest.Contents); err != nil {
			return err
		}
	}
	if containsBackupContent(manifest.Contents, "app_secrets.json") {
		restoredConfig, configErr := config.LoadFile(filepath.Join(configDir, "server.yml"))
		if configErr != nil {
			return fmt.Errorf("load restored database configuration: %w", configErr)
		}
		if err := restoreAppSecrets(filepath.Join(staging, "app_secrets.json"), dataDir, restoredConfig); err != nil {
			return fmt.Errorf("restore app secrets: %w", err)
		}
	}
	if hasRestoredPGPKeypair(manifest.Contents) {
		if err := republishRestoredPGP(configDir, dataDir); err != nil {
			return fmt.Errorf("%s: %w", cliText("cli.backup.pgp_publication_pending"), err)
		}
	}
	appendBackupAudit("backup.restored", "info", "success", map[string]any{"filename": filepath.Base(backupFile)})

	fmt.Println(cliText("cli.backup.restore_completed"))
	return nil
}

func rewriteRestoredSSLPaths(configPath, configDir string, contents []string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if len(root.Content) == 0 {
		return errors.New("server.yml is empty")
	}
	server := yamlMappingValue(root.Content[0], "server")
	ssl := yamlMappingValue(server, "ssl")
	for _, field := range []string{"cert", "key"} {
		if !containsBackupContent(contents, "ssl-external/"+field) {
			continue
		}
		node := yamlMappingValue(ssl, field)
		if node == nil {
			return fmt.Errorf("server.ssl.%s is missing from restored configuration", field)
		}
		node.Value = filepath.Join(configDir, "ssl", "restored-"+field)
	}
	updated, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, updated, 0o600)
}

func containsBackupContent(contents []string, want string) bool {
	for _, content := range contents {
		if content == want {
			return true
		}
	}
	return false
}

func restoreAppSecrets(path, dataDir string, cfg *config.ServerConfig) error {
	var secrets []struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
		UpdatedAt string `json:"updated_at"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &secrets); err != nil {
		return err
	}
	if err := db.Init(dataDir, databaseConnectionConfig(cfg)); err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return db.Write(context.Background(), func(tx *sql.Tx) error {
		for _, secret := range secrets {
			if secret.Name == "" {
				return errors.New("app secret has no name")
			}
			if secret.UpdatedAt == "" {
				secret.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			}
			if _, err := tx.ExecContext(context.Background(), `INSERT INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, secret.Name, secret.Value, secret.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func restoreLimits(cfg *config.ServerConfig) config.RestoreConfig {
	if cfg == nil {
		return config.DefaultConfig().Server.Restore
	}
	return cfg.Server.Restore
}

func hasRestoredPGPKeypair(contents []string) bool {
	publicKey, privateKey := false, false
	for _, item := range contents {
		clean := filepath.ToSlash(filepath.Clean(item))
		publicKey = publicKey || clean == "security/pgp.pub.asc"
		privateKey = privateKey || clean == "security/pgp.priv.asc.enc"
	}
	return publicKey && privateKey
}

func republishRestoredPGP(configDir, dataDir string) error {
	cfg, err := config.LoadFile(filepath.Join(configDir, "server.yml"))
	if err != nil {
		return err
	}
	if err := db.Init(dataDir); err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return publishRestoredProjectPGP(cfg, filepath.Join(configDir, "security"))
}

// installFile copies src to dest atomically (write to a temp file in the
// destination directory, then rename), creating parent directories.
func installFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dest)
}
