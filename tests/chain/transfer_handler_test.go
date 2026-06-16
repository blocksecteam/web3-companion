// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/chain"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

type fakeResolver struct{ decimals uint8 }

func (f *fakeResolver) Resolve(_ context.Context, _, _ string) (uint8, error) {
	return f.decimals, nil
}

func setupTransferRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	r, apiKey, _ := setupTransferRouterWithResolver(t, nil)
	return r, apiKey
}

func setupTransferRouterWithResolver(t *testing.T, resolver chain.TokenResolver) (*gin.Engine, string, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_transfer", "0xa9C0378137f494FFCddcf862B7b52e01f0b01482")
	cfg := &config.Config{}
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, RPCURL: ""},
		{Name: "base", ChainID: 8453, RPCURL: ""},
	}
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	opts := []func(*chain.Handler){}
	if resolver != nil {
		opts = append(opts, chain.WithTokenResolver(resolver))
	}
	opts = append(opts, chain.WithTransactionStore(db))
	chain.RegisterRoutesWithOptions(api, cfg, opts...)
	return r, apiKey, db
}

func TestTransfer_Native_Returns200WithCorrectFields(t *testing.T) {
	r, apiKey, db := setupTransferRouterWithResolver(t, nil)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":     "ethereum",
		"wallet_id": "wallet_transfer",
		"token":     "native",
		"to":        "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount":    "0.001",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TransactionID string `json:"transaction_id"`
		Status        string `json:"status"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.TransactionID == "" {
		t.Fatal("transaction_id is empty")
	}
	if resp.Status != txintent.StatusBuilt {
		t.Fatalf("status = %q, want %q", resp.Status, txintent.StatusBuilt)
	}
	var intent storage.TransactionIntent
	if err := db.First(&intent, "transaction_id = ?", resp.TransactionID).Error; err != nil {
		t.Fatalf("intent not stored: %v", err)
	}
	// Native transfer now goes through BuildExecuteBundle (ERC-7821 batch),
	// same as ERC-20 transfer and swap. The outer tx targets the user's own
	// (delegated) wallet; the recipient is encoded inside the batch calldata.
	if !strings.EqualFold(intent.To, "0xa9C0378137f494FFCddcf862B7b52e01f0b01482") {
		t.Fatalf("intent.To should be the sender's own delegated wallet, got %q", intent.To)
	}
	if intent.GasLimit != 70_000 {
		t.Fatalf("intent.GasLimit = %d, want 70_000 (fixed native transfer gas)", intent.GasLimit)
	}
	if !strings.HasPrefix(intent.Data, "0xe9ae5c53") {
		t.Fatalf("intent.Data should start with ERC-7821 execute selector, got %s", intent.Data[:10])
	}
	// Inner call value = 0.001e18 = 1000000000000000 = 0x38d7ea4c68000; total = same.
	if !strings.EqualFold(intent.Value, "0x38d7ea4c68000") {
		t.Fatalf("intent.Value = %q, want 0x38d7ea4c68000 (1e15 hex)", intent.Value)
	}
	// Recipient address must appear in the batch calldata.
	if !strings.Contains(strings.ToLower(intent.Data), "97d618e3a0afeccfb5c50b5df258cd7e48650510") {
		t.Fatalf("recipient address not found in batch calldata: %s", intent.Data)
	}
}

func TestTransfer_Native_TokenEth_Rejected(t *testing.T) {

	r, apiKey := setupTransferRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":  "ethereum",
		"token":  "eth",
		"to":     "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount": "0.001",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("'eth' should be rejected (use 'native'), got %d: %s", w.Code, w.Body.String())
	}
}

func TestTransfer_ERC20_Returns200WithERC7821Calldata(t *testing.T) {
	r, apiKey, db := setupTransferRouterWithResolver(t, &fakeResolver{decimals: 6})
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":     "ethereum",
		"wallet_id": "wallet_transfer",
		"token":     "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to":        "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount":    "0.1",
	})

	if w.Code == http.StatusBadRequest {
		t.Errorf("valid ERC-20 transfer should not be 400, got: %s", w.Body.String())
	}
	if w.Code == http.StatusOK {
		var resp struct {
			TransactionID string `json:"transaction_id"`
		}
		testutil.ParseJSON(t, w, &resp)
		var intent storage.TransactionIntent
		if err := db.First(&intent, "transaction_id = ?", resp.TransactionID).Error; err != nil {
			t.Fatalf("intent not stored: %v", err)
		}
		if !strings.HasPrefix(intent.Data, "0xe9ae5c53") {
			t.Errorf("ERC-20: expected ERC-7821 selector 0xe9ae5c53, got %q", intent.Data)
		}
		if intent.To != "0xa9C0378137f494FFCddcf862B7b52e01f0b01482" {
			t.Errorf("ERC-20: to should be wallet address (self-call), got %v", intent.To)
		}
	}
}

func TestTransfer_ERC20_MissingWalletID_Returns400(t *testing.T) {
	r, apiKey := setupTransferRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":  "ethereum",
		"token":  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to":     "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount": "0.1",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("ERC-20 without wallet_id should be 400, got %d", w.Code)
	}
}

func TestTransfer_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupTransferRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":  "arbitrum",
		"token":  "native",
		"to":     "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount": "0.001",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("unsupported chain should be 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTransfer_BareSymbol_Returns400(t *testing.T) {
	r, apiKey := setupTransferRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":        "ethereum",
		"token":        "USDC",
		"to":           "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount":       "0.1",
		"from_address": "0xa9C0378137f494FFCddcf862B7b52e01f0b01482",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("bare symbol USDC should be 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTransfer_EmptyToken_Returns400(t *testing.T) {
	r, apiKey := setupTransferRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain":  "ethereum",
		"token":  "",
		"to":     "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		"amount": "0.001",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty token should be 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTransfer_MissingRequiredField_Returns400(t *testing.T) {
	r, apiKey := setupTransferRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
		"chain": "ethereum",
		"token": "native",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing required fields should be 400, got %d", w.Code)
	}
}

// Recipient must be a valid 40-char hex address. Without this guard,
// padHexAddress silently left-pads / truncates malformed input so a 39-char
// address turns into a real-but-shifted recipient, a 41-char input truncates
// to its trailing 40 chars (a different valid-looking address), and empty
// input becomes the zero address. The validation rejects these before they
// reach calldata.
func TestTransfer_RejectsMalformedRecipient(t *testing.T) {
	cases := []struct {
		name string
		to   string
	}{
		// 39 hex chars after 0x — too short
		{"short", "0x97d618E3a0AFECCFB5C50B5dF258cD7E4865051"},
		// 41 hex chars after 0x — too long
		{"long", "0x97d618E3a0AFECCFB5C50B5dF258cD7E486505100"},
		// Non-hex character inside an otherwise 40-char string
		{"non_hex_char", "0x97d618E3a0AFECCFB5C50B5dF258cD7E486505XY"},
		// Empty
		{"empty", ""},
		// Just the prefix
		{"prefix_only", "0x"},
		// Whitespace-only (trim'd to empty)
		{"whitespace", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, apiKey := setupTransferRouter(t)
			w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/transfer", apiKey, map[string]interface{}{
				"chain":     "ethereum",
				"wallet_id": "wallet_transfer",
				"token":     "native",
				"to":        tc.to,
				"amount":    "0.001",
			})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("malformed recipient %q should be 400, got %d: %s", tc.to, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "invalid_recipient") && !strings.Contains(w.Body.String(), "invalid_request") {
				// missing/empty `to` is rejected at JSON binding (binding:"required"),
				// so it surfaces as invalid_request; everything else hits the explicit
				// IsHexAddress guard and returns invalid_recipient.
				t.Errorf("expected invalid_recipient (or invalid_request for empty), got: %s", w.Body.String())
			}
		})
	}
}
