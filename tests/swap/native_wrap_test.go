// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/swap"
)

var testWETH = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"

func TestWrapCalls_AllFalse_EmptyResult(t *testing.T) {
	pre, post := swap.WrapCalls(testWETH, swap.NativeWrap{}, big.NewInt(1e18), big.NewInt(9e17))
	if len(pre) != 0 || len(post) != 0 {
		t.Errorf("expected empty pre/post, got pre=%d post=%d", len(pre), len(post))
	}
}

func TestWrapCalls_WrapIn_PreHasDeposit(t *testing.T) {
	amountIn := big.NewInt(1e18)
	pre, post := swap.WrapCalls(testWETH, swap.NativeWrap{WrapIn: true}, amountIn, big.NewInt(9e17))
	if len(pre) != 1 {
		t.Fatalf("expected 1 pre call, got %d", len(pre))
	}
	if len(post) != 0 {
		t.Errorf("expected 0 post calls, got %d", len(post))
	}
	call := pre[0]
	if !strings.EqualFold(call.To, testWETH) {
		t.Errorf("pre call To = %s, want %s", call.To, testWETH)
	}
	if call.Data != "0xd0e30db0" {
		t.Errorf("pre call Data = %s, want 0xd0e30db0 (deposit selector)", call.Data)
	}
	if call.Value.Cmp(amountIn) != 0 {
		t.Errorf("pre call Value = %s, want %s", call.Value, amountIn)
	}
}

func TestWrapCalls_UnwrapIn_PreHasWithdraw(t *testing.T) {
	amountIn := big.NewInt(2e18)
	pre, post := swap.WrapCalls(testWETH, swap.NativeWrap{UnwrapIn: true}, amountIn, big.NewInt(0))
	if len(pre) != 1 {
		t.Fatalf("expected 1 pre call, got %d", len(pre))
	}
	if len(post) != 0 {
		t.Errorf("expected 0 post calls, got %d", len(post))
	}
	call := pre[0]
	if !strings.HasPrefix(call.Data, "0x2e1a7d4d") {
		t.Errorf("expected withdraw selector 0x2e1a7d4d, got %s", call.Data)
	}
	if call.Value.Sign() != 0 {
		t.Errorf("withdraw call Value should be 0, got %s", call.Value)
	}
	// 2e18 = 0x1BC16D674EC80000
	if !strings.Contains(strings.ToLower(call.Data), "1bc16d674ec80000") {
		t.Errorf("amount 2e18 not found in withdraw calldata: %s", call.Data)
	}
}

func TestWrapCalls_UnwrapOut_PostHasWithdraw(t *testing.T) {
	amountOutMin := big.NewInt(9e17)
	pre, post := swap.WrapCalls(testWETH, swap.NativeWrap{UnwrapOut: true}, big.NewInt(1e18), amountOutMin)
	if len(pre) != 0 {
		t.Errorf("expected 0 pre calls, got %d", len(pre))
	}
	if len(post) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(post))
	}
	call := post[0]
	if !strings.HasPrefix(call.Data, "0x2e1a7d4d") {
		t.Errorf("expected withdraw selector, got %s", call.Data)
	}
	if call.Value.Sign() != 0 {
		t.Errorf("withdraw call Value should be 0, got %s", call.Value)
	}
}

func TestWrapCalls_WrapOut_PostHasDeposit(t *testing.T) {
	amountOutMin := big.NewInt(5e17)
	pre, post := swap.WrapCalls(testWETH, swap.NativeWrap{WrapOut: true}, big.NewInt(1e18), amountOutMin)
	if len(pre) != 0 {
		t.Errorf("expected 0 pre calls, got %d", len(pre))
	}
	if len(post) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(post))
	}
	call := post[0]
	if call.Data != "0xd0e30db0" {
		t.Errorf("expected deposit selector, got %s", call.Data)
	}
	if call.Value.Cmp(amountOutMin) != 0 {
		t.Errorf("post call Value = %s, want %s", call.Value, amountOutMin)
	}
}

