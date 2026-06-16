// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/token"
)

const permit2Addr = "0x000000000022D473030F116dDEE9F6B43aC78BA3"

func TestEncodePermit2Approve_Selector(t *testing.T) {
	data := token.EncodePermit2Approve(
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		big.NewInt(1_000_000),
		1_000_000_000,
	)
	if !strings.HasPrefix(data, "0x87517c45") {
		t.Errorf("expected Permit2.approve selector 0x87517c45, got %s", data[:10])
	}
}

func TestEncodePermit2Approve_Length(t *testing.T) {

	data := token.EncodePermit2Approve(
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		big.NewInt(1_000_000),
		1_000_000_000,
	)
	if len(data) != 2+8+64+64+64+64 {
		t.Errorf("expected length %d, got %d", 2+8+64+64+64+64, len(data))
	}
}

func TestBuildPermit2ApproveSequence_ZeroAllowance_ReturnsTwoCalls(t *testing.T) {
	tokenAddr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	router := "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af"
	amount := big.NewInt(1_000_000)
	currentPermit2Allowance := big.NewInt(0)

	calls := token.BuildPermit2ApproveSequence(token.Permit2Address, tokenAddr, router, amount, currentPermit2Allowance)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (approve Permit2, Permit2.approve router), got %d", len(calls))
	}

	if !strings.EqualFold(calls[0].To, tokenAddr) {
		t.Errorf("call[0].To should be token, got %s", calls[0].To)
	}
	if !strings.HasPrefix(calls[0].Data, "0x095ea7b3") {
		t.Errorf("call[0] should be ERC-20 approve, got %s", calls[0].Data[:10])
	}
	if !strings.Contains(strings.ToLower(calls[0].Data), strings.ToLower(permit2Addr[2:])) {
		t.Errorf("call[0] spender should be Permit2, data: %s", calls[0].Data)
	}

	if !strings.EqualFold(calls[1].To, permit2Addr) {
		t.Errorf("call[1].To should be Permit2, got %s", calls[1].To)
	}
	if !strings.HasPrefix(calls[1].Data, "0x87517c45") {
		t.Errorf("call[1] should be Permit2.approve, got %s", calls[1].Data[:10])
	}
}

func TestBuildPermit2ApproveSequence_SufficientPermit2Allowance_OnlyPermit2Approve(t *testing.T) {

	tokenAddr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	router := "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af"
	amount := big.NewInt(1_000_000)
	currentPermit2Allowance := new(big.Int).Mul(amount, big.NewInt(10))

	calls := token.BuildPermit2ApproveSequence(token.Permit2Address, tokenAddr, router, amount, currentPermit2Allowance)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (only Permit2.approve router), got %d", len(calls))
	}
	if !strings.EqualFold(calls[0].To, permit2Addr) {
		t.Errorf("single call should target Permit2, got %s", calls[0].To)
	}
	if !strings.HasPrefix(calls[0].Data, "0x87517c45") {
		t.Errorf("single call should be Permit2.approve selector, got %s", calls[0].Data[:10])
	}
}

func TestBuildPermit2ApproveSequence_PartialAllowance_SafeResetThenPermit2Approve(t *testing.T) {

	tokenAddr := "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	router := "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af"
	amount := big.NewInt(5_000_000)
	currentPermit2Allowance := big.NewInt(1_000_000)

	calls := token.BuildPermit2ApproveSequence(token.Permit2Address, tokenAddr, router, amount, currentPermit2Allowance)
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls (reset, approve Permit2, Permit2.approve), got %d", len(calls))
	}

	if !strings.HasSuffix(calls[0].Data, strings.Repeat("0", 64)) {
		t.Errorf("call[0] should be approve(Permit2, 0)")
	}

	if !strings.HasPrefix(calls[1].Data, "0x095ea7b3") {
		t.Errorf("call[1] should be ERC-20 approve")
	}

	if !strings.HasPrefix(calls[2].Data, "0x87517c45") {
		t.Errorf("call[2] should be Permit2.approve")
	}
}

// Regression: EncodePermit2Approve must panic (not silently corrupt) when
// amount is negative. Before hardening, fmt.Sprintf("%064x", negative) emitted
// "-..." into the calldata.
func TestEncodePermit2Approve_NegativeAmount_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative amount, got none")
		}
	}()
	token.EncodePermit2Approve(
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		big.NewInt(-1),
		1_000_000_000,
	)
}

// Regression: EncodePermit2Approve must panic when amount overflows uint160
// (the on-chain Permit2 amount type). Picks 2^160 exactly — just outside the
// representable range.
func TestEncodePermit2Approve_AmountOverflowsUint160_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for amount > 2^160-1, got none")
		}
	}()
	overflow := new(big.Int).Lsh(big.NewInt(1), 160) // 2^160
	token.EncodePermit2Approve(
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		overflow,
		1_000_000_000,
	)
}

// Regression: EncodePermit2Approve must panic when expiration overflows uint48.
func TestEncodePermit2Approve_ExpirationOverflowsUint48_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for expiration >= 2^48, got none")
		}
	}()
	token.EncodePermit2Approve(
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		big.NewInt(1),
		1<<48, // exactly 2^48 — first unrepresentable value
	)
}
