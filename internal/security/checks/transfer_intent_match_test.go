// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

const (
	testOwner       = "0x1111111111111111111111111111111111111111"
	testBob         = "0x2222222222222222222222222222222222222222"
	testDead        = "0x000000000000000000000000000000000000dead"
	testUSDC        = "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	testUSDCImpl    = "0x43506849d7c04f9138d1a2050bbf3a0c054402dd" // USDC proxy implementation
	testRelayer     = "0x3333333333333333333333333333333333333333"
	testMMDelegator = "0x63c0c19a282a1b52b07dd5a65b58948a07dae32b"
	testAttacker    = "0x9999999999999999999999999999999999999999"
)

// nativeTransferTrace builds a minimal trace: an EOA-level CALL whose first
// child is a CALL delivering `weiValue` to `to`. This mirrors how the rpc
// simulator returns ERC-7821 batch native transfers — outer call into the
// 7702-delegated EOA, inner CALL to the actual recipient.
func nativeTransferTrace(to, weiValue string) *security.SimulationCall {
	return &security.SimulationCall{
		Type: "CALL", From: testOwner, To: testOwner, Value: "0",
		Calls: []security.SimulationCall{
			{Type: "CALL", From: testOwner, To: to, Value: weiValue},
		},
	}
}

// delegatedNativeTransferTrace mirrors how an EIP-7702 delegated wallet's
// transfer actually appears on-chain: an outer CALL into the EOA, then the
// EVM-enforced designator DELEGATECALL from the EOA to its installed delegate
// implementation (`delegateTo`), and finally the real value CALL to the
// recipient nested inside the delegate's execution context.
func delegatedNativeTransferTrace(delegateTo, recipient, weiValue string) *security.SimulationCall {
	return &security.SimulationCall{
		Type: "CALL", From: testRelayer, To: testOwner, Value: "0",
		Calls: []security.SimulationCall{
			{
				Type: "DELEGATECALL", From: testOwner, To: delegateTo, Value: "0",
				Calls: []security.SimulationCall{
					{Type: "CALL", From: testOwner, To: recipient, Value: weiValue},
				},
			},
		},
	}
}

func runTransferIntentMatch(t *testing.T, ctx security.SimulationContext) security.CheckResult {
	t.Helper()
	return TransferIntentMatch{}.Run(ctx)
}

func baseSimContext(summaryJSON string, trace *security.SimulationCall, balances []security.BalanceChange, fundFlow []string) security.SimulationContext {
	return security.SimulationContext{
		CalldataContext: security.CalldataContext{
			ShapeContext: security.ShapeContext{
				Req: security.CheckRequest{
					Owner:       testOwner,
					SummaryJSON: summaryJSON,
				},
			},
			Ops: nil,
		},
		Sim: &security.SimulationResult{
			Code: 0,
			Data: &security.SimulationData{
				Status:         true,
				BalanceChanges: balances,
				Trace:          trace,
			},
		},
		FundFlow: fundFlow,
	}
}

// Non-self native transfer where the BalanceChange line is gas-polluted
// (declared 0.0001 ETH, actual outflow 0.0001055 ETH because gas is paid
// in native). The rule must consult trace call.Value, not BalanceChange,
// or it false-fails with token_amount_mismatch.
func TestTransferIntentMatch_NativeNonSelfTransfer_PassesViaTrace_DespiteGasPollutedBalanceChange(t *testing.T) {
	summary := `{"token":"native","amount":"0.0001","to":"` + testDead + `"}`
	trace := nativeTransferTrace(testDead, "100000000000000") // 0.0001 ETH in wei
	// Use the zero-address native dialect (one of the canonical native
	// spellings recognised by normaliseTokenAddress) so the fixture
	// exercises the gas-pollution path: address-normalise passes, then
	// the magnitude compare (0.0001055 abs vs declared 0.0001) is what
	// the rule must handle correctly via trace call.Value.
	balances := []security.BalanceChange{{
		Account: testOwner,
		Assets: []security.BalanceChangeAsset{
			{Address: "0x0000000000000000000000000000000000000000", Type: "native", Amount: "-0.0001055"},
		},
	}}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner, testDead}))

	if result.Status != "pass" {
		t.Fatalf("expected pass (trace value is authoritative for native transfers), got status=%q detail=%q observed=%+v", result.Status, result.Detail, result.Observed)
	}
}