func TestWrapCalls_WrapInAndUnwrapOut(t *testing.T) {
	pre, post := swap.WrapCalls(testWETH, swap.NativeWrap{WrapIn: true, UnwrapOut: true}, big.NewInt(1e18), big.NewInt(9e17))
	if len(pre) != 1 {
		t.Errorf("expected 1 pre call, got %d", len(pre))
	}
	if len(post) != 1 {
		t.Errorf("expected 1 post call, got %d", len(post))
	}
	if pre[0].Data != "0xd0e30db0" {
		t.Errorf("pre should be deposit, got %s", pre[0].Data)
	}
	if !strings.HasPrefix(post[0].Data, "0x2e1a7d4d") {
		t.Errorf("post should be withdraw, got %s", post[0].Data)
	}
}

func TestWrapCalls_WithdrawEncodesAmount(t *testing.T) {
	amount := big.NewInt(1_000_000)
	pre, _ := swap.WrapCalls(testWETH, swap.NativeWrap{UnwrapIn: true}, amount, big.NewInt(0))
	if len(pre) != 1 {
		t.Fatalf("expected 1 pre call, got %d", len(pre))
	}
	data := strings.ToLower(pre[0].Data)
	// selector: 4 bytes = 8 hex chars; then 32 bytes = 64 hex chars; total 74 hex chars + "0x"
	if len(data) != 2+8+64 {
		t.Errorf("withdraw calldata length = %d, want %d", len(data), 2+8+64)
	}
	// 1_000_000 = 0xF4240; 32-byte big-endian tail is ...000f4240; last 7 hex chars = "00f4240"
	if !strings.HasSuffix(data, "00f4240") {
		t.Errorf("amount not correctly encoded at end of calldata: %s", data)
	}
}

func TestResolveWrapCalls_AllFalse_EmptyResult(t *testing.T) {
	pre, post, err := swap.ResolveWrapCalls("ethereum", swap.NativeWrap{}, "1000000000000000000", "1900000000", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pre) != 0 || len(post) != 0 {
		t.Errorf("expected empty pre/post, got pre=%d post=%d", len(pre), len(post))
	}
}

func TestResolveWrapCalls_WrapIn_PreHasDeposit(t *testing.T) {
	pre, post, err := swap.ResolveWrapCalls("ethereum", swap.NativeWrap{WrapIn: true}, "1000000000000000000", "1900000000", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pre) != 1 {
		t.Fatalf("expected 1 pre call, got %d", len(pre))
	}
	if len(post) != 0 {
		t.Errorf("expected 0 post calls, got %d", len(post))
	}
	if !strings.EqualFold(pre[0].To, "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2") {
		t.Errorf("expected ETH WETH address, got %s", pre[0].To)
	}
	if pre[0].Data != "0xd0e30db0" {
		t.Errorf("expected deposit selector, got %s", pre[0].Data)
	}
	expected := new(big.Int)
	expected.SetString("1000000000000000000", 10)
	if pre[0].Value.Cmp(expected) != 0 {
		t.Errorf("expected Value %s, got %s", expected, pre[0].Value)
	}
}

func TestResolveWrapCalls_UnwrapOut_PostUsesAmountOutMin(t *testing.T) {
	// OutputAmount 1900000000, slippage 50 BPS → amountOutMin = 1900000000 * 9950 / 10000 = 1890500000
	pre, post, err := swap.ResolveWrapCalls("ethereum", swap.NativeWrap{UnwrapOut: true}, "1000000000000000000", "1900000000", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pre) != 0 {
		t.Errorf("expected 0 pre, got %d", len(pre))
	}
	if len(post) != 1 {
		t.Fatalf("expected 1 post, got %d", len(post))
	}
	if !strings.HasPrefix(post[0].Data, "0x2e1a7d4d") {
		t.Errorf("expected withdraw selector, got %s", post[0].Data)
	}
	// amountOutMin = 1890500000 = 0x70AEBDA0; should appear in the latter part of calldata
	if !strings.Contains(strings.ToLower(post[0].Data), "70aebda0") {
		t.Errorf("amountOutMin 1890500000 (0x70AEBDA0) not found in withdraw calldata: %s", post[0].Data)
	}
}

func TestResolveWrapCalls_UnknownChain_ReturnsError(t *testing.T) {
	_, _, err := swap.ResolveWrapCalls("solana", swap.NativeWrap{WrapIn: true}, "1000000", "1000000", 50)
	if err == nil {
		t.Error("expected error for unknown chain, got nil")
	}
}

func TestResolveWrapCalls_InvalidInputAmount_ReturnsError(t *testing.T) {
	_, _, err := swap.ResolveWrapCalls("ethereum", swap.NativeWrap{WrapIn: true}, "not-a-number", "1000000", 50)
	if err == nil {
		t.Error("expected error for invalid InputAmount, got nil")
	}
}

