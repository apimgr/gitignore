package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"

	"github.com/apimgr/gitignore/src/config"
)

// Backup encryption parameters (AI.md PART 21). Argon2id derives the AES-256
// key from the operator password; the key is never persisted and the password
// is never stored. saltLen/nonceLen are prepended to the ciphertext so restore
// can reconstruct the key without any side-channel state.
const (
	backupManifestVersion = "1.0.0"
	backupArgonTime       = 3
	backupArgonMemory     = 64 * 1024
	backupArgonThreads    = 4
	backupKeyLen          = 32
	backupSaltLen         = 16
	backupNonceLen        = 12
)

// backupManifest is the manifest.json embedded at the root of every archive.
// Checksums maps each archived file path to its SHA-256, letting restore
// verify integrity before any file is installed (AI.md PART 21 → "Manifest").
type backupManifest struct {
	Version          string            `json:"version"`
	CreatedAt        string            `json:"created_at"`
	CreatedBy        string            `json:"created_by"`
	AppVersion       string            `json:"app_version"`
	Contents         []string          `json:"contents"`
	Checksums        map[string]string `json:"checksums"`
	Encrypted        bool              `json:"encrypted"`
	EncryptionMethod string            `json:"encryption_method,omitempty"`
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
	add("db/gitignore.db", filepath.Join(dataDir, "db", "gitignore.db"))

	for _, sub := range []string{"template", "theme"} {
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

// buildBackupArchive builds a gzip-compressed tar in memory containing the
// manifest and all source files, returning the raw archive bytes.
func buildBackupArchive(sources []backupSource, encrypted bool) ([]byte, error) {
	manifest := backupManifest{
		Version:    backupManifestVersion,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		CreatedBy:  "operator",
		AppVersion: Version,
		Checksums:  make(map[string]string),
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
			return nil, fmt.Errorf("reading %s: %w", src.diskPath, err)
		}
		sum := sha256.Sum256(data)
		manifest.Checksums[src.archivePath] = hex.EncodeToString(sum[:])
		manifest.Contents = append(manifest.Contents, src.archivePath)
		files = append(files, payload{name: src.archivePath, data: data})
	}

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
// archive with AES-256-GCM. Output layout: salt || nonce || ciphertext. The
// plaintext archive never touches disk (AI.md PART 21 → "How Encryption Works").
func encryptBackup(plaintext []byte, password string) ([]byte, error) {
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
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, len(salt)+len(nonce)+len(ciphertext))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// decryptBackup reverses encryptBackup: it splits salt/nonce/ciphertext,
// re-derives the key, and authenticates + decrypts with AES-256-GCM. A wrong
// password fails the GCM tag check and returns an error (AI.md PART 21
// "Decrypt test").
func decryptBackup(blob []byte, password string) ([]byte, error) {
	if len(blob) < backupSaltLen+backupNonceLen {
		return nil, errors.New("encrypted backup is truncated")
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
		return nil, errors.New("decryption failed (wrong password or corrupted backup)")
	}
	return plaintext, nil
}

// promptPassword reads a password from the terminal without echoing it. It
// never accepts a password on the command line (AI.md PART 21 note: CLI
// passwords leak via shell history and process lists).
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("a password is required but stdin is not a terminal")
	}
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}

