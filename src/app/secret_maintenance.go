package app

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/pgp"
	"github.com/apimgr/gitignore/src/service"
)

func handleSecretMaintenance(configPath, configDir, dataDir, logsDir string, args []string) error {
	if len(args) != 2 || args[0] != "rotate" || (args[1] != "installation_secret" && args[1] != "encryption_key") {
		return errors.New("usage: --maintenance secret rotate installation_secret|encryption_key")
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if !service.IsElevated() && !authenticateSecurityMaintenance(cfg.Server.Token) {
		return errors.New("operator authorization required")
	}
	if _, err := readPGPConfirmation("Type CONFIRM to rotate a project secret: ", "CONFIRM"); err != nil {
		return err
	}
	if err := db.Init(dataDir, databaseConnectionConfig(cfg)); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer func() { _ = db.Close() }()
	switch args[1] {
	case "installation_secret":
		return rotateInstallationSecret(configDir, logsDir)
	case "encryption_key":
		return rotateEncryptionKey(configPath, cfg.Server.Security.EncryptionKey, logsDir)
	}
	return nil
}

func rotateInstallationSecret(configDir, logsDir string) error {
	previousCandidates, err := db.InstallationSecretCandidates(time.Now().UTC())
	if err != nil {
		return err
	}
	newSecret := make([]byte, 32)
	if _, err := rand.Read(newSecret); err != nil {
		return err
	}
	privatePath := filepath.Join(configDir, "security", pgpPrivateFilename)
	privateKey, err := os.ReadFile(privatePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read encrypted PGP private key: %w", err)
	}
	var rewrapped []byte
	if err == nil {
		var plaintext []byte
		for _, candidate := range previousCandidates {
			plaintext, err = pgp.UnprotectPrivateKey(privateKey, candidate)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("decrypt PGP private key: %w", err)
		}
		rewrapped, err = pgp.ProtectPrivateKey(plaintext, newSecret)
		if err != nil {
			return fmt.Errorf("re-encrypt PGP private key: %w", err)
		}
	}
	if err := db.RotateInstallationSecret(newSecret, time.Now().UTC()); err != nil {
		return err
	}
	if rewrapped != nil {
		if err := atomicPGPWrite(privatePath, rewrapped, 0600); err != nil {
			return fmt.Errorf("write re-encrypted PGP private key: %w", err)
		}
	}
	if err := appendSecretRotationAudit(logsDir, "security.installation_secret_rotated", "installation_secret", "7d"); err != nil {
		return fmt.Errorf("write installation secret rotation audit event: %w", err)
	}
	fmt.Println("installation_secret rotated")
	return nil
}

// reconcilePGPPrivateKey completes a PGP re-encryption interrupted after the
// installation secret was committed to the database.
func reconcilePGPPrivateKey(configDir string) error {
	path := filepath.Join(configDir, "security", pgpPrivateFilename)
	encrypted, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	candidates, err := db.InstallationSecretCandidates(time.Now().UTC())
	if err != nil {
		return err
	}
	current := candidates[0]
	if _, err := pgp.UnprotectPrivateKey(encrypted, current); err == nil {
		return nil
	}
	for _, previous := range candidates[1:] {
		plaintext, err := pgp.UnprotectPrivateKey(encrypted, previous)
		if err != nil {
			continue
		}
		rewrapped, err := pgp.ProtectPrivateKey(plaintext, current)
		if err != nil {
			return err
		}
		return atomicPGPWrite(path, rewrapped, 0600)
	}
	return errors.New("encrypted PGP private key cannot be opened with current or grace-period installation secret")
}

func rotateEncryptionKey(configPath, currentEncoded, logsDir string) error {
	nextBytes := make([]byte, 32)
	if _, err := rand.Read(nextBytes); err != nil {
		return err
	}
	nextEncoded := base64.StdEncoding.EncodeToString(nextBytes)
	now := time.Now().UTC()
	if err := db.PrepareEncryptionKeyRotation(currentEncoded, nextEncoded, now); err != nil {
		return err
	}
	if _, err := config.RotateEncryptionKey(configPath, nextEncoded); err != nil {
		return fmt.Errorf("persist replacement encryption key; staged rotation will resume at startup: %w", err)
	}
	if err := db.CompleteEncryptionKeyRotation(nextEncoded); err != nil {
		return fmt.Errorf("complete encryption key data re-encryption: %w", err)
	}
	if err := appendSecretRotationAudit(logsDir, "security.encryption_key_rotated", "encryption_key", "30d"); err != nil {
		return fmt.Errorf("write encryption key rotation audit event: %w", err)
	}
	fmt.Println("encryption_key rotated")
	return nil
}

func appendSecretRotationAudit(logsDir, event, name, grace string) error {
	return audit.Append(logsDir, audit.Entry{
		Event: event, Category: "security", Severity: "info",
		Actor:   audit.Actor{Type: "operator", ID: "operator"},
		Target:  &audit.Target{Type: "secret", ID: name},
		Details: map[string]any{"source": "cli", "grace_period": grace}, Result: "success", Source: "cli",
	})
}