func TestResolveWrapCalls_ChainCaseInsensitive(t *testing.T) {
	pre, _, err := swap.ResolveWrapCalls("Ethereum", swap.NativeWrap{WrapIn: true}, "1000000000000000000", "1900000000", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pre) != 1 {
		t.Fatalf("expected 1 pre call, got %d", len(pre))
	}
}

// composeAndBundle simulates the handler's composition logic: ResolveWrapCalls → assemble calls → BuildExecuteBundle
func composeAndBundle(wallet string, quote *swap.QuoteResult, nw swap.NativeWrap, slippageBPS int, build *swap.BuildResult) (swap.SwapBundle, error) {
	wrapPre, wrapPost, err := swap.ResolveWrapCalls("ethereum", nw, quote.InputAmount, quote.OutputAmount, slippageBPS)
	if err != nil {
		return swap.SwapBundle{}, err
	}
	swapValue := new(big.Int)
	if build.Value != "" && build.Value != "0x" && build.Value != "0x0" {
		swapValue.SetString(strings.TrimPrefix(build.Value, "0x"), 16)
	}
	var calls []erc7821.HexCall
	calls = append(calls, wrapPre...)
	calls = append(calls, erc7821.HexCall{To: build.To, Value: swapValue, Data: build.Data})
	calls = append(calls, wrapPost...)
	return swap.BuildExecuteBundle(wallet, calls, build.GasLimit), nil
}

func TestNativeWrapFlow_QuoteToBundle_WrapInProducesDeposit(t *testing.T) {
	quote := &swap.QuoteResult{
		Protocol:     "test",
		SourceAsset:  "native",
		TargetAsset:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		InputAmount:  "1000000000000000000",
		OutputAmount: "1900000000",
	}
	nw := swap.NativeWrap{WrapIn: true}
	build := &swap.BuildResult{
		To:    "0x1111111111111111111111111111111111111111",
		Value: "0x0",
		Data:  "0xfeedface",
	}
	bundle, err := composeAndBundle("0x2222222222222222222222222222222222222222", quote, nw, 50, build)
	if err != nil {
		t.Fatalf("composeAndBundle: %v", err)
	}
	low := strings.ToLower(bundle.Data)
	if !strings.Contains(low, "d0e30db0") {
		t.Errorf("expected WETH.deposit selector in bundle, not found")
	}
	if !strings.Contains(low, "feedface") {
		t.Errorf("expected swap data in bundle, not found")
	}
	// total value = 1e18 (from deposit), swap.Value = 0
	if bundle.Value != "0xde0b6b3a7640000" {
		t.Errorf("expected bundle value 0xde0b6b3a7640000 (1e18), got %s", bundle.Value)
	}
}

func TestNativeWrapFlow_QuoteToBundle_UnwrapOutProducesWithdraw(t *testing.T) {
	quote := &swap.QuoteResult{
		Protocol:     "test",
		SourceAsset:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		TargetAsset:  "native",
		InputAmount:  "1000000",
		OutputAmount: "500000000000000000",
	}
	nw := swap.NativeWrap{UnwrapOut: true}
	build := &swap.BuildResult{
		To:    "0x1111111111111111111111111111111111111111",
		Value: "0x0",
		Data:  "0xfeedface",
	}
	bundle, err := composeAndBundle("0x2222222222222222222222222222222222222222", quote, nw, 50, build)
	if err != nil {
		t.Fatalf("composeAndBundle: %v", err)
	}
	low := strings.ToLower(bundle.Data)
	if !strings.Contains(low, "2e1a7d4d") {
		t.Errorf("expected WETH.withdraw selector in bundle, not found")
	}
	if !strings.Contains(low, "feedface") {
		t.Errorf("expected swap data in bundle, not found")
	}
	// withdraw value itself = 0, swap.Value = 0 → total = 0
	if bundle.Value != "0x0" {
		t.Errorf("expected bundle value 0x0, got %s", bundle.Value)
	}
}

