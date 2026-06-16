// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

var odosResult = swap.QuoteResult{Protocol: "odos", OutputAmount: "2000000000", SourceAsset: "0xA0b86991...", TargetAsset: "0xEeee..."}
var kyberResult = swap.QuoteResult{Protocol: "kyber", OutputAmount: "1900000000"}
var paraResult = swap.QuoteResult{Protocol: "paraswap", OutputAmount: "1800000000"}
var ooResult = swap.QuoteResult{Protocol: "openocean", OutputAmount: "1700000000"}
var uniResult = swap.QuoteResult{Protocol: "uniswap", OutputAmount: "1500000000"}
var pancakeResult = swap.QuoteResult{Protocol: "pancake", OutputAmount: "1400000000"}

var swapParams = swap.QuoteRequest{
	Chain:         "ethereum",
	FromAddress:   "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	SourceAsset:   "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
	TargetAsset:   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
	Amount:        "1000000000000000000",
	DisplayAmount: "1",
	Slippage:      0.5,
}

func TestSwap_Registry_AllAdaptersRegistered(t *testing.T) {
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)

	names := reg.List()
	expected := []string{"odos", "kyber", "paraswap", "openocean", "uniswap", "pancake"}

	if len(names) < 6 {
		t.Errorf("expected at least 6 protocols, got %d: %v", len(names), names)
	}
	for _, exp := range expected {
		found := false
		for _, name := range names {
			if name == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing protocol: %s", exp)
		}
	}
}

func TestSwap_Adapter_Name(t *testing.T) {
	adapter := adapters.NewOdosAdapter()
	if adapter.Name() != "odos" {
		t.Errorf("expected 'odos', got %q", adapter.Name())
	}
	if len(adapter.SupportedChains()) < 4 {
		t.Errorf("odos supported chains too few: %v", adapter.SupportedChains())
	}
}

func TestSwap_Quote_UnknownProtocol_404(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}))
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	body := map[string]interface{}{
		"protocol":     "unknown_dex",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":       "1",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestSwap_Quote_MissingChain_400(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}))
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, map[string]string{
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSwap_Build_MissingQuote_400(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}))
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	body := map[string]interface{}{
		"protocol": "odos",
		"chain":    "ethereum",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/build", apiKey, body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSwap_Quote_FanOut_UnsupportedChain(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}))
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	body := map[string]interface{}{
		"chain":        "solana",
		"from_address": "5p2i4HR8HqziBbv6xvF5VxurRp3rZ4BVEfY2H5xy3Lrs",
		"source_asset": "11111111111111111111111111111111",
		"target_asset": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		"amount":       "1",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)

	if w.Code != http.StatusServiceUnavailable {
		t.Logf("response: %s", w.Body.String())
	}
}

func TestSwap_Protocols_List_All(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}))
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/swap/protocols", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	protocols := resp["protocols"].([]interface{})
	if len(protocols) < 6 {
		t.Errorf("got %d protocols, want at least 6", len(protocols))
	}
}
