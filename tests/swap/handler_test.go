// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupSwapRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_swap", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")

	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 18}), swap.WithDB(db))

	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)

	return r, apiKey
}

func TestSwap_ListProtocols(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/swap/protocols", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestSwap_ListProtocols_Unauthorized(t *testing.T) {
	r, _ := setupSwapRouter(t)
	w := testutil.DoRequest(r, "GET", "/api/v1/swap/protocols", nil, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestSwap_Quote_SingleProtocol(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	body := map[string]interface{}{
		"protocol":     "odos",
		"chain":        "ethereum",
		"wallet_id":    "wallet_swap",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "100",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)

	t.Logf("quote response: %s", w.Body.String())
}

func TestSwap_Quote_UnknownProtocol(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	body := map[string]interface{}{
		"protocol":     "nonexistent",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "100",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestSwap_Quote_MissingFields(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSwap_Quote_RequiresAmount(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	body := map[string]interface{}{
		"protocol":     "odos",
		"chain":        "ethereum",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/quote", apiKey, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestSwap_Build_MissingFields(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/build", apiKey, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSwap_Build_ResponseShape(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	body := map[string]interface{}{
		"protocol":     "odos",
		"chain":        "ethereum",
		"wallet_id":    "wallet_swap",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "100",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/build", apiKey, body)
	if w.Code == http.StatusOK {
		var resp map[string]interface{}
		testutil.ParseJSON(t, w, &resp)
		for _, key := range []string{"transaction_id", "status", "summary", "expires_at"} {
			if _, ok := resp[key]; !ok {
				t.Errorf("build response missing field %q", key)
			}
		}
	}
}

func TestSwap_BuildIntentIncludesApproveCallWhenNeeded(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	walletAddress := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	testutil.CreateTestWallet(t, db, userID, "wallet_swap_approve", walletAddress)

	rpc := allowanceRPC(t, "0")
	defer rpc.Close()
	router, _ := setupSwapRouterWithDB(t, db, map[string]string{"ethereum": rpc.URL}, &buildRecordingAdapter{name: "fake"})

	body := map[string]interface{}{
		"protocol":     "fake",
		"chain":        "ethereum",
		"wallet_id":    "wallet_swap_approve",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "1",
	}
	w := testutil.DoAuthRequest(router, "POST", "/api/v1/swap/build", apiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("build = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TransactionID string `json:"transaction_id"`
	}
	testutil.ParseJSON(t, w, &resp)

	var intent storage.TransactionIntent
	if err := db.First(&intent, "transaction_id = ?", resp.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	ops, check := security.DecodeOperations(security.CheckRequest{
		To:    intent.To,
		Value: intent.Value,
		Data:  intent.Data,
	})
	if check.Status != "pass" {
		t.Fatalf("decode intent calldata: %#v", check)
	}
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want approve + swap", len(ops))
	}
	if ops[0].To != common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48") {
		t.Fatalf("first op to = %s, want token approve", ops[0].To.Hex())
	}
	if got := fmt.Sprintf("0x%x", ops[0].Data[:4]); got != "0x095ea7b3" {
		t.Fatalf("first op method = %s, want approve", got)
	}
	if ops[1].To != common.HexToAddress(testSwapRouter) {
		t.Fatalf("second op to = %s, want swap router %s", ops[1].To.Hex(), testSwapRouter)
	}
}

func TestSwap_Build_UnknownProtocol(t *testing.T) {
	r, apiKey := setupSwapRouter(t)
	body := map[string]interface{}{
		"protocol":     "nonexistent",
		"chain":        "ethereum",
		"wallet_id":    "wallet_swap",
		"source_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"target_asset": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"amount":       "100",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/swap/build", apiKey, body)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

const testSwapRouter = "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"

type buildRecordingAdapter struct {
	name string
}

func (a *buildRecordingAdapter) Name() string              { return a.name }
func (a *buildRecordingAdapter) SupportedChains() []string { return []string{"ethereum"} }
func (a *buildRecordingAdapter) Capabilities() swap.SwapCapabilities {
	return swap.SwapCapabilities{
		Native:           swap.NativeCapability{AcceptsNativeIn: true, AcceptsWETHIn: true, OutputsNative: true, OutputsWETH: true},
		SupportsSlippage: true,
	}
}
func (a *buildRecordingAdapter) Quote(_ context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	return &swap.QuoteResult{
		Protocol:      a.name,
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  "1000",
		ApproveTarget: testSwapRouter,
		RawQuote:      map[string]string{"ok": "true"},
	}, nil
}

func (a *buildRecordingAdapter) Build(_ context.Context, _ swap.BuildRequest) (*swap.BuildResult, error) {
	return &swap.BuildResult{To: testSwapRouter, Value: "0x0", Data: "0xdeadbeef", GasLimit: 100000}, nil
}

func setupSwapRouterWithDB(t *testing.T, db *gorm.DB, rpcURLs map[string]string, adapter swap.ProtocolAdapter) (*gin.Engine, string) {
	t.Helper()
	reg := swap.NewRegistry()
	reg.Register(adapter)
	cfg := &config.Config{Chains: []config.ChainConfig{{Name: "ethereum", ChainID: 1, RPCURL: rpcURLs["ethereum"]}}}
	h := swap.NewHandler(reg, swap.WithResolver(&fakeResolver{decimals: 6}), swap.WithRPCURLs(rpcURLs), swap.WithConfig(cfg), swap.WithDB(db))
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, ""
}

func allowanceRPC(t *testing.T, allowance string) *httptest.Server {
	t.Helper()
	n := new(big.Int)
	n.SetString(allowance, 10)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%064x"}`, n)
	}))
}
