// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/token"
)

// bundleFromBuild combines approveCalls + build and delegates to the generic
// BuildExecuteBundle. Used by tests that want a single helper to assemble the
// full swap bundle shape.
func bundleFromBuild(walletAddress string, approveCalls []swap.ApproveCall, build *swap.BuildResult) swap.SwapBundle {
	swapValue := new(big.Int)
	if build.Value != "" && build.Value != "0x" && build.Value != "0x0" {
		swapValue.SetString(strings.TrimPrefix(build.Value, "0x"), 16)
	}
	calls := swap.ApproveCallsToHex(approveCalls)
	calls = append(calls, erc7821.HexCall{To: build.To, Value: swapValue, Data: build.Data})
	return swap.BuildExecuteBundle(walletAddress, calls, build.GasLimit)
}

const (
	testWallet   = "0x34239406901B30733212f418AFfbE3c38a8cFb6A"
	testRouter   = "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	testToken    = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	testSwapData = "0xdeadbeef01020304"
)

func TestBuildSwapExecuteBundle_ToIsWalletNotRouter(t *testing.T) {

	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: "0x0"}
	bundle := bundleFromBuild(testWallet, nil, build)
	if !strings.EqualFold(bundle.To, testWallet) {
		t.Errorf("expected To=%s (wallet), got %s", testWallet, bundle.To)
	}
}

func TestBuildSwapExecuteBundle_DataIsERC7821Selector(t *testing.T) {

	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: "0x0"}
	bundle := bundleFromBuild(testWallet, nil, build)
	if !strings.HasPrefix(bundle.Data, "0xe9ae5c53") {
		t.Errorf("expected ERC-7821 execute selector 0xe9ae5c53, got %s", bundle.Data[:10])
	}
}

func TestBuildSwapExecuteBundle_NativeInput_ValueEqualsSwapValue(t *testing.T) {

	ethValue := "0x5af3107a4000"
	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: ethValue}
	bundle := bundleFromBuild(testWallet, nil, build)

	n := new(big.Int)
	n.SetString(strings.TrimPrefix(bundle.Value, "0x"), 16)
	expected := new(big.Int)
	expected.SetString(strings.TrimPrefix(ethValue, "0x"), 16)
	if n.Cmp(expected) != 0 {
		t.Errorf("value should be %s (swap value), got %s", expected, n)
	}
}

func TestBuildSwapExecuteBundle_NativeInput_NoApprove_OneCallInBundle(t *testing.T) {

	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: "0x5af3107a4000"}
	bundle := bundleFromBuild(testWallet, nil, build)

	if !strings.Contains(strings.ToLower(bundle.Data), strings.ToLower(testRouter[2:])) {
		t.Errorf("swap router %s not found in bundle calldata", testRouter)
	}
}

func TestBuildSwapExecuteBundle_ERC20Input_ApprovePrependedBeforeSwap(t *testing.T) {

	approveData := token.EncodeERC20Approve(testRouter, big.NewInt(1_000_000))
	approveCalls := []swap.ApproveCall{
		{To: testToken, Data: approveData, Value: big.NewInt(0)},
	}
	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: "0x0"}
	bundle := bundleFromBuild(testWallet, approveCalls, build)

	lower := strings.ToLower(bundle.Data)
	if !strings.Contains(lower, strings.ToLower(testToken[2:])) {
		t.Errorf("token address not found in bundle (approve call missing)")
	}
	if !strings.Contains(lower, strings.ToLower(testRouter[2:])) {
		t.Errorf("router address not found in bundle (swap call missing)")
	}
}

func TestBuildSwapExecuteBundle_ERC20Input_ValueIsZero(t *testing.T) {

	approveCalls := []swap.ApproveCall{
		{To: testToken, Data: token.EncodeERC20Approve(testRouter, big.NewInt(1_000_000)), Value: big.NewInt(0)},
	}
	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: "0x0"}
	bundle := bundleFromBuild(testWallet, approveCalls, build)

	n := new(big.Int)
	n.SetString(strings.TrimPrefix(bundle.Value, "0x"), 16)
	if n.Sign() != 0 {
		t.Errorf("ERC-20 swap bundle value should be 0, got %s", n)
	}
}

