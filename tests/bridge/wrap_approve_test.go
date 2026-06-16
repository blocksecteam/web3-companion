// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/bridge"
	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

// nativeOnlyApproveAdapter declares Mode=approve with AcceptsNativeIn=true and
// AcceptsWETHIn=false — a bridge that accepts native via msg.value and uses
// the approve flow for any ERC-20 input it supports separately. When a user
// supplies WETH, the handler must:
//   - prepend WETH.withdraw to convert WETH → native
//   - forward the native value via bridgeCall.Value
//   - NOT emit an approve, because the bridge will receive native (msg.value
//     pull, not transferFrom). Before the symmetric-derivation fix the handler
//     kept approveToken = req.FromToken (= WETH) and emitted a useless
//     approve(WETH, spender, amount).
type nativeOnlyApproveAdapter struct{}

func (nativeOnlyApproveAdapter) Name() string { return "native_only_approve" }
func (nativeOnlyApproveAdapter) Capabilities() bridge.BridgeCapabilities {
	return bridge.BridgeCapabilities{
		Native: bridge.NativeCapability{
			AcceptsNativeIn: true,
			AcceptsWETHIn:   false, // forces Unwrap when user gives WETH
			OutputsNative:   true,
			OutputsWETH:     true,
		},
		Approve:          bridge.ApproveCapability{Mode: bridge.ApproveModeApprove, Style: bridge.ApproveStyleERC20},
		SupportsSlippage: true,
	}
}
func (nativeOnlyApproveAdapter) SupportedPairs() []bridge.ChainPair {
	return []bridge.ChainPair{{FromChain: "ethereum", ToChain: "base"}}
}
func (nativeOnlyApproveAdapter) Quote(_ context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	return &bridge.QuoteResult{
		Bridge:           "native_only_approve",
		InputAmount:      req.Amount,
		EstimatedReceive: req.Amount,
		ApproveTarget:    "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
		RawQuote:         map[string]interface{}{},
	}, nil
}
func (nativeOnlyApproveAdapter) Build(_ context.Context, _ bridge.BuildRequest) (*bridge.BuildResult, error) {
	return &bridge.BuildResult{
		To:       "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 300000,
	}, nil
}

const wethEthereum = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"

// setupBridgeRouterWithDB mirrors setupBridgeWithOptions but exposes the DB so
// the test can fetch the intent afterwards. Inline so it doesn't disturb the
// shared helper used by every other bridge test.
func setupBridgeRouterWithDB(t *testing.T, db *gorm.DB, adapters ...bridge.Adapter) (*gin.Engine, string) {
	t.Helper()
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_bridge", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	opts := []bridge.HandlerOption{
		bridge.WithResolver(&fakeResolver{decimals: 18}),
		bridge.WithDB(db),
	}
	h := bridge.NewHandlerWithOptions(opts, adapters...)
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestBridge_ApproveMode_Unwrap_SkipsApprove(t *testing.T) {
	db := testutil.NewTestDB(t)
	r, apiKey := setupBridgeRouterWithDB(t, db, nativeOnlyApproveAdapter{})

	body := map[string]interface{}{
		"bridge":     "native_only_approve",
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": wethEthereum, // user provides WETH
		"to_token":   "native",     // OutputsNative=true
		"amount":     "1",
		"wallet_id":  "wallet_bridge",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, body)
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

	var sawWithdraw bool
	for _, op := range ops {
		if len(op.Data) < 4 {
			continue
		}
		sel := common.Bytes2Hex(op.Data[:4])
		if sel == "095ea7b3" {
			t.Errorf("handler emitted approve(%s) but bridge receives native via msg.value — approve is useless; ops=%+v", op.To.Hex(), ops)
		}
		if sel == "2e1a7d4d" && strings.EqualFold(op.To.Hex(), wethEthereum) {
			sawWithdraw = true
		}
	}
	if !sawWithdraw {
		t.Errorf("missing WETH.withdraw pre-call; ops=%+v", ops)
	}

	// Unwrap path makes effectiveInput=native, so the handler skips approve and
	// the summary must not surface approve_target (no approval will appear in
	// the trace for the security agent to cross-check against).
	var summary map[string]interface{}
	if err := json.Unmarshal([]byte(intent.SummaryJSON), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if _, ok := summary["approve_target"]; ok {
		t.Errorf("summary should omit approve_target when input is native (no approval emitted); summary=%v", summary)
	}
}

// erc20ApproveAdapter declares Mode=approve and accepts every form on both
// sides — no handler-inserted wrap step. Used to exercise the ERC-20 input
// path where the handler emits an approval and the summary must surface
// approve_target so the security agent can cross-check the spender.
type erc20ApproveAdapter struct{}

func (erc20ApproveAdapter) Name() string { return "erc20_approve" }
func (erc20ApproveAdapter) Capabilities() bridge.BridgeCapabilities {
	return bridge.BridgeCapabilities{
		Native:           bridge.NativeCapability{AcceptsNativeIn: true, AcceptsWETHIn: true, OutputsNative: true, OutputsWETH: true},
		Approve:          bridge.ApproveCapability{Mode: bridge.ApproveModeApprove, Style: bridge.ApproveStyleERC20},
		SupportsSlippage: true,
	}
}
func (erc20ApproveAdapter) SupportedPairs() []bridge.ChainPair {
	return []bridge.ChainPair{{FromChain: "ethereum", ToChain: "base"}}
}
func (erc20ApproveAdapter) Quote(_ context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	return &bridge.QuoteResult{
		Bridge:           "erc20_approve",
		InputAmount:      req.Amount,
		EstimatedReceive: req.Amount,
		ApproveTarget:    "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
		RawQuote:         map[string]interface{}{},
	}, nil
}
func (erc20ApproveAdapter) Build(_ context.Context, _ bridge.BuildRequest) (*bridge.BuildResult, error) {
	return &bridge.BuildResult{
		To: "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae", Value: "0", Data: "0xdeadbeef", GasLimit: 300000,
	}, nil
}

const usdcEthereum = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"

func TestBridge_ApproveMode_ERC20_SurfacesApproveTargetInSummary(t *testing.T) {
	db := testutil.NewTestDB(t)
	r, apiKey := setupBridgeRouterWithDB(t, db, erc20ApproveAdapter{})

	body := map[string]interface{}{
		"bridge":     "erc20_approve",
		"from_chain": "ethereum",
		"to_chain":   "base",
		"from_token": usdcEthereum,
		"to_token":   usdcEthereum,
		"amount":     "1",
		"wallet_id":  "wallet_bridge",
	}
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, body)
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
	var summary map[string]interface{}
	if err := json.Unmarshal([]byte(intent.SummaryJSON), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	got, _ := summary["approve_target"].(string)
	if !strings.EqualFold(got, "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae") {
		t.Errorf("summary.approve_target = %q, want the adapter's ApproveTarget (security agent uses this to verify the on-chain approval spender)", got)
	}
}
