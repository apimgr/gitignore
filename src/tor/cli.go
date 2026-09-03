package tor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cretz/bine/control"
	"github.com/cretz/bine/torutil"
	"github.com/cretz/bine/torutil/ed25519"

	"github.com/apimgr/gitignore/src/config"
)

// SecretKeyPath returns the path to the persisted ed25519 secret key blob.
func SecretKeyPath(dataDir string) string {
	return filepath.Join(dataDir, "tor", "site", "hs_ed25519_secret_key")
}

// HostnamePath returns the path to the persisted .onion hostname file.
func HostnamePath(dataDir string) string {
	return filepath.Join(dataDir, "tor", "site", "hostname")
}

// vanityStagePath returns the path used to stage a searched vanity key until it
// is applied.
func vanityStagePath(dataDir string) string {
	return filepath.Join(dataDir, "tor", "site", "vanity_staged_key")
}

// ReadHostname returns the persisted .onion address, or "" when none exists.
func ReadHostname(dataDir string) string {
	data, err := os.ReadFile(HostnamePath(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ServiceIDFromBlob computes the .onion service ID (without suffix) for a
// persisted ed25519 secret key blob.
func ServiceIDFromBlob(blob string) (string, error) {
	key, err := control.ED25519KeyFromBlob(blob)
	if err != nil {
		return "", err
	}
	return torutil.OnionServiceIDFromV3PublicKey(key.PublicKey()), nil
}

// GenerateVanityKey searches for an ed25519 key whose .onion service ID starts
// with prefix (case-insensitive). It returns the key blob and the resulting
// service ID. The search stops when ctx is cancelled. Onion service IDs use
// base32 (a-z, 2-7), so prefixes containing other characters never match.
func GenerateVanityKey(ctx context.Context, prefix string) (blob, serviceID string, err error) {
	prefix = strings.ToLower(prefix)
	for _, r := range prefix {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz234567", r) {
			return "", "", fmt.Errorf("invalid vanity prefix %q: onion addresses use base32 (a-z, 2-7)", prefix)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		default:
		}

		kp, genErr := ed25519.GenerateKey(nil)
		if genErr != nil {
			return "", "", genErr
		}
		id := torutil.OnionServiceIDFromV3PublicKey(kp.PublicKey())
		if strings.HasPrefix(id, prefix) {
			ek := &control.ED25519Key{KeyPair: kp}
			return ek.Blob(), id, nil
		}
	}
}

// StageVanityKey persists a searched vanity key blob so it can later be applied.
func StageVanityKey(dataDir, blob string) error {
	return ensureTorFile(vanityStagePath(dataDir), []byte(blob))
}

// StagedVanityServiceID returns the service ID of a staged vanity key, or ""
// when none is staged.
func StagedVanityServiceID(dataDir string) (string, error) {
	data, err := os.ReadFile(vanityStagePath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return ServiceIDFromBlob(strings.TrimSpace(string(data)))
}

// ApplyStagedVanityKey promotes a staged vanity key to the live secret key and
// clears the stale hostname so it is regenerated on next start.
func ApplyStagedVanityKey(dataDir string) (string, error) {
	data, err := os.ReadFile(vanityStagePath(dataDir))
	if err != nil {
		return "", err
	}
	blob := strings.TrimSpace(string(data))
	id, err := ServiceIDFromBlob(blob)
	if err != nil {
		return "", err
	}
	if err := ensureTorFile(SecretKeyPath(dataDir), []byte(blob)); err != nil {
		return "", err
	}
	_ = os.Remove(HostnamePath(dataDir))
	_ = os.Remove(vanityStagePath(dataDir))
	return id, nil
}

// ImportKey validates a key blob file and installs it as the live secret key.
// It accepts either a base64 blob file (as produced by this binary) or a raw
// key; the blob is validated by computing its service ID.
func ImportKey(dataDir, srcPath string) (string, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	blob := strings.TrimSpace(string(data))
	id, err := ServiceIDFromBlob(blob)
	if err != nil {
		return "", fmt.Errorf("invalid key file: %w", err)
	}
	if err := ensureTorFile(SecretKeyPath(dataDir), []byte(blob)); err != nil {
		return "", err
	}
	_ = os.Remove(HostnamePath(dataDir))
	return id, nil
}

// RegenerateKeys deletes the persisted key material so a fresh address is
// generated on next start.
func RegenerateKeys(dataDir string) error {
	if err := os.Remove(SecretKeyPath(dataDir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(HostnamePath(dataDir))
	return nil
}

// ValidateConfig checks that a usable tor binary is available and that torrc
// generation succeeds. It returns the resolved binary path.
func ValidateConfig(cfg *config.TorConfig) (string, error) {
	bin, found := FindBinary(cfg.Binary)
	if !found {
		return "", fmt.Errorf("tor binary not found")
	}
	if getTorConfig(cfg) == "" {
		return "", fmt.Errorf("failed to generate torrc")
	}
	return bin, nil
}
