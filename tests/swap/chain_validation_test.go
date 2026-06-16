// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"net/http"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func setupSwapRouterWithConfig(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	cfg := &config.Config{}
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum"}, {Name: "bsc"}, {Name: "polygon"}, {Name: "base"},
	}
	reg := swap.NewRegistry()
	h := swap.NewHandler(reg, swap.WithConfig(cfg))
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestSwapQuote_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupSwapRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/quote", apiKey, map[string]interface{}{
		"chain":        "arbitrum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "eth",
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":       "0.001",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain arbitrum, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwapQuote_SupportedChain_NotRejectedByChainValidation(t *testing.T) {
	r, apiKey := setupSwapRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/quote", apiKey, map[string]interface{}{
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "eth",
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":       "0.001",
	})
	if w.Code == http.StatusBadRequest {
		t.Errorf("ethereum should not be rejected, got: %s", w.Body.String())
	}
}

func TestSwapBuild_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupSwapRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/build", apiKey, map[string]interface{}{
		"protocol":     "odos",
		"chain":        "solana",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "1",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain solana in build, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwapCheckAllowance_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupSwapRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/check-allowance", apiKey, map[string]interface{}{
		"chain":   "avalanche",
		"token":   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"owner":   "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"spender": "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
		"amount":  "1000000",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain avalanche, got %d: %s", w.Code, w.Body.String())
	}
}
