// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestSSM_CreateAndSign_Real(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()

	ctx := integrationCtx(t)
	kms, err := vault.NewKMSClient(ctx,
		envOr(t, "AWS_REGION", "us-east-1"),
		envOr(t, "AWS_KMS_KEY_ID", ""),
	)
	if err != nil {
		t.Fatalf("kms init: %v", err)
	}

	v := vault.New(kms, db, keysDir, nil, nil)
	wallet, err := v.CreateWallet(ctx, userID, "integration_test_wallet")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	t.Logf("created key: %s", wallet.Address)

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

	signedTx, err := v.SignTransaction(ctx, userID, wallet.WalletID, tx, big.NewInt(1))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(1)), signedTx)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	if sender.Hex() != wallet.Address {
		t.Errorf("sender mismatch: %s vs %s", sender.Hex(), wallet.Address)
	}
	t.Logf("signature valid for address: %s", sender.Hex())

	v.DeleteWallet(ctx, userID, wallet.WalletID)

	_, err = v.GetWallet(userID, "integration_test_wallet")
	if err == nil {
		t.Error("key should be deleted")
	}
}

func TestSSM_KeyCRUD_Lifecycle(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	keysDir := t.TempDir()
	ctx := integrationCtx(t)

	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, keysDir, nil, nil)

	wallet, err := v.CreateWallet(ctx, userID, "lifecycle_key")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := v.GetWallet(userID, "lifecycle_key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Address != wallet.Address {
		t.Error("address mismatch")
	}

	keys, err := v.ListWallets(userID, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("list count: %d, want 1", len(keys))
	}

	if err := v.DeleteWallet(ctx, userID, "lifecycle_key"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = v.GetWallet(userID, "lifecycle_key")
	if err == nil {
		t.Error("key should not be found after delete")
	}
}
