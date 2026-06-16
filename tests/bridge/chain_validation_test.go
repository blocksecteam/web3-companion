// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"net/http"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func setupBridgeRouterWithConfig(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_bridge", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	cfg := &config.Config{}
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum"}, {Name: "bsc"}, {Name: "polygon"}, {Name: "base"},
	}
	reg := bridge.NewRegistry()
	bridgeadapters.RegisterAll(reg, nil)
	h := bridge.NewHandler(reg, bridge.WithConfig(cfg), bridge.WithDB(db))
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestBridgeQuote_UnsupportedFromChain_Returns400(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "arbitrum",
		"to_chain":   "base",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":     "1000000",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported from_chain arbitrum, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBridgeQuote_UnsupportedToChain_Returns400(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "avalanche",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":     "1000000",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported to_chain avalanche, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBridgeQuote_SupportedChains_NotRejectedByChainValidation(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":     "1000000",
	})
	if w.Code == http.StatusBadRequest {
		t.Errorf("supported chains should not be rejected by chain validation, got: %s", w.Body.String())
	}
}

func TestBridgeBuild_UnsupportedFromChain_Returns400(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/build", apiKey, map[string]interface{}{
		"bridge":     "relay",
		"from_chain": "optimism",
		"to_chain":   "base",
		"from_token": "0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85",
		"amount":     "1",
		"wallet_id":  "wallet_bridge",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported from_chain optimism in build, got %d: %s", w.Code, w.Body.String())
	}
}
