// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"net/http"
	"testing"

	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestBridgeQuote_BareFromTokenSymbol_Returns400(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "usdc",
		"amount":     "1000000",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bare symbol 'usdc' as from_token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBridgeQuote_BareToTokenSymbol_Returns400(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to_token":   "usdc",
		"amount":     "1000000",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bare symbol 'usdc' as to_token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBridgeQuote_ContractAddress_NotRejectedByTokenValidation(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":     "1000000",
	})
	if w.Code == http.StatusBadRequest {
		t.Errorf("contract address should not be rejected by token validation, got: %s", w.Body.String())
	}
}

func TestBridgeQuote_NativeToken_NotRejectedByTokenValidation(t *testing.T) {
	r, apiKey := setupBridgeRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "native",
		"amount":     "1000000000000000",
	})
	if w.Code == http.StatusBadRequest {
		t.Errorf("'native' should not be rejected by token validation, got: %s", w.Body.String())
	}
}
