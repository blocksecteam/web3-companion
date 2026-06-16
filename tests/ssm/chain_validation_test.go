// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	ssmhandler "github.com/blocksecteam/web3-companion/internal/ssm/handler"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupSSMRouterWithChainConfig(t *testing.T) (*gin.Engine, string, *gorm.DB, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "main", "0x34239406901B30733212f418AFfbE3c38a8cFb6A")

	cfg := &config.Config{}
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum"}, {Name: "bsc"}, {Name: "polygon"}, {Name: "base"},
	}

	v := vault.New(nil, db, t.TempDir(), nil, nil)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	ssmhandler.RegisterCompanionRoutesWithConfig(api, r, v, db, nil, cfg)
	return r, apiKey, db, userID
}

func TestSSMSign_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey, db, userID := setupSSMRouterWithChainConfig(t)
	intent := createChainValidationIntent(t, db, userID, "arbitrum", 42161)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain arbitrum, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSSMSign_SupportedChain_NotRejectedByChainValidation(t *testing.T) {
	r, apiKey, db, userID := setupSSMRouterWithChainConfig(t)
	intent := createChainValidationIntent(t, db, userID, "ethereum", 1)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})

	if w.Code == http.StatusBadRequest {
		t.Errorf("ethereum should not be rejected by chain validation, got: %s", w.Body.String())
	}
}

func createChainValidationIntent(t *testing.T, db *gorm.DB, userID, chain string, chainID int64) storage.TransactionIntent {
	t.Helper()
	var wallet storage.Wallet
	if err := db.First(&wallet, "wallet_id = ?", "main").Error; err != nil {
		t.Fatalf("load wallet: %v", err)
	}
	intent, err := txintent.NewService(db, vault.NewTestKMSClient()).Create(t.Context(), txintent.CreateRequest{
		UserID:    userID,
		WalletID:  wallet.WalletID,
		WalletUID: wallet.WalletUID,
		Chain:     chain,
		ChainID:   chainID,
		TxType:    "swap",
		Owner:     wallet.Address,
		To:        "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
		Value:     "0",
		Data:      "0x",
		GasLimit:  21000,
		Recipient: "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}
	intent.Status = txintent.StatusSecurityPassed
	intent.SecurityVerdict = "pass"
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save intent: %v", err)
	}
	return intent
}