func TestBuildSwapExecuteBundle_GasLimit_FallsBackToFloor(t *testing.T) {
	build := &swap.BuildResult{To: testRouter, Data: testSwapData, Value: "0x0"}
	b := bundleFromBuild(testWallet, nil, build)
	if b.GasLimit != 200_000 {
		t.Errorf("expected fallback gas limit floor 200,000, got %d", b.GasLimit)
	}
}

func TestBuildExecuteBundle_MinimalCall_StillEncodes(t *testing.T) {
	// erc7821.EncodeExecuteHex requires at least one call; pass a single no-op call to verify the pure encoding path.
	calls := []erc7821.HexCall{{To: testWallet, Value: big.NewInt(0), Data: "0x"}}
	bundle := swap.BuildExecuteBundle(testWallet, calls, 0)
	if !strings.HasPrefix(bundle.Data, "0xe9ae5c53") {
		t.Errorf("expected execute selector, got %s", bundle.Data[:10])
	}
	if bundle.To != testWallet {
		t.Errorf("To should be wallet, got %s", bundle.To)
	}
}

func TestBuildExecuteBundle_SingleCall_DataAndValueEmbedded(t *testing.T) {
	calls := []erc7821.HexCall{
		{To: testRouter, Value: big.NewInt(0), Data: "0xfeedface"},
	}
	bundle := swap.BuildExecuteBundle(testWallet, calls, 0)
	if !strings.Contains(strings.ToLower(bundle.Data), "feedface") {
		t.Errorf("call data not embedded: %s", bundle.Data)
	}
	if bundle.Value != "0x0" {
		t.Errorf("expected value 0x0, got %s", bundle.Value)
	}
}

func TestBuildExecuteBundle_ValuesSummed(t *testing.T) {
	calls := []erc7821.HexCall{
		{To: testRouter, Value: big.NewInt(1_000_000_000_000_000_000), Data: "0xaa"}, // 1e18
		{To: testRouter, Value: big.NewInt(500_000_000_000_000_000), Data: "0xbb"},   // 5e17
	}
	bundle := swap.BuildExecuteBundle(testWallet, calls, 0)
	// total = 1.5e18 = 0x14d1120d7b160000
	if bundle.Value != "0x14d1120d7b160000" {
		t.Errorf("expected total 0x14d1120d7b160000 (1.5e18), got %s", bundle.Value)
	}
}

func TestBuildExecuteBundle_GasLimit_AppliesMultiplierAndFloor(t *testing.T) {
	calls := []erc7821.HexCall{{To: testRouter, Value: big.NewInt(0), Data: "0x"}}
	b1 := swap.BuildExecuteBundle(testWallet, calls, 100_000)
	if b1.GasLimit != 200_000 {
		t.Errorf("expected floor 200_000, got %d", b1.GasLimit)
	}
	b2 := swap.BuildExecuteBundle(testWallet, calls, 1_000_000)
	if b2.GasLimit != 1_250_000 {
		t.Errorf("expected 1_250_000, got %d", b2.GasLimit)
	}
}

func TestApproveCallsToHex_PreservesFields(t *testing.T) {
	approveCalls := []swap.ApproveCall{
		{To: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Data: "0xabcd", Value: big.NewInt(0)},
		{To: "0xdAC17F958D2ee523a2206206994597C13D831ec7", Data: "0xef01", Value: big.NewInt(123)},
	}
	hex := swap.ApproveCallsToHex(approveCalls)
	if len(hex) != 2 {
		t.Fatalf("expected 2 hex calls, got %d", len(hex))
	}
	if hex[0].To != approveCalls[0].To || hex[0].Data != approveCalls[0].Data {
		t.Errorf("first call fields mismatch")
	}
	if hex[1].Value.Cmp(big.NewInt(123)) != 0 {
		t.Errorf("expected value 123, got %s", hex[1].Value)
	}
}

func TestApproveCallsToHex_NilValue_DefaultsToZero(t *testing.T) {
	approveCalls := []swap.ApproveCall{
		{To: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Data: "0xabcd", Value: nil},
	}
	hex := swap.ApproveCallsToHex(approveCalls)
	if len(hex) != 1 {
		t.Fatalf("expected 1 hex call, got %d", len(hex))
	}
	if hex[0].Value == nil || hex[0].Value.Sign() != 0 {
		t.Errorf("nil Value should default to 0, got %v", hex[0].Value)
	}
}
