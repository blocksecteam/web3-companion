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
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

type fakeResolver struct {
	decimals uint8
	err      error
	calls    int
}

func (f *fakeResolver) Resolve(_ context.Context, _, _ string) (uint8, error) {
	f.calls++
	return f.decimals, f.err
}

type recordingAdapter struct {
	captured bridge.QuoteRequest
}

func (a *recordingAdapter) Name() string { return "fake" }
func (a *recordingAdapter) Capabilities() bridge.BridgeCapabilities {
	return bridge.BridgeCapabilities{
		Native:           bridge.NativeCapability{AcceptsNativeIn: true, AcceptsWETHIn: true, OutputsNative: true, OutputsWETH: true},
		Approve:          bridge.ApproveCapability{Mode: bridge.ApproveModeDeposit},
		SupportsSlippage: false,
	}
}
func (a *recordingAdapter) SupportedPairs() []bridge.ChainPair {
	return []bridge.ChainPair{{FromChain: "ethereum", ToChain: "bsc"}}
}
func (a *recordingAdapter) Quote(_ context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	a.captured = req
	return &bridge.QuoteResult{
		Bridge:           "fake",
		InputAmount:      req.Amount,
		EstimatedReceive: "4900000000000000000",
	}, nil
}
func (a *recordingAdapter) Build(_ context.Context, _ bridge.BuildRequest) (*bridge.BuildResult, error) {
	return &bridge.BuildResult{
		To:       "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 200000,
	}, nil
}

func setupResolverRouter(t *testing.T, resolver bridge.Resolver, adapter bridge.Adapter) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_bridge", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	h := bridge.NewHandlerWithOptions([]bridge.HandlerOption{bridge.WithResolver(resolver), bridge.WithDB(db)}, adapter)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestBridgeQuote_ResolvesDecimalsAndComputesBaseUnitsAmount(t *testing.T) {
	resolver := &fakeResolver{decimals: 6}
	adapter := &recordingAdapter{}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"from_chain": "ethereum",
		"to_chain":   "bsc",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to_token":   "native",
		"amount":     "5",
		"wallet_id":  "wallet_bridge",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	if resolver.calls != 1 {
		t.Errorf("resolver called %d times, want 1", resolver.calls)
	}
	if adapter.captured.Amount != "5000000" {
		t.Errorf("adapter.Amount = %q, want %q (5 × 10^6)", adapter.captured.Amount, "5000000")
	}
}

func TestBridgeQuote_NativeAmountIsConverted(t *testing.T) {
	resolver := &fakeResolver{decimals: 18}
	adapter := &recordingAdapter{}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"from_chain":   "ethereum",
		"to_chain":     "bsc",
		"from_token":   "native",
		"to_token":     "native",
		"amount":       "0.005",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	if adapter.captured.Amount != "5000000000000000" {
		t.Errorf("adapter.Amount = %q, want %q (0.005 × 10^18)", adapter.captured.Amount, "5000000000000000")
	}
}

func TestBridgeQuote_ResolverErrorFailsRequest(t *testing.T) {
	resolver := &fakeResolver{err: bridgeResolveErr{}}
	adapter := &recordingAdapter{}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"from_chain":   "ethereum",
		"to_chain":     "bsc",
		"from_token":   "0xunknown0000000000000000000000000000000001",
		"to_token":     "native",
		"amount":       "1",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, body)
	if w.Code < 400 {
		t.Errorf("got %d, want 4xx when resolver fails", w.Code)
	}
	if adapter.captured.Amount != "" {
		t.Errorf("adapter should NOT be called when resolver fails; got Amount=%q", adapter.captured.Amount)
	}
}

func TestBridgeQuote_NoResolver_Returns500(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	h := bridge.NewHandlerWithAdapters(&recordingAdapter{})
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	body := map[string]interface{}{
		"from_chain":   "ethereum",
		"to_chain":     "bsc",
		"from_token":   "native",
		"to_token":     "native",
		"amount":       "1",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, body)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500 when resolver not configured", w.Code)
	}
}

func TestBridgeBuild_ResolvesDisplayAmount(t *testing.T) {
	resolver := &fakeResolver{decimals: 6}
	adapter := &recordingAdapter{}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"bridge":     "fake",
		"from_chain": "ethereum",
		"to_chain":   "bsc",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to_token":   "native",
		"amount":     "5",
		"wallet_id":  "wallet_bridge",
	}
	testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, body)
	if resolver.calls < 1 {
		t.Errorf("resolver called %d times, want >= 1", resolver.calls)
	}
	if adapter.captured.Amount != "5000000" {
		t.Errorf("adapter.Amount = %q, want %q (5 × 10^6)", adapter.captured.Amount, "5000000")
	}
}

type bridgeResolveErr struct{}

func (bridgeResolveErr) Error() string { return "fake bridge resolver failure" }