func TestNativeWrapFlow_QuoteToBundle_NoWrap_BehaviorUnchanged(t *testing.T) {
	quote := &swap.QuoteResult{
		Protocol:     "test",
		SourceAsset:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		TargetAsset:  "0xdAC17F958D2ee523a2206206994597C13D831ec7",
		InputAmount:  "1000000",
		OutputAmount: "1000000",
	}
	nw := swap.NativeWrap{} // all false
	build := &swap.BuildResult{
		To:    "0x1111111111111111111111111111111111111111",
		Value: "0x0",
		Data:  "0xfeedface",
	}
	bundle, err := composeAndBundle("0x2222222222222222222222222222222222222222", quote, nw, 50, build)
	if err != nil {
		t.Fatalf("composeAndBundle: %v", err)
	}
	low := strings.ToLower(bundle.Data)
	if strings.Contains(low, "d0e30db0") || strings.Contains(low, "2e1a7d4d") {
		t.Errorf("zero-value NativeWrap should not produce deposit/withdraw selectors in bundle")
	}
	if !strings.Contains(low, "feedface") {
		t.Errorf("swap data missing in bundle")
	}
}

func TestDeriveNativeWrap_BothSidesDualSupport_AllFalse(t *testing.T) {
	// Protocol supports native+WETH on both sides (as all current adapters do); no conversion needed regardless of user input
	nw := swap.DeriveNativeWrap(true, true, swap.NativeCapability{
		AcceptsNativeIn: true, AcceptsWETHIn: true,
		OutputsNative: true, OutputsWETH: true,
	})
	if nw != (swap.NativeWrap{}) {
		t.Errorf("expected zero value, got %+v", nw)
	}
	nw2 := swap.DeriveNativeWrap(false, false, swap.NativeCapability{
		AcceptsNativeIn: true, AcceptsWETHIn: true,
		OutputsNative: true, OutputsWETH: true,
	})
	if nw2 != (swap.NativeWrap{}) {
		t.Errorf("expected zero value, got %+v", nw2)
	}
}

func TestDeriveNativeWrap_WethOnly_NativeRequest_WrapsBothSides(t *testing.T) {
	nw := swap.DeriveNativeWrap(true, true, swap.NativeCapability{
		AcceptsNativeIn: false, AcceptsWETHIn: true,
		OutputsNative: false, OutputsWETH: true,
	})
	expected := swap.NativeWrap{WrapIn: true, UnwrapOut: true}
	if nw != expected {
		t.Errorf("expected %+v, got %+v", expected, nw)
	}
}

func TestDeriveNativeWrap_WethOnly_WethRequest_NoWrap(t *testing.T) {
	nw := swap.DeriveNativeWrap(false, false, swap.NativeCapability{
		AcceptsNativeIn: false, AcceptsWETHIn: true,
		OutputsNative: false, OutputsWETH: true,
	})
	if nw != (swap.NativeWrap{}) {
		t.Errorf("expected zero value, got %+v", nw)
	}
}

func TestDeriveNativeWrap_NativeOnly_WethRequest_UnwrapsBothSides(t *testing.T) {
	nw := swap.DeriveNativeWrap(false, false, swap.NativeCapability{
		AcceptsNativeIn: true, AcceptsWETHIn: false,
		OutputsNative: true, OutputsWETH: false,
	})
	expected := swap.NativeWrap{UnwrapIn: true, WrapOut: true}
	if nw != expected {
		t.Errorf("expected %+v, got %+v", expected, nw)
	}
}

func TestDeriveNativeWrap_MixedSupport_OnlyInputNeedsWrap(t *testing.T) {
	// Protocol input accepts only WETH, output is dual → user provides native and wants native: only WrapIn
	nw := swap.DeriveNativeWrap(true, true, swap.NativeCapability{
		AcceptsNativeIn: false, AcceptsWETHIn: true,
		OutputsNative: true, OutputsWETH: true,
	})
	expected := swap.NativeWrap{WrapIn: true}
	if nw != expected {
		t.Errorf("expected %+v, got %+v", expected, nw)
	}
}

func TestDeriveNativeWrap_NativeIn_WethOut_NeedsWrapBoth(t *testing.T) {
	// Protocol accepts only native input and outputs only WETH; user provides WETH and wants native → UnwrapIn + UnwrapOut
	nw := swap.DeriveNativeWrap(false, true, swap.NativeCapability{
		AcceptsNativeIn: true, AcceptsWETHIn: false,
		OutputsNative: false, OutputsWETH: true,
	})
	expected := swap.NativeWrap{UnwrapIn: true, UnwrapOut: true}
	if nw != expected {
		t.Errorf("expected %+v, got %+v", expected, nw)
	}
}