// T2 — pre-existing self-transfer support. Native self-transfer produces no
// BalanceChange entry at all (prestate diff nets to zero). Trace path still
// finds it. Guards against the fix accidentally regressing self-transfer.
func TestTransferIntentMatch_NativeSelfTransfer_PassesViaTrace_WhenBalanceChangeAbsent(t *testing.T) {
	summary := `{"token":"native","amount":"0.05","to":"` + testOwner + `"}`
	trace := nativeTransferTrace(testOwner, "50000000000000000") // 0.05 ETH in wei
	// Self-transfer: outflow and inflow cancel in the prestate diff, so
	// the simulator emits no native BalanceChange row at all.
	balances := []security.BalanceChange{}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner}))

	if result.Status != "pass" {
		t.Fatalf("expected pass for native self-transfer via trace, got status=%q detail=%q", result.Status, result.Detail)
	}
}

// T3 — ERC-20 transfers continue to use BalanceChange (Transfer event log,
// gas-clean). Guards against the fix mis-routing ERC-20 through the trace
// path, where call.Value would be zero for token transfers.
func TestTransferIntentMatch_ERC20Transfer_PassesViaBalanceChange(t *testing.T) {
	summary := `{"token":"` + testUSDC + `","amount":"100","to":"` + testBob + `"}`
	// ERC-20 trace: outer call to the token contract, value 0. The amount
	// lives in calldata/event, not call.Value — so trace would never
	// match. BalanceChange from Transfer event is the right source.
	trace := &security.SimulationCall{
		Type: "CALL", From: testOwner, To: testUSDC, Value: "0",
	}
	balances := []security.BalanceChange{{
		Account: testOwner,
		Assets: []security.BalanceChangeAsset{
			{Address: testUSDC, Type: "ERC20", Amount: "-100"},
		},
	}}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner, testBob}))

	if result.Status != "pass" {
		t.Fatalf("expected pass for ERC-20 transfer via BalanceChange, got status=%q detail=%q", result.Status, result.Detail)
	}
}

// Negative case: if trace shows a different amount than declared, the
// rule must fail. Guards against a degenerate implementation that
// unconditionally passes native transfers.
func TestTransferIntentMatch_NativeTransfer_FailsOnAmountMismatch(t *testing.T) {
	summary := `{"token":"native","amount":"0.0001","to":"` + testDead + `"}`
	// Declared 0.0001 ETH but trace only delivers 0.00005 ETH.
	trace := nativeTransferTrace(testDead, "50000000000000")
	balances := []security.BalanceChange{{
		Account: testOwner,
		Assets:  []security.BalanceChangeAsset{{Address: "", Type: "native", Amount: "-0.0000555"}},
	}}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner, testDead}))

	if result.Status != "fail" {
		t.Fatalf("expected fail for amount mismatch, got status=%q detail=%q", result.Status, result.Detail)
	}
	// The mismatch must be reported as token_amount_mismatch specifically
	// (not e.g. recipient_mismatch — recipient is correct here).
	findings, ok := result.Observed.([]map[string]string)
	if !ok {
		t.Fatalf("expected []map[string]string findings, got %T", result.Observed)
	}
	var sawAmountMismatch bool
	for _, f := range findings {
		if f["check"] == "token_amount_mismatch" {
			sawAmountMismatch = true
			break
		}
	}
	if !sawAmountMismatch {
		t.Fatalf("expected token_amount_mismatch finding, got %+v", findings)
	}
	// And recipient_mismatch must NOT fire — recipient is correct.
	for _, f := range findings {
		if f["check"] == "recipient_mismatch" {
			t.Fatalf("unexpected recipient_mismatch finding: %+v", f)
		}
	}
	// Sanity: detail line should mention the expected token+amount.
	if !strings.Contains(result.Detail, "transfer intent finding") {
		t.Fatalf("expected detail to summarise findings, got %q", result.Detail)
	}
}

