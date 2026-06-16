// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/chain"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func setupChainRouterWithConfig(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)

	cfg := &config.Config{}
	cfg.Chains = []config.ChainConfig{
		{
			Name: "ethereum", ChainID: 1, NativeSymbol: "ETH", RPCURL: "https://eth.example.test",
			CanonicalTokens: map[string]config.CanonicalToken{
				"USDC": {Address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Decimals: 6},
				"WETH": {Address: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", Decimals: 18},
			},
		},
		{
			Name: "base", ChainID: 8453, NativeSymbol: "ETH", RPCURL: "https://base.example.test",
			CanonicalTokens: map[string]config.CanonicalToken{
				"USDC": {Address: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", Decimals: 6},
			},
		},
	}

	r := gin.New()
	api := r.Group("/api/v1")
	chain.RegisterRoutes(api, cfg)
	_ = apiKey
	return r, apiKey
}

func TestChainsEndpoint_ReturnsAllConfiguredChains(t *testing.T) {
	r, _ := setupChainRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/supported", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var chains []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &chains); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(chains) != 2 {
		t.Errorf("expected 2 chains, got %d", len(chains))
	}
}

func TestChainsEndpoint_IncludesChainFields(t *testing.T) {
	r, _ := setupChainRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/supported", "", nil)
	var chains []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &chains)

	eth := chains[0]
	if eth["name"] != "ethereum" {
		t.Errorf("expected name=ethereum, got %v", eth["name"])
	}
	if eth["chain_id"] == nil {
		t.Error("chain_id should be present")
	}
	if eth["native_symbol"] != "ETH" {
		t.Errorf("expected native_symbol=ETH, got %v", eth["native_symbol"])
	}
}

func TestChainsEndpoint_IncludesCanonicalTokens(t *testing.T) {
	r, _ := setupChainRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/supported", "", nil)
	var chains []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &chains)

	eth := chains[0]
	tokens, ok := eth["canonical_tokens"].(map[string]interface{})
	if !ok {
		t.Fatalf("canonical_tokens missing or wrong type")
	}
	usdc, ok := tokens["USDC"].(map[string]interface{})
	if !ok {
		t.Fatal("USDC missing in canonical_tokens")
	}
	if usdc["address"] != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("wrong USDC address: %v", usdc["address"])
	}
	if usdc["decimals"] == nil {
		t.Error("decimals should be present")
	}
}

func TestChainsEndpoint_IsPublicNoAuthRequired(t *testing.T) {
	r, _ := setupChainRouterWithConfig(t)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/chain/supported", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("chains endpoint should be public, got %d", w.Code)
	}
}
