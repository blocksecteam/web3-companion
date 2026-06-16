// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/token"
)

const (
	approveToken   = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	approveSpender = "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	approveRouter  = "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af"
)

func TestGetApproveCalls_SimpleSpender_ZeroAllowance_SingleApprove(t *testing.T) {
	calls := swap.GetApproveCalls("erc20", approveToken, approveSpender, "", big.NewInt(1_000_000), big.NewInt(0))
	if len(calls) != 1 {
		t.Fatalf("expected 1 approve call, got %d", len(calls))
	}
	if !strings.HasPrefix(calls[0].Data, "0x095ea7b3") {
		t.Errorf("expected approve selector, got %s", calls[0].Data[:10])
	}
}

func TestGetApproveCalls_SimpleSpender_SufficientAllowance_NoCalls(t *testing.T) {
	calls := swap.GetApproveCalls("erc20", approveToken, approveSpender, "", big.NewInt(1_000_000), big.NewInt(5_000_000))
	if len(calls) != 0 {
		t.Errorf("expected 0 calls when allowance sufficient, got %d", len(calls))
	}
}

func TestGetApproveCalls_SimpleSpender_PartialAllowance_SafeResetTwoCalls(t *testing.T) {

	calls := swap.GetApproveCalls("erc20", approveToken, approveSpender, "", big.NewInt(5_000_000), big.NewInt(1_000_000))
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (reset + approve), got %d", len(calls))
	}

	if !strings.HasSuffix(calls[0].Data, strings.Repeat("0", 64)) {
		t.Error("first call should be approve(spender, 0)")
	}
}

func TestGetApproveCalls_Permit2Spender_ZeroAllowance_TwoCalls(t *testing.T) {

	calls := swap.GetApproveCalls("permit2", approveToken, token.Permit2Address, approveRouter,
		big.NewInt(1_000_000), big.NewInt(0))
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	if !strings.EqualFold(calls[0].To, approveToken) {
		t.Errorf("call[0].To should be token %s, got %s", approveToken, calls[0].To)
	}
	if !strings.HasPrefix(calls[0].Data, "0x095ea7b3") {
		t.Errorf("call[0] should be ERC-20 approve, got %s", calls[0].Data[:10])
	}

	if !strings.Contains(strings.ToLower(calls[0].Data), strings.ToLower(token.Permit2Address[2:])) {
		t.Errorf("call[0] approve spender should be Permit2")
	}

	if !strings.EqualFold(calls[1].To, token.Permit2Address) {
		t.Errorf("call[1].To should be Permit2, got %s", calls[1].To)
	}
	if !strings.HasPrefix(calls[1].Data, "0x87517c45") {
		t.Errorf("call[1] should be Permit2.approve, got %s", calls[1].Data[:10])
	}
}

func TestGetApproveCalls_Permit2Spender_SufficientAllowance_OnlyPermit2Approve(t *testing.T) {

	calls := swap.GetApproveCalls("permit2", approveToken, token.Permit2Address, approveRouter,
		big.NewInt(1_000_000), big.NewInt(99_999_999))
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (only Permit2.approve), got %d", len(calls))
	}
	if !strings.HasPrefix(calls[0].Data, "0x87517c45") {
		t.Errorf("should be Permit2.approve, got %s", calls[0].Data[:10])
	}
}

func TestGetApproveCalls_Permit2Spender_PartialAllowance_ThreeCalls(t *testing.T) {

	calls := swap.GetApproveCalls("permit2", approveToken, token.Permit2Address, approveRouter,
		big.NewInt(5_000_000), big.NewInt(1_000_000))
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if !strings.HasSuffix(calls[0].Data, strings.Repeat("0", 64)) {
		t.Error("call[0] should be approve(Permit2, 0) reset")
	}
	if !strings.HasPrefix(calls[1].Data, "0x095ea7b3") {
		t.Errorf("call[1] should be ERC-20 approve(Permit2, amount)")
	}
	if !strings.HasPrefix(calls[2].Data, "0x87517c45") {
		t.Errorf("call[2] should be Permit2.approve(router)")
	}
}

func TestGetApproveCalls_EmptyStyle_DefaultsToERC20(t *testing.T) {
	// style="" falls into the default case → standard ERC-20 approve.
	// Spender address must NOT auto-promote to Permit2 — style is the only
	// signal that selects the Permit2 path.
	calls := swap.GetApproveCalls("", approveToken, token.Permit2Address, "",
		big.NewInt(1_000_000), big.NewInt(0))

	if len(calls) == 0 {
		t.Error("expected at least one approve call")
	}

	for _, c := range calls {
		if strings.HasPrefix(c.Data, "0x87517c45") {
			t.Error("should not have Permit2.approve when router is empty")
		}
	}
}

func TestGetApproveCalls_EmptyStyle_DoesNotAutoPromoteToPermit2(t *testing.T) {
	// Regression guard: an earlier fallback path auto-promoted style="" to permit2
	// whenever spender == Permit2Address && router != "". That auto-detection is gone;
	// any unknown/empty style must now default to erc20. Do not re-introduce the
	// promotion — adapters must declare ApproveStyle explicitly.
	calls := swap.GetApproveCalls("", approveToken, token.Permit2Address, approveRouter,
		big.NewInt(1_000_000), big.NewInt(0))

	for _, c := range calls {
		if strings.HasPrefix(c.Data, "0x87517c45") {
			t.Error("empty style must not auto-promote to permit2 even when spender=Permit2 + router!=\"\"")
		}
	}
}