// EIP-7702 expected behavior: a delegated wallet (MetaMask Delegator) ALWAYS
// executes through a designator DELEGATECALL into its installed implementation.
// That frame is the wallet delegating to its own code — not a red flag — so a
// transfer whose only "delegatecall" is to the trusted delegate must still pass.
func TestTransferIntentMatch_DelegateCallToTrustedDelegate_NotFlagged(t *testing.T) {
	summary := `{"token":"native","amount":"0.0001","to":"` + testDead + `"}`
	trace := delegatedNativeTransferTrace(testMMDelegator, testDead, "100000000000000")
	balances := []security.BalanceChange{{
		Account: testOwner,
		Assets: []security.BalanceChangeAsset{
			{Address: "0x0000000000000000000000000000000000000000", Type: "native", Amount: "-0.0001055"},
		},
	}}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner, testDead}))

	if result.Status != "pass" {
		t.Fatalf("expected pass (delegatecall to trusted delegate is expected EIP-7702 behavior), got status=%q detail=%q observed=%+v", result.Status, result.Detail, result.Observed)
	}
}

// A DELEGATECALL whose target is NOT the wallet's trusted delegate hands
// execution to an unknown contract — that is the real red flag the rule must
// still catch, even for an otherwise amount/recipient-matching transfer.
func TestTransferIntentMatch_DelegateCallToUntrustedTarget_Flagged(t *testing.T) {
	summary := `{"token":"native","amount":"0.0001","to":"` + testDead + `"}`
	trace := delegatedNativeTransferTrace(testAttacker, testDead, "100000000000000")
	balances := []security.BalanceChange{{
		Account: testOwner,
		Assets: []security.BalanceChangeAsset{
			{Address: "0x0000000000000000000000000000000000000000", Type: "native", Amount: "-0.0001055"},
		},
	}}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner, testDead}))

	if result.Status != "fail" {
		t.Fatalf("expected fail for delegatecall to untrusted target, got status=%q detail=%q", result.Status, result.Detail)
	}
	findings, ok := result.Observed.([]map[string]string)
	if !ok {
		t.Fatalf("expected []map[string]string findings, got %T", result.Observed)
	}
	var sawDelegateCall bool
	for _, f := range findings {
		if f["check"] == "trace_delegatecall" {
			sawDelegateCall = true
		}
	}
	if !sawDelegateCall {
		t.Fatalf("expected trace_delegatecall finding for untrusted delegate target, got %+v", findings)
	}
}

// USDC, USDT, and most upgradeable contracts are proxies: the proxy does a
// DELEGATECALL to its implementation contract. This DELEGATECALL is internal
// to the called contract (From=USDC_proxy, To=USDC_impl) — not from the
// wallet — and poses no risk to the wallet's execution context. The check
// must ignore it; only DELEGATECALLs originating from the owner matter.
func TestTransferIntentMatch_ProxyDelegateCall_NotFlagged(t *testing.T) {
	summary := `{"token":"` + testUSDC + `","amount":"100","to":"` + testBob + `"}`
	// Trace shape: owner → 7702 delegate → CALL USDC → USDC does
	// DELEGATECALL to its implementation (proxy pattern).
	trace := &security.SimulationCall{
		Type: "CALL", From: testRelayer, To: testOwner, Value: "0",
		Calls: []security.SimulationCall{
			{
				Type: "DELEGATECALL", From: testOwner, To: testMMDelegator, Value: "0",
				Calls: []security.SimulationCall{
					{
						Type: "CALL", From: testOwner, To: testUSDC, Value: "0",
						Calls: []security.SimulationCall{
							{Type: "DELEGATECALL", From: testUSDC, To: testUSDCImpl, Value: "0"},
						},
					},
				},
			},
		},
	}
	balances := []security.BalanceChange{{
		Account: testOwner,
		Assets: []security.BalanceChangeAsset{
			{Address: testUSDC, Type: "ERC20", Amount: "-100"},
		},
	}}

	result := runTransferIntentMatch(t, baseSimContext(summary, trace, balances, []string{testOwner, testBob}))

	if result.Status != "pass" {
		t.Fatalf("expected pass (proxy DELEGATECALL is internal to USDC, not from wallet), got status=%q detail=%q observed=%+v", result.Status, result.Detail, result.Observed)
	}
}
