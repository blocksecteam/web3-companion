// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain_test

import (
	"net/http"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/chain"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func setupChainRouterWithAuth(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	cfg := &config.Config{}
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum"}, {Name: "bsc"}, {Name: "polygon"}, {Name: "base"},
	}
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	chain.RegisterRoutes(api, cfg)
	return r, apiKey
}

func TestChainGas_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupChainRouterWithAuth(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/gas/arbitrum", apiKey, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain arbitrum, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChainGas_SupportedChain_NotRejectedByChainValidation(t *testing.T) {
	r, apiKey := setupChainRouterWithAuth(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/gas/ethereum", apiKey, nil)

	if w.Code == http.StatusBadRequest {
		t.Errorf("ethereum should not be rejected by chain validation, got: %s", w.Body.String())
	}
}

func TestChainBroadcast_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupChainRouterWithAuth(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/broadcast", apiKey, map[string]interface{}{
		"chain":     "optimism",
		"signed_tx": "0x02f9...",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain optimism, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChainEstimate_UnsupportedChain_Returns400(t *testing.T) {
	r, apiKey := setupChainRouterWithAuth(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/chain/estimate", apiKey, map[string]interface{}{
		"chain": "avalanche",
		"from":  "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"to":    "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported chain avalanche, got %d: %s", w.Code, w.Body.String())
	}
}
