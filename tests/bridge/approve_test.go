// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"math/big"
	"strings"
	"testing"

	bridgeapi "github.com/blocksecteam/web3-companion/internal/bridge"
	"github.com/blocksecteam/web3-companion/internal/token"
)

// permit2-style approve must produce two calls when allowance starts at zero:
//  1. token.approve(Permit2, amount)
//  2. Permit2.approve(token, router, amount, deadline)
//
// The Permit2 call uses selector 0x87517c45.
func TestComputeApproveSequence_Permit2_ZeroAllowance_TwoCalls(t *testing.T) {
	owner := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	tokenAddr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" // USDC
	router := "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae"

	calls := bridgeapi.ComputeApproveSequence(
		context.Background(),
		"", // no RPC → falls back to assumed zero allowance
		owner,
		tokenAddr,
		token.Permit2Address,
		router,
		"permit2",
		big.NewInt(1_000_000),
	)
	if len(calls) != 2 {
		t.Fatalf("permit2 calls = %d, want 2 (token.approve + Permit2.approve)", len(calls))
	}
	if !strings.EqualFold(calls[0].To, tokenAddr) {
		t.Errorf("call[0].To = %s, want token contract %s", calls[0].To, tokenAddr)
	}
	if !strings.HasPrefix(calls[0].Data, "0x095ea7b3") {
		t.Errorf("call[0] should be ERC-20 approve selector, got %s", calls[0].Data[:10])
	}
	if !strings.Contains(strings.ToLower(calls[0].Data), strings.ToLower(token.Permit2Address[2:])) {
		t.Errorf("call[0] spender should be Permit2 address; data = %s", calls[0].Data)
	}
	if !strings.EqualFold(calls[1].To, token.Permit2Address) {
		t.Errorf("call[1].To = %s, want Permit2 contract %s", calls[1].To, token.Permit2Address)
	}
	if !strings.HasPrefix(calls[1].Data, "0x87517c45") {
		t.Errorf("call[1] should be Permit2.approve selector, got %s", calls[1].Data[:10])
	}
}

// ERC-20 default-path approve sequence must produce the standard
// approve(spender, amount) call.
func TestComputeApproveSequence_ERC20_DefaultPath(t *testing.T) {
	owner := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	tokenAddr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	spender := "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae"

	calls := bridgeapi.ComputeApproveSequence(
		context.Background(),
		"",
		owner,
		tokenAddr,
		spender,
		"", // router unused for erc20
		"erc20",
		big.NewInt(1_000_000),
	)
	if len(calls) != 1 {
		t.Fatalf("erc20 zero-allowance calls = %d, want 1", len(calls))
	}
	if !strings.HasPrefix(calls[0].Data, "0x095ea7b3") {
		t.Errorf("erc20 path should emit ERC-20 approve selector, got %s", calls[0].Data[:10])
	}
}
