// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/swap"
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
	name     string
	captured swap.QuoteRequest
}

func (a *recordingAdapter) Name() string              { return a.name }
func (a *recordingAdapter) SupportedChains() []string { return []string{"ethereum"} }
func (a *recordingAdapter) Capabilities() swap.SwapCapabilities {
	return swap.SwapCapabilities{
		Native:           swap.NativeCapability{AcceptsNativeIn: true, AcceptsWETHIn: true, OutputsNative: true, OutputsWETH: true},
		SupportsSlippage: true,
	}
}
func (a *recordingAdapter) Quote(_ context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	a.captured = req

	return &swap.QuoteResult{
		Protocol:     a.name,
		SourceAsset:  req.SourceAsset,
		TargetAsset:  req.TargetAsset,
		InputAmount:  req.Amount,
		OutputAmount: "0",
	}, nil
}
func (a *recordingAdapter) Build(_ context.Context, _ swap.BuildRequest) (*swap.BuildResult, error) {
	return nil, nil
}

func setupResolverRouter(t *testing.T, resolver swap.Resolver, adapter swap.ProtocolAdapter) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	reg.Register(adapter)
	h := swap.NewHandler(reg, swap.WithResolver(resolver))
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestQuote_ResolvesDecimalsAndComputesBaseUnitsAmount(t *testing.T) {
	resolver := &fakeResolver{decimals: 6}
	adapter := &recordingAdapter{name: "fake"}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"protocol":     "fake",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "100",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	if resolver.calls != 1 {
		t.Errorf("resolver.Resolve called %d times, want 1", resolver.calls)
	}
	if adapter.captured.Amount != "100000000" {
		t.Errorf("adapter.Amount = %q, want %q (100 × 10^6)", adapter.captured.Amount, "100000000")
	}
	if adapter.captured.DisplayAmount != "100" {
		t.Errorf("adapter.DisplayAmount = %q, want %q (input preserved)", adapter.captured.DisplayAmount, "100")
	}
	if adapter.captured.SourceAsset != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("adapter.SourceAsset wrong: %q", adapter.captured.SourceAsset)
	}
}

func TestQuote_NativeAmountIsConverted(t *testing.T) {
	resolver := &fakeResolver{decimals: 18}
	adapter := &recordingAdapter{name: "fake"}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"protocol":     "fake",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "ETH",
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"amount":       "0.001",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	if adapter.captured.Amount != "1000000000000000" {
		t.Errorf("adapter.Amount = %q, want %q (0.001 × 10^18)", adapter.captured.Amount, "1000000000000000")
	}
}

func TestQuote_ResolverErrorFailsRequest(t *testing.T) {
	resolver := &fakeResolver{err: errFakeResolve()}
	adapter := &recordingAdapter{name: "fake"}
	r, apiKey := setupResolverRouter(t, resolver, adapter)

	body := map[string]interface{}{
		"protocol":     "fake",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xunknown1111111111111111111111111111111111",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "1",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	if w.Code < 400 {
		t.Errorf("got %d, want 4xx/5xx when resolver fails", w.Code)
	}
	if adapter.captured.Amount != "" {
		t.Errorf("adapter should NOT be called when resolver fails; got Amount=%q", adapter.captured.Amount)
	}
}

func errFakeResolve() error { return fakeResolveErr{} }

type fakeResolveErr struct{}

func (fakeResolveErr) Error() string { return "fake resolver failure" }