// runBackup implements `--maintenance backup [filename]`.
func runBackup(cfg *config.Config, configDir, dataDir, backupFile string) error {
	encrypt := cfg != nil && cfg.Server.Backup.Encryption.Enabled

	password := ""
	if encrypt {
		pw, err := promptPassword("Enter backup password: ")
		if err != nil {
			return err
		}
		confirm, err := promptPassword("Confirm backup password: ")
		if err != nil {
			return err
		}
		if pw == "" {
			return errors.New("backup password cannot be empty")
		}
		if pw != confirm {
			return errors.New("passwords do not match")
		}
		password = pw
	}

	// Ensure the filename extension matches the encryption decision.
	backupFile = normalizeBackupExt(backupFile, encrypt)

	sources, err := collectBackupSources(configDir, dataDir)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("nothing to back up (no config or database found)")
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
	if err := os.WriteFile(backupFile, out, 0o600); err != nil {
		return err
	}

	// Verify the freshly written backup by reading it back and re-checking
	// every manifest checksum (AI.md PART 21 → "Verification").
	if err := verifyBackupFile(backupFile, password); err != nil {
		os.Remove(backupFile)
		return fmt.Errorf("backup verification failed: %w", err)
	}

	fmt.Printf("Backup created: %s\n", backupFile)
	return nil
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
	staging, err := os.MkdirTemp("", "gitignore-restore-")
	if err != nil {
		return "", err
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("invalid gzip stream: %w", err)
	}
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(staging)
			return "", err
		}

		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			os.RemoveAll(staging)
			return "", fmt.Errorf("refusing path traversal entry: %s", hdr.Name)
		}
		dest := filepath.Join(staging, clean)
		if !strings.HasPrefix(dest, filepath.Clean(staging)+string(os.PathSeparator)) {
			os.RemoveAll(staging)
			return "", fmt.Errorf("refusing entry outside staging: %s", hdr.Name)
		}

		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				os.RemoveAll(staging)
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			os.RemoveAll(staging)
			return "", err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			os.RemoveAll(staging)
			return "", err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			os.RemoveAll(staging)
			return "", err
		}
		f.Close()
	}

	return staging, nil
}

// loadAndVerifyManifest reads manifest.json from the staging dir and verifies
// every listed file exists with a matching SHA-256 (AI.md PART 21 → "Restore
// Verification"). Nothing is installed until this passes.
func loadAndVerifyManifest(staging string) (*backupManifest, error) {
	raw, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	var m backupManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	for path, want := range m.Checksums {
		clean := filepath.Clean(path)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return nil, fmt.Errorf("manifest lists unsafe path: %s", path)
		}
		data, err := os.ReadFile(filepath.Join(staging, clean))
		if err != nil {
			return nil, fmt.Errorf("manifest file missing: %s", path)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			return nil, fmt.Errorf("checksum mismatch for %s", path)
		}
	}

	return &m, nil
}

// verifyBackupFile confirms a backup on disk is non-empty, decryptable (if
// encrypted), extractable, and passes manifest checksum verification.
func verifyBackupFile(backupFile, password string) error {
	fi, err := os.Stat(backupFile)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return errors.New("backup file is empty")
	}
	archive, err := readBackupArchive(backupFile, password)
	if err != nil {
		return err
	}
	staging, err := extractToStaging(archive)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	_, err = loadAndVerifyManifest(staging)
	return err
}

// runRestore implements `--maintenance restore <backup-file>`. It verifies the
// backup fully in a staging directory before installing any file (AI.md PART
// 21 → "Restore Verification" / "Restore Behavior").
func runRestore(backupFile, configDir, dataDir string) error {
	if _, err := os.Stat(backupFile); err != nil {
		return fmt.Errorf("backup file not found: %s", backupFile)
	}

	password := ""
	if strings.HasSuffix(backupFile, ".enc") {
		pw, err := promptPassword("Enter backup password: ")
		if err != nil {
			return err
		}
		password = pw
	}

	fmt.Println("Verifying backup integrity...")
	archive, err := readBackupArchive(backupFile, password)
	if err != nil {
		return err
	}
	staging, err := extractToStaging(archive)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	manifest, err := loadAndVerifyManifest(staging)
	if err != nil {
		return err
	}
	if manifest.AppVersion != "" && manifest.AppVersion != Version {
		fmt.Printf("Warning: backup was created by app version %s (running %s)\n", manifest.AppVersion, Version)
	}
	fmt.Println("Verification OK")

	// Install each verified file to its live location. server.yml goes to the
	// config dir; everything else is relative to the config dir except the
	// database, which lives under the data dir.
	fmt.Println("Restoring...")
	for _, rel := range manifest.Contents {
		src := filepath.Join(staging, filepath.Clean(rel))
		var dest string
		if rel == "db/gitignore.db" {
			dest = filepath.Join(dataDir, "db", "gitignore.db")
		} else {
			dest = filepath.Join(configDir, filepath.Clean(rel))
		}
		if err := installFile(src, dest); err != nil {
			return fmt.Errorf("installing %s: %w", rel, err)
		}
	}

	fmt.Println("Restore completed")
	return nil
}

// installFile copies src to dest atomically (write to a temp file in the
// destination directory, then rename), creating parent directories.
func installFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dest)
}
