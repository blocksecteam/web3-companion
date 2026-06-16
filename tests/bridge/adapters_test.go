// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupBridge(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_bridge", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	reg := bridge.NewRegistry()
	bridgeadapters.RegisterAll(reg, nil)
	h := bridge.NewHandler(reg)
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func setupBridgeWithOptions(t *testing.T, opts []bridge.HandlerOption, adapters ...bridge.Adapter) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_bridge", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	opts = append(opts, bridge.WithDB(db))
	h := bridge.NewHandlerWithOptions(opts, adapters...)
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

type fakeBridgeAdapter struct {
	quoteCalls int
}

func (a *fakeBridgeAdapter) Name() string { return "fake" }
func (a *fakeBridgeAdapter) Capabilities() bridge.BridgeCapabilities {
	return bridge.BridgeCapabilities{
		Native:           bridge.NativeCapability{AcceptsNativeIn: true, AcceptsWETHIn: true, OutputsNative: true, OutputsWETH: true},
		Approve:          bridge.ApproveCapability{Mode: bridge.ApproveModeDeposit},
		SupportsSlippage: false,
	}
}
func (a *fakeBridgeAdapter) SupportedPairs() []bridge.ChainPair {
	return []bridge.ChainPair{{FromChain: "ethereum", ToChain: "base"}}
}
func (a *fakeBridgeAdapter) Quote(ctx context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	a.quoteCalls++
	return &bridge.QuoteResult{
		Bridge:           a.Name(),
		InputAmount:      req.Amount,
		EstimatedReceive: req.Amount,
		RawQuote: map[string]interface{}{
			"transactionRequest": map[string]interface{}{
				"to":       "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
				"value":    "0x0",
				"data":     "0xdeadbeef",
				"gasLimit": "300000",
			},
		},
	}, nil
}
func (a *fakeBridgeAdapter) Build(ctx context.Context, req bridge.BuildRequest) (*bridge.BuildResult, error) {
	return &bridge.BuildResult{
		To:       "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 300000,
	}, nil
}

func TestBridge_Protocols_AllThreeRegistered(t *testing.T) {
	r, apiKey := setupBridge(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/bridge/protocols", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	protocols, ok := resp["protocols"].([]interface{})
	if !ok {
		t.Fatal("missing protocols field")
	}
	found := make(map[string]bool)
	for _, p := range protocols {
		entry, ok := p.(map[string]interface{})
		if !ok {
			t.Fatalf("protocol entry not an object: %#v", p)
		}
		name, _ := entry["name"].(string)
		found[name] = true
	}
	for _, expected := range []string{"bungee", "relay", "lifi"} {
		if !found[expected] {
			t.Errorf("protocol %q not found in list: %v", expected, protocols)
		}
	}
}

func TestBridge_Quote_RequiredFields_Validation(t *testing.T) {
	r, apiKey := setupBridge(t)

	tests := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"missing from_chain", map[string]interface{}{"to_chain": "base", "from_token": "0x...", "amount": "100"}, 400},
		{"missing to_chain", map[string]interface{}{"from_chain": "ethereum", "from_token": "0x...", "amount": "100"}, 400},
		{"missing amount", map[string]interface{}{"from_chain": "ethereum", "to_chain": "base", "from_token": "0x..."}, 400},
		{"empty body", map[string]interface{}{}, 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, tt.body)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestBridge_SupportedPairs(t *testing.T) {
	adapter := &fakeBridgeAdapter{}
	r, apiKey := setupBridgeWithOptions(
		t,
		[]bridge.HandlerOption{bridge.WithResolver(&fakeResolver{decimals: 18})},
		adapter,
	)

	body := map[string]interface{}{
		"from_chain":   "ethereum",
		"to_chain":     "base",
		"from_token":   "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "1",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, body)
	t.Logf("bridge quote: %d: %s", w.Code, w.Body.String())
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestBridge_Build_MissingFields(t *testing.T) {
	r, apiKey := setupBridge(t)
	tests := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"missing bridge", map[string]interface{}{}, 400},
		{"missing required fields", map[string]interface{}{"bridge": "fake"}, 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, tt.body)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestBridge_Build_ReturnsExecutableCalldata(t *testing.T) {
	adapter := &fakeBridgeAdapter{}
	r, apiKey := setupBridgeWithOptions(
		t,
		[]bridge.HandlerOption{bridge.WithResolver(&fakeResolver{decimals: 18})},
		adapter,
	)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, map[string]interface{}{
		"bridge":     "fake",
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":     "1",
		"wallet_id":  "wallet_bridge",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	if resp["transaction_id"] == "" || resp["status"] != "built" {
		t.Fatalf("expected transaction intent response, got %v", resp)
	}
	if adapter.quoteCalls != 1 {
		t.Fatalf("expected build to re-quote once, got %d quote calls", adapter.quoteCalls)
	}
}

func TestBridge_Build_UnknownBridge_404(t *testing.T) {
	r, apiKey := setupBridgeWithOptions(t, []bridge.HandlerOption{bridge.WithResolver(&fakeResolver{decimals: 18})}, &fakeBridgeAdapter{})
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, map[string]interface{}{
		"bridge":     "unknown_bridge",
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":     "1",
		"wallet_id":  "wallet_bridge",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestBridge_Protocols_Unauthorized(t *testing.T) {
	r, _ := setupBridge(t)
	w := testutil.DoRequest(r, "GET", "/api/v1/bridge/protocols", nil, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}
