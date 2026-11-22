package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apimgr/gitignore/src/pgp"
)

func encryptSecurityReport(encodedKey string, plaintext []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("security report encryption key is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plaintext, nil)...), nil
}

// DecryptSecurityReport decrypts an AES-256-GCM report body or contact blob
// with the configured at-rest key for trusted maintenance workflows.
func DecryptSecurityReport(encodedKey string, ciphertext []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("security report encryption key is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, fmt.Errorf("security report ciphertext is invalid")
	}
	nonce, ciphertext := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}

func (s *Server) encryptSecurityReportBody(encodedKey string, plaintext []byte) ([]byte, string, string, error) {
	if s != nil && s.config != nil && s.config.Paths != nil {
		path := filepath.Join(s.config.Paths.GetConfigDir(), "security", "pgp.pub.asc")
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 && len(data) <= 128*1024 {
			publicKey := string(data)
			if _, err := pgp.ParsePublicKey(publicKey); err != nil {
				return nil, "", "", fmt.Errorf("project PGP public key is invalid: %w", err)
			}
			ciphertext, err := pgp.Encrypt(plaintext, publicKey)
			if err != nil {
				return nil, "", "", err
			}
			return ciphertext, "pgp", publicKey, nil
		}
		if !os.IsNotExist(err) && err != nil {
			return nil, "", "", err
		}
	}
	ciphertext, err := encryptSecurityReport(encodedKey, plaintext)
	return ciphertext, "aes-256-gcm", "", err
}
