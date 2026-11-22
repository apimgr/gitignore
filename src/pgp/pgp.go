// Package pgp provides the project OpenPGP key and message operations.
package pgp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"golang.org/x/crypto/scrypt"
)

const (
	privateEnvelopeVersion = "PGP-PRIVATE-ENC-V1"
	scryptN                = 1 << 15
	scryptR                = 8
	scryptP                = 1
)

// KeyPair contains ASCII-armored public material and an AES-GCM protected private key.
type KeyPair struct {
	PublicKey           string
	EncryptedPrivateKey []byte
}

// Generate creates an Ed25519 signing and Curve25519 encryption keypair.
// identity is conventionally "Name <email>".
func Generate(identity string, installationSecret []byte) (*KeyPair, error) {
	if len(installationSecret) != 32 {
		return nil, errors.New("installation secret must be 32 bytes")
	}
	name, email, err := splitIdentity(identity)
	if err != nil {
		return nil, err
	}
	cfg := &packet.Config{
		Algorithm:       packet.PubKeyAlgoEdDSA,
		Curve:           packet.Curve25519,
		KeyLifetimeSecs: uint32((2 * 365 * 24 * time.Hour).Seconds()),
	}
	e, err := openpgp.NewEntity(name, "", email, cfg)
	if err != nil {
		return nil, fmt.Errorf("generate OpenPGP keypair: %w", err)
	}
	var pub, priv bytes.Buffer
	pubArmor, err := armor.Encode(&pub, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, err
	}
	if err = e.Serialize(pubArmor); err != nil {
		_ = pubArmor.Close()
		return nil, fmt.Errorf("serialize public key: %w", err)
	}
	if err = pubArmor.Close(); err != nil {
		return nil, err
	}
	privArmor, err := armor.Encode(&priv, openpgp.PrivateKeyType, nil)
	if err != nil {
		return nil, err
	}
	if err = e.SerializePrivate(privArmor, cfg); err != nil {
		_ = privArmor.Close()
		return nil, fmt.Errorf("serialize private key: %w", err)
	}
	if err = privArmor.Close(); err != nil {
		return nil, err
	}
	protected, err := ProtectPrivateKey(priv.Bytes(), installationSecret)
	if err != nil {
		return nil, err
	}
	return &KeyPair{PublicKey: pub.String(), EncryptedPrivateKey: protected}, nil
}

func splitIdentity(identity string) (string, string, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" || strings.ContainsAny(identity, "\x00()") {
		return "", "", errors.New("invalid OpenPGP identity")
	}
	if i := strings.LastIndex(identity, "<"); i >= 0 && strings.HasSuffix(identity, ">") {
		name := strings.TrimSpace(identity[:i])
		email := strings.TrimSpace(identity[i+1 : len(identity)-1])
		if name == "" || email == "" || strings.ContainsAny(email, "<>") {
			return "", "", errors.New("invalid OpenPGP identity")
		}
		return name, email, nil
	}
	if strings.ContainsAny(identity, "<>") {
		return "", "", errors.New("invalid OpenPGP identity")
	}
	return identity, "", nil
}

