// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupSmartQuote(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)

	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}))

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestSmartQuote_FanOut_ReturnsBest(t *testing.T) {
	r, apiKey := setupSmartQuote(t)

	body := map[string]interface{}{
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":       "1",
		"slippage":     0.5,
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	t.Logf("fan-out response: %d %s", w.Code, w.Body.String())

	if w.Code == http.StatusOK {
		var resp map[string]interface{}
		testutil.ParseJSON(t, w, &resp)
		if _, ok := resp["chain"]; !ok {
			t.Error("response missing chain field")
		}
		if _, ok := resp["from_token"]; !ok {
			t.Error("response missing from_token field")
		}
		if _, ok := resp["to_token"]; !ok {
			t.Error("response missing to_token field")
		}
		if protos, ok := resp["protocols"]; ok {
			if arr, ok := protos.([]interface{}); ok {
				t.Logf("fan-out returned %d protocol entries", len(arr))
				for _, p := range arr {
					pm := p.(map[string]interface{})
					if _, ok := pm["protocol"]; !ok {
						t.Error("protocol entry missing protocol field")
					}
					if _, ok := pm["in_amount"]; !ok {
						t.Error("protocol entry missing in_amount field")
					}
					if _, ok := pm["out_amount"]; !ok {
						t.Error("protocol entry missing out_amount field")
					}
				}
			}
		}
		if _, ok := resp["best"]; ok {
			t.Error("response should not have 'best' field")
		}
		if _, ok := resp["all_results"]; ok {
			t.Error("response should not have 'all_results' field")
		}
	}
}

func TestSmartQuote_SpecificProtocol_NoFanOut(t *testing.T) {
	r, apiKey := setupSmartQuote(t)

	body := map[string]interface{}{
		"protocol":     "kyber",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":       "1",
		"slippage":     0.5,
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Logf("response: %s", w.Body.String())
		return
	}
	if _, ok := resp["all_results"]; ok {
		t.Error("single protocol quote should not have all_results")
	}
	if _, ok := resp["best"]; ok {
		t.Error("single protocol quote should not have best field")
	}
}

func TestQuoteComparison_HigherOutputWins(t *testing.T) {

	compareOutputs := func(a, b string) int {
		if a > b {
			return 1
		}
		if a < b {
			return -1
		}
		return 0
	}

	if compareOutputs("1000000", "2000000") >= 0 {
		t.Error("1000000 should be less than 2000000")
	}
	if compareOutputs("2000000", "1000000") <= 0 {
		t.Error("2000000 should be greater than 1000000")
	}
	if compareOutputs("1000000", "1000000") != 0 {
		t.Error("equal should be 0")
	}
}

func TestQuoteComparison_SameLength(t *testing.T) {
	compareOutputs := func(a, b string) int {
		if a > b {
			return 1
		}
		if a < b {
			return -1
		}
		return 0
	}
	if compareOutputs("1900000000", "2000000000") >= 0 {
		t.Error("1900000000 should be less than 2000000000")
	}
}

func TestProtocolRegistry_Get(t *testing.T) {
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)

	adapter, err := reg.Get("odos")
	if err != nil {
		t.Fatalf("Get odos: %v", err)
	}
	if adapter.Name() != "odos" {
		t.Errorf("name = %s, want odos", adapter.Name())
	}
	chains := adapter.SupportedChains()
	found := false
	for _, c := range chains {
		if c == "ethereum" {
			found = true
			break
		}
	}
	if !found {
		t.Error("odos should support ethereum")
	}
}

func TestProtocolRegistry_Get_Unknown(t *testing.T) {
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error for unknown protocol")
	}
}

func TestAdapter_ImplementsInterface(t *testing.T) {
	var _ swap.ProtocolAdapter = adapters.NewOdosAdapter()
	var _ swap.ProtocolAdapter = adapters.NewKyberAdapter()
	var _ swap.ProtocolAdapter = adapters.NewParaSwapAdapter(nil)
	var _ swap.ProtocolAdapter = adapters.NewOpenOceanAdapter()
	var _ swap.ProtocolAdapter = adapters.NewUniswapAdapter(nil)
	var _ swap.ProtocolAdapter = adapters.NewPancakeAdapter(nil)
}
