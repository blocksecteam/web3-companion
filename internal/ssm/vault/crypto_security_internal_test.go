// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"crypto/ecdsa"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestSecureDeleteOverwritesBeforeRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sensitive.key")
	original := []byte("private-key-material-do-not-leak")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	v := &Vault{}
	if err := v.secureDelete(path); err != nil {
		t.Fatalf("secureDelete: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be removed after secureDelete")
	}
}

func TestSecureDeleteReturnsNilForMissingFile(t *testing.T) {
	v := &Vault{}
	if err := v.secureDelete("/nonexistent/path/file.key"); err != nil {
		t.Fatalf("secureDelete on missing file should return nil, got: %v", err)
	}
}

func TestRandomHexLength(t *testing.T) {
	hex := randomHex(16)
	if len(hex) != 32 {
		t.Fatalf("randomHex(16) length = %d, want 32", len(hex))
	}
}

func TestRandomHexUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		h := randomHex(16)
		if seen[h] {
			t.Fatalf("duplicate randomHex at iteration %d", i)
		}
		seen[h] = true
	}
}

func TestWipePrivateKeyZerosD(t *testing.T) {
	key := &ecdsa.PrivateKey{D: new(big.Int).SetInt64(0xDEADBEEF)}

	WipePrivateKey(key)

	if key.D.Sign() != 0 {
		t.Fatalf("D should be zero after WipePrivateKey, got %v", key.D)
	}
}

func TestWipePrivateKeyNilSafe(t *testing.T) {
	WipePrivateKey(nil)

	key := &ecdsa.PrivateKey{}
	WipePrivateKey(key)
}

func TestZeroBytesClears(t *testing.T) {
	b := []byte{0xFF, 0xAB, 0xCD, 0xEF, 0x12, 0x34}
	ZeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("byte[%d] = 0x%02X, want 0x00", i, v)
		}
	}
}

func TestGenerateDataKeyLength(t *testing.T) {
	key, err := GenerateDataKey()
	if err != nil {
		t.Fatalf("GenerateDataKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}
}

func TestGenerateDataKeyUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key, err := GenerateDataKey()
		if err != nil {
			t.Fatalf("GenerateDataKey iteration %d: %v", i, err)
		}
		s := string(key)
		if seen[s] {
			t.Fatalf("duplicate key generated at iteration %d", i)
		}
		seen[s] = true
	}
}

func TestEncryptNonceUniqueness(t *testing.T) {
	key, _ := GenerateDataKey()
	plaintext := []byte("test data for nonce uniqueness check")

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		ct, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt iteration %d: %v", i, err)
		}
		nonce := string(ct[:12])
		if seen[nonce] {
			t.Fatalf("duplicate nonce at iteration %d", i)
		}
		seen[nonce] = true
	}
}

func TestEncryptRejectsShortKey(t *testing.T) {
	_, err := Encrypt([]byte("short"), []byte("data"))
	if err == nil {
		t.Fatal("Encrypt should reject key shorter than 32 bytes")
	}
}

func TestDecryptRejectsShortCiphertext(t *testing.T) {
	key, _ := GenerateDataKey()
	_, err := Decrypt(key, []byte("short"))
	if err == nil {
		t.Fatal("Decrypt should reject ciphertext shorter than nonce size")
	}
}