// ProtectPrivateKey wraps armored private key bytes with AES-256-GCM.
func ProtectPrivateKey(plaintext, installationSecret []byte) ([]byte, error) {
	if len(installationSecret) != 32 {
		return nil, errors.New("installation secret must be 32 bytes")
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key(installationSecret, salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return nil, fmt.Errorf("derive private-key encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, []byte(privateEnvelopeVersion))
	data := append(salt, nonce...)
	data = append(data, sealed...)
	return []byte(privateEnvelopeVersion + ":" + base64.RawStdEncoding.EncodeToString(data)), nil
}

// UnprotectPrivateKey decrypts a value produced by ProtectPrivateKey.
func UnprotectPrivateKey(envelope, installationSecret []byte) ([]byte, error) {
	if len(installationSecret) != 32 {
		return nil, errors.New("installation secret must be 32 bytes")
	}
	parts := strings.SplitN(string(envelope), ":", 2)
	if len(parts) != 2 || parts[0] != privateEnvelopeVersion {
		return nil, errors.New("unsupported encrypted private-key format")
	}
	data, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid encrypted private-key encoding")
	}
	if len(data) < 16+12+16 {
		return nil, errors.New("encrypted private key is truncated")
	}
	salt, nonce, ciphertext := data[:16], data[16:28], data[28:]
	key, err := scrypt.Key(installationSecret, salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return nil, fmt.Errorf("derive private-key encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(privateEnvelopeVersion))
	if err != nil {
		return nil, errors.New("private-key decryption failed")
	}
	return plaintext, nil
}

// ParsePrivateKey validates one armored private key and returns its entity.
func ParsePrivateKey(armored []byte) (*openpgp.Entity, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	if len(entities) != 1 || entities[0].PrivateKey == nil {
		return nil, errors.New("private key must contain exactly one private identity")
	}
	return entities[0], nil
}

// PublicFromPrivate returns the public portion of one armored private key.
func PublicFromPrivate(armored []byte) (string, error) {
	entity, err := ParsePrivateKey(armored)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	writer, err := armor.Encode(&output, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := entity.Serialize(writer); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("serialize public key: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return output.String(), nil
}

// Fingerprint returns the uppercase OpenPGP v4 primary-key fingerprint.
func Fingerprint(armored string) (string, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}
	if len(entities) != 1 || entities[0].PrimaryKey == nil || entities[0].PrimaryKey.Version != 4 {
		return "", errors.New("public key must contain one OpenPGP v4 primary key")
	}
	return strings.ToUpper(fmt.Sprintf("%X", entities[0].PrimaryKey.Fingerprint)), nil
}

// SignRotation signs the new public key using the current project private key.
func SignRotation(encryptedPrivateKey, installationSecret []byte, newPublicKey string) ([]byte, error) {
	armored, err := UnprotectPrivateKey(encryptedPrivateKey, installationSecret)
	if err != nil {
		return nil, err
	}
	entity, err := ParsePrivateKey(armored)
	if err != nil {
		return nil, err
	}
	var signature bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&signature, []*openpgp.Entity{entity}, strings.NewReader(newPublicKey), nil); err != nil {
		return nil, fmt.Errorf("sign rotated public key: %w", err)
	}
	return signature.Bytes(), nil
}

// ParsePublicKey parses and validates one armored researcher encryption key.
func ParsePublicKey(armored string) (*openpgp.Entity, error) {
	if strings.TrimSpace(armored) == "" {
		return nil, errors.New("public key is empty")
	}
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	if len(entities) != 1 {
		return nil, errors.New("public key must contain exactly one identity")
	}
	e := entities[0]
	if e.PrimaryKey == nil {
		return nil, errors.New("public key has no primary key")
	}
	_, identity := e.PrimaryIdentity(time.Now(), nil)
	if identity == nil {
		return nil, errors.New("public key has no valid primary identity")
	}
	if _, err := e.EncryptionKeyWithError(time.Now(), nil); err != nil {
		return nil, errors.New("public key has no valid encryption key")
	}
	return e, nil
}

// FetchPublicKey retrieves and validates one public encryption key over HTTPS.
func FetchPublicKey(ctx context.Context, address string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("public key URL must use HTTPS")
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" || request.URL.Host == "" || len(via) >= 3 {
				return errors.New("unsafe public key URL redirect")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/pgp-keys, application/pgp, text/plain")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch public key: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public key URL returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if err != nil {
		return "", fmt.Errorf("read public key response: %w", err)
	}
	if len(data) == 0 || len(data) > 128*1024 {
		return "", errors.New("public key response must be between 1 byte and 128 KiB")
	}
	armored := strings.TrimSpace(string(data))
	if _, err := ParsePublicKey(armored); err != nil {
		return "", err
	}
	return armored, nil
}

// Encrypt encrypts plaintext to one or more ASCII-armored recipient keys.
func Encrypt(plaintext []byte, recipients ...string) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, errors.New("at least one recipient key is required")
	}
	entities := make([]*openpgp.Entity, 0, len(recipients))
	for _, key := range recipients {
		e, err := ParsePublicKey(key)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	var out bytes.Buffer
	w, err := armor.Encode(&out, openpgp.MessageType, nil)
	if err != nil {
		return nil, err
	}
	body, err := openpgp.Encrypt(w, entities, nil, nil, nil, nil)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("encrypt message: %w", err)
	}
	if _, err = io.Copy(body, bytes.NewReader(plaintext)); err != nil {
		_ = body.Close()
		_ = w.Close()
		return nil, err
	}
	if err = body.Close(); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Decrypt decrypts an armored OpenPGP message using an encrypted private key.
func Decrypt(message, encryptedPrivateKey, installationSecret []byte) ([]byte, error) {
	priv, err := UnprotectPrivateKey(encryptedPrivateKey, installationSecret)
	if err != nil {
		return nil, err
	}
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(priv))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	if len(entities) != 1 {
		return nil, errors.New("private key must contain exactly one identity")
	}
	block, err := armor.Decode(bytes.NewReader(message))
	if err != nil {
		return nil, fmt.Errorf("decode armored message: %w", err)
	}
	if block.Type != openpgp.MessageType {
		return nil, errors.New("encrypted message has an unexpected armor type")
	}
	md, err := openpgp.ReadMessage(block.Body, entities, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt message: %w", err)
	}
	plaintext, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		return nil, fmt.Errorf("read decrypted message: %w", err)
	}
	return plaintext, nil
}
