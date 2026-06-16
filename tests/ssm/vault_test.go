// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"log/slog"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestVaultCryptoRoundtrip(t *testing.T) {
	key, err := vault.GenerateDataKey()
	if err != nil {
		t.Fatalf("GenerateDataKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}

	plaintext := []byte("test private key material - 32 bytes!!")
	encrypted, err := vault.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	decrypted, err := vault.Decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("decrypted != plaintext")
	}
}

func TestVaultCryptoDecryptWrongKey(t *testing.T) {
	key1, _ := vault.GenerateDataKey()
	key2, _ := vault.GenerateDataKey()
	encrypted, _ := vault.Encrypt(key1, []byte("test data - 32 bytes long!!"))

	_, err := vault.Decrypt(key2, encrypted)
	if err == nil {
		t.Error("expected decrypt error with wrong key")
	}
}

func TestVaultCryptoDecryptCorrupted(t *testing.T) {
	key, _ := vault.GenerateDataKey()
	encrypted, _ := vault.Encrypt(key, []byte("test data - 32 bytes long!!"))

	encrypted[len(encrypted)/2] ^= 0xFF

	_, err := vault.Decrypt(key, encrypted)
	if err == nil {
		t.Error("expected decrypt error with corrupted data")
	}
}

func TestVaultZeroBytes(t *testing.T) {
	b := []byte("sensitive data that should be zeroed")
	vault.ZeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("byte at %d is %d, want 0", i, v)
		}
	}
}

func TestVaultCreateAndGetKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)

	keysDir := t.TempDir()
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	key, err := v.CreateWallet(context.Background(), userID, "test_wallet")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if key.WalletID != "test_wallet" {
		t.Errorf("key_id = %s, want test_wallet", key.WalletID)
	}
	if len(key.Address) != 42 || key.Address[:2] != "0x" {
		t.Errorf("invalid address: %s", key.Address)
	}

	got, err := v.GetWallet(userID, "test_wallet")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if got.Address != key.Address {
		t.Error("address mismatch")
	}

	keys, err := v.ListWallets(userID, 10, 0)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("list length = %d, want 1", len(keys))
	}
}

func TestVaultCreateWalletDoesNotLogPrivateKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, err := v.CreateWallet(context.Background(), userID, "logged_key"); err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}

	logText := logs.String()
	if strings.Contains(logText, `"private_key":"0x`) {
		t.Fatalf("wallet create log contains private key field: %s", logText)
	}
}

func TestEncryptedFileKMSWrapsDataKeys(t *testing.T) {
	kms, err := vault.NewEncryptedFileKMSClient("test-ssm-root-key-that-is-long-enough")
	if err != nil {
		t.Fatalf("NewEncryptedFileKMSClient: %v", err)
	}
	plain, wrapped, err := kms.GenerateDataKey(context.Background())
	if err != nil {
		t.Fatalf("GenerateDataKey: %v", err)
	}
	if string(plain) == string(wrapped) {
		t.Fatalf("encrypted data key must not equal plaintext")
	}
	unwrapped, err := kms.Decrypt(context.Background(), wrapped)
	if err != nil {
		t.Fatalf("Decrypt wrapped key: %v", err)
	}
	if string(unwrapped) != string(plain) {
		t.Fatalf("unwrapped data key mismatch")
	}
}

func TestEncryptedFileKMSRejectsWrongRootKey(t *testing.T) {
	kms, err := vault.NewEncryptedFileKMSClient("test-ssm-root-key-that-is-long-enough")
	if err != nil {
		t.Fatalf("NewEncryptedFileKMSClient: %v", err)
	}
	_, wrapped, err := kms.GenerateDataKey(context.Background())
	if err != nil {
		t.Fatalf("GenerateDataKey: %v", err)
	}
	wrong, err := vault.NewEncryptedFileKMSClient("different-ssm-root-key-that-is-long-enough")
	if err != nil {
		t.Fatalf("wrong NewEncryptedFileKMSClient: %v", err)
	}
	if _, err := wrong.Decrypt(context.Background(), wrapped); err == nil {
		t.Fatalf("Decrypt with wrong root key succeeded")
	}
}

func TestEncryptedFileKMSRejectsShortRootKey(t *testing.T) {
	if _, err := vault.NewEncryptedFileKMSClient("too-short"); err == nil {
		t.Fatalf("short WEB3_SSM_KEY should be rejected")
	}
}

func TestVaultSignTransaction(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	defer os.RemoveAll(keysDir)

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	key, err := v.CreateWallet(context.Background(), userID, "sign_key")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	to := crypto.PubkeyToAddress(func() *ecdsa.PrivateKey { k, _ := crypto.GenerateKey(); return k }().PublicKey)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(2_000_000_000),
		GasFeeCap: big.NewInt(50_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1e18),
	})

	signedTx, err := v.SignTransaction(context.Background(), userID, key.WalletID, tx, big.NewInt(1))
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}

	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(1)), signedTx)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	if sender.Hex() != key.Address {
		t.Errorf("recovered sender = %s, want %s", sender.Hex(), key.Address)
	}
}

func TestVaultDeleteKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	defer os.RemoveAll(keysDir)

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	_, err := v.CreateWallet(context.Background(), userID, "del_key")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	if err := v.DeleteWallet(context.Background(), userID, "del_key"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}

	_, err = v.GetWallet(userID, "del_key")
	if err == nil {
		t.Error("expected error getting deleted key")
	}
}

func TestVaultDeleteAllowsRecreateSameWalletID(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	defer os.RemoveAll(keysDir)

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	first, err := v.CreateWallet(context.Background(), userID, "reused_key")
	if err != nil {
		t.Fatalf("create first wallet: %v", err)
	}
	if err := db.Create(&storage.PolicyInstance{
		PolicyInstanceID:          "pinst_old_reused_key",
		WalletID:                  first.WalletID,
		UserID:                    userID,
		PolicyDefinitionID:        "pdef_old_reused_key",
		PolicyDefinitionVersionID: "pver_old_reused_key",
	}).Error; err != nil {
		t.Fatalf("create old policy instance: %v", err)
	}

	if err := v.DeleteWallet(context.Background(), userID, first.WalletID); err != nil {
		t.Fatalf("delete first wallet: %v", err)
	}

	second, err := v.CreateWallet(context.Background(), userID, first.WalletID)
	if err != nil {
		t.Fatalf("recreate wallet with same id: %v", err)
	}
	if second.WalletID != first.WalletID {
		t.Fatalf("wallet id = %s, want %s", second.WalletID, first.WalletID)
	}
	if second.Address == first.Address {
		t.Fatalf("recreated wallet reused old address %s", second.Address)
	}

	var oldInstances int64
	db.Model(&storage.PolicyInstance{}).
		Where("wallet_id = ? AND user_id = ?", first.WalletID, userID).
		Count(&oldInstances)
	if oldInstances != 0 {
		t.Fatalf("deleted wallet left policy instances for reused id")
	}
}

func TestVaultSoftDeleteRetainsKeyAndRecreateIsolatesGeneration(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	defer os.RemoveAll(keysDir)

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	first, err := v.CreateWallet(context.Background(), userID, "retained_key")
	if err != nil {
		t.Fatalf("create first wallet: %v", err)
	}
	firstKeyFile := first.KeyFile
	if _, err := os.Stat(firstKeyFile); err != nil {
		t.Fatalf("first key file missing before delete: %v", err)
	}
	if err := db.Create(&storage.PolicyInstance{
		PolicyInstanceID:          "pinst_retained_key_old",
		UserID:                    userID,
		WalletID:                  first.WalletID,
		WalletUID:                 first.WalletUID,
		WalletGeneration:          first.Generation,
		PolicyDefinitionID:        "pdef_retained_key_old",
		PolicyDefinitionVersionID: "pver_retained_key_old",
	}).Error; err != nil {
		t.Fatalf("create old policy instance: %v", err)
	}

	if err := v.DeleteWallet(context.Background(), userID, first.WalletID); err != nil {
		t.Fatalf("soft delete first wallet: %v", err)
	}
	if _, err := os.Stat(firstKeyFile); err != nil {
		t.Fatalf("soft delete should retain key file %s: %v", firstKeyFile, err)
	}
	if _, err := v.SignTransaction(context.Background(), userID, first.WalletID, types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(1),
		Nonce:   1,
		Gas:     21000,
	}), big.NewInt(1)); !errors.Is(err, vault.ErrWalletNotFound) {
		t.Fatalf("sign deleted wallet error = %v, want ErrWalletNotFound", err)
	}

	var deleted storage.Wallet
	if err := db.Where("user_id = ? AND wallet_id = ? AND deleted_at IS NOT NULL", userID, first.WalletID).First(&deleted).Error; err != nil {
		t.Fatalf("load deleted wallet row: %v", err)
	}
	if deleted.WalletUID == "" || deleted.KeyStatus != "soft_deleted" {
		t.Fatalf("deleted wallet metadata not retained: %+v", deleted)
	}
	var oldInstanceCount int64
	db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", "pinst_retained_key_old").Count(&oldInstanceCount)
	if oldInstanceCount != 0 {
		t.Fatalf("old policy instance should be hard deleted after wallet deletion")
	}

	second, err := v.CreateWallet(context.Background(), userID, first.WalletID)
	if err != nil {
		t.Fatalf("recreate same wallet id: %v", err)
	}
	if second.WalletID != first.WalletID {
		t.Fatalf("second wallet id = %s, want %s", second.WalletID, first.WalletID)
	}
	if second.WalletUID == "" || second.WalletUID == deleted.WalletUID {
		t.Fatalf("second wallet uid = %q, deleted uid = %q", second.WalletUID, deleted.WalletUID)
	}
	if second.Generation <= deleted.Generation {
		t.Fatalf("second generation = %d, want > deleted generation %d", second.Generation, deleted.Generation)
	}
	if second.KeyFile == firstKeyFile {
		t.Fatalf("recreated wallet reused retained key file path %s", second.KeyFile)
	}
	if _, err := os.Stat(second.KeyFile); err != nil {
		t.Fatalf("second key file missing: %v", err)
	}
	if _, err := os.Stat(firstKeyFile); err != nil {
		t.Fatalf("first key file should still be retained after recreate: %v", err)
	}
}

func TestVaultKeyNotFound(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	_, err := v.GetWallet(userID, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}
