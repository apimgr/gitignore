package pgp

import (
	"bytes"
	"testing"
)

func TestGenerateProtectEncryptAndDecrypt(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, 32)
	keyPair, err := Generate("GitIgnore Security <security@example.test>", secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKey(keyPair.PublicKey); err != nil {
		t.Fatalf("generated public key did not parse: %v", err)
	}
	plaintext := []byte("coordinated disclosure report")
	ciphertext, err := Encrypt(plaintext, keyPair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := Decrypt(ciphertext, keyPair.EncryptedPrivateKey, secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted report = %q", decrypted)
	}
	if _, err := UnprotectPrivateKey(keyPair.EncryptedPrivateKey, bytes.Repeat([]byte{0x24}, 32)); err == nil {
		t.Fatal("wrong installation secret unexpectedly decrypted private key")
	}
}

func TestRejectInvalidIdentityAndPublicKey(t *testing.T) {
	if _, err := Generate("bad <identity", make([]byte, 32)); err == nil {
		t.Fatal("invalid identity unexpectedly generated a key")
	}
	if _, err := ParsePublicKey("not a key"); err == nil {
		t.Fatal("invalid public key unexpectedly parsed")
	}
}
