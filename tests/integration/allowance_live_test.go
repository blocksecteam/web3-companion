// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"math/big"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

func TestFetchAllowance_KnownZeroAllowance(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	ctx := integrationCtx(t)

	allowance, err := swap.FetchAllowance(ctx, rpcURL, addrUSDCEth, addrDead, addrKyberRoute)
	if err != nil {
		t.Fatalf("FetchAllowance: %v", err)
	}
	if allowance.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("expected zero allowance for dead address, got %s", allowance)
	}
}

func TestFetchAllowance_ReturnsUint256(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	ctx := integrationCtx(t)

	allowance, err := swap.FetchAllowance(ctx, rpcURL, addrUSDCEth, addrVitalik, addrKyberRoute)
	if err != nil {
		t.Fatalf("FetchAllowance: %v", err)
	}
	if allowance == nil {
		t.Fatal("expected non-nil allowance")
	}
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if allowance.Cmp(maxUint256) > 0 {
		t.Errorf("allowance exceeds uint256 max: %s", allowance)
	}
	t.Logf("allowance: %s", allowance)
}

func TestFetchAllowance_NonERC20Token_ReturnsZero(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	ctx := integrationCtx(t)

	allowance, err := swap.FetchAllowance(ctx, rpcURL, addrDead, addrVitalik, addrKyberRoute)
	if err != nil {
		t.Logf("non-ERC20 token returned error (acceptable): %v", err)
		return
	}
	if allowance.Sign() != 0 {
		t.Errorf("expected zero allowance for non-ERC20 address, got %s", allowance)
	}
}
