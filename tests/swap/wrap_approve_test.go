// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

// wethOnlyAdapter declares AcceptsNativeIn=false, AcceptsWETHIn=true so the
// handler is forced to WrapIn the user's native into WETH before calling the
// protocol. Exercises the WrapIn path where the approve target is WETH (the
// post-wrap token), not the user's original native input — asking
// approveCallsForBuild "do I need to approve native?" always returns no and
// leaves the protocol's transferFrom(WETH) to fail.
type wethOnlyAdapter struct{}

func (wethOnlyAdapter) Name() string              { return "weth_only" }
func (wethOnlyAdapter) SupportedChains() []string { return []string{"ethereum"} }
func (wethOnlyAdapter) Capabilities() swap.SwapCapabilities {
	return swap.SwapCapabilities{
		Native: swap.NativeCapability{
			AcceptsNativeIn: false, // forces WrapIn when user sends native
			AcceptsWETHIn:   true,
			OutputsNative:   true,
			OutputsWETH:     true,
		},
		SupportsSlippage: true,
	}
}
func (wethOnlyAdapter) Quote(_ context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	return &swap.QuoteResult{
		Protocol:      "weth_only",
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  "1000000000000000000",
		ApproveStyle:  "erc20",
		ApproveTarget: testSwapRouter,
	}, nil
}
func (wethOnlyAdapter) Build(_ context.Context, _ swap.BuildRequest) (*swap.BuildResult, error) {
	return &swap.BuildResult{
		To: testSwapRouter, Value: "0x0", Data: "0xdeadbeef", GasLimit: 100000,
	}, nil
}

// wethEthereum is the canonical WETH address on Ethereum mainnet.
// Source of truth lives in internal/token.WrappedNativeAddress("ethereum").
const wethEthereum = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"

func TestSwap_WrapIn_ApproveTargetsWETHNotNative(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	walletAddress := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	testutil.CreateTestWallet(t, db, userID, "wallet_wrap_in", walletAddress)

	// Zero current allowance so we expect an approve to be emitted.
	rpc := allowanceRPC(t, "0")
	defer rpc.Close()

	router, _ := setupSwapRouterWithDB(t, db, map[string]string{"ethereum": rpc.URL}, wethOnlyAdapter{})

	body := map[string]interface{}{
		"protocol":     "weth_only",
		"chain":        "ethereum",
		"wallet_id":    "wallet_wrap_in",
		"source_asset": "native", // user sends native ETH
		"target_asset": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
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

	// Expected execution order: WETH.deposit (wrap), WETH.approve (approve), swap call.
	// Find the approve op and verify its target is the WETH contract, not "native".
	var approveOp *security.Operation
	for i, op := range ops {
		if len(op.Data) >= 4 && strings.EqualFold(common.Bytes2Hex(op.Data[:4]), "095ea7b3") {
			approveOp = &ops[i]
			break
		}
	}
	if approveOp == nil {
		t.Fatalf("no approve call emitted; ops = %+v — handler should approve WETH when WrapIn=true", ops)
	}
	if !strings.EqualFold(approveOp.To.Hex(), wethEthereum) {
		t.Errorf("approve targets %s, want WETH contract %s", approveOp.To.Hex(), wethEthereum)
	}
}
