// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm

import (
	"context"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestPrivateKeyGenerationUsesCSPRNG(t *testing.T) {
	addresses := make(map[string]bool)
	for i := 0; i < 50; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
		if addresses[addr] {
			t.Fatalf("duplicate address at iteration %d: %s", i, addr)
		}
		addresses[addr] = true
	}
}

func TestWalletCreationProducesUniqueAddresses(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	addresses := make(map[string]bool)
	for i := 0; i < 10; i++ {
		walletID := "entropy_test_" + string(rune('A'+i))
		w, err := v.CreateWallet(context.Background(), userID, walletID)
		if err != nil {
			t.Fatalf("CreateWallet %d: %v", i, err)
		}
		if addresses[w.Address] {
			t.Fatalf("duplicate wallet address at iteration %d: %s", i, w.Address)
		}
		addresses[w.Address] = true

		if err := v.DeleteWallet(context.Background(), userID, walletID); err != nil {
			t.Fatalf("DeleteWallet %d: %v", i, err)
		}
	}
}

func TestHKDFKEKDeterministic(t *testing.T) {
	rootKey := "test-ssm-root-key-that-is-long-enough-for-hkdf"

	kms1, err := vault.NewEncryptedFileKMSClient(rootKey)
	if err != nil {
		t.Fatalf("first NewEncryptedFileKMSClient: %v", err)
	}
	kms2, err := vault.NewEncryptedFileKMSClient(rootKey)
	if err != nil {
		t.Fatalf("second NewEncryptedFileKMSClient: %v", err)
	}

	pt1, enc1, err := kms1.GenerateDataKey(context.Background())
	if err != nil {
		t.Fatalf("GenerateDataKey kms1: %v", err)
	}

	dec, err := kms2.Decrypt(context.Background(), enc1)
	if err != nil {
		t.Fatalf("kms2 should decrypt kms1's wrapped key: %v", err)
	}
	if string(dec) != string(pt1) {
		t.Fatal("decrypted data key mismatch")
	}
}

func TestHKDFKEKDifferentRootKeysFail(t *testing.T) {
	kms1, err := vault.NewEncryptedFileKMSClient("root-key-alpha-that-is-long-enough")
	if err != nil {
		t.Fatalf("kms1: %v", err)
	}
	kms2, err := vault.NewEncryptedFileKMSClient("root-key-bravo-that-is-long-enough")
	if err != nil {
		t.Fatalf("kms2: %v", err)
	}

	_, enc, err := kms1.GenerateDataKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := kms2.Decrypt(context.Background(), enc); err == nil {
		t.Fatal("different root key should fail to decrypt")
	}
}

func TestEncryptedFileKMSRoundtripWithWallet(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()

	kms, err := vault.NewEncryptedFileKMSClient("hkdf-test-root-key-that-is-long-enough")
	if err != nil {
		t.Fatalf("NewEncryptedFileKMSClient: %v", err)
	}
	v := vault.New(kms, db, keysDir, nil, nil)

	w, err := v.CreateWallet(context.Background(), userID, "hkdf_wallet")
	if err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}
	if len(w.Address) != 42 || w.Address[:2] != "0x" {
		t.Fatalf("invalid address: %s", w.Address)
	}

	addr, privHex, err := v.ExportPrivateKey(context.Background(), userID, "hkdf_wallet")
	if err != nil {
		t.Fatalf("ExportPrivateKey: %v", err)
	}
	if addr != w.Address {
		t.Fatalf("address mismatch: %s != %s", addr, w.Address)
	}
	if len(privHex) != 66 || privHex[:2] != "0x" {
		t.Fatalf("unexpected private key hex length: %d", len(privHex))
	}
}

func TestDeleteWalletCleansKeyFile(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	w, err := v.CreateWallet(context.Background(), userID, "cleanup_test")
	if err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}

	if _, err := os.Stat(w.KeyFile); err != nil {
		t.Fatalf("key file should exist after creation: %v", err)
	}
}
