// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"math/big"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/chain"
)

func TestChain_GasPrice_Live_Ethereum(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatalf("get gas price: %v", err)
	}
	if gasPrice.Sign() <= 0 {
		t.Error("gas price should be positive")
	}
	maxGas := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e9))
	if gasPrice.Cmp(maxGas) > 0 {
		t.Errorf("gas price %s exceeds 1000 gwei — suspicious", gasPrice)
	}
	t.Logf("ethereum gas: %s gwei", new(big.Int).Div(gasPrice, big.NewInt(1e9)))
}

func TestChain_GasPrice_Live_Base(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "base")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatalf("get gas price: %v", err)
	}
	if gasPrice.Sign() <= 0 {
		t.Error("gas price should be positive")
	}
	t.Logf("base gas: %s gwei", new(big.Int).Div(gasPrice, big.NewInt(1e9)))
}

func TestChain_EstimateGas_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	estimated, err := client.EstimateGas(ctx, map[string]interface{}{
		"from": addrVitalik,
		"to":   addrVitalik,
	})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if estimated < 20000 || estimated > 100000 {
		t.Errorf("suspicious native transfer estimate: %d (expected 21000-30000 range)", estimated)
	}
	t.Logf("transfer estimate: %d gas", estimated)
}

func TestChain_EstimateGas_ContractCall_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	estimated, err := client.EstimateGas(ctx, map[string]interface{}{
		"from": addrVitalik,
		"to":   addrUSDCEth,
		"data": "0xa9059cbb000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa960450000000000000000000000000000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Logf("estimate contract call (may revert if no balance): %v", err)
		return
	}
	if estimated < 21000 {
		t.Errorf("contract call estimate too low: %d", estimated)
	}
	t.Logf("contract call estimate: %d gas", estimated)
}

func TestChain_TransactionReceipt_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	// This TX may be pruned on public RPCs — test gracefully handles that.
	receipt, err := client.TransactionReceipt(ctx,
		"0xc0ec4975c8b3f2827be7fef37f4fa2480ccb7547169a2a1e03e39d268154a9e5")
	if err != nil {
		t.Logf("receipt lookup (may be pruned on public RPC): %v", err)
		return
	}
	if receipt.Status != 1 {
		t.Errorf("expected successful tx, got status %d", receipt.Status)
	}
	t.Logf("receipt: status=%d block=%d gas=%d", receipt.Status, receipt.BlockNumber, receipt.GasUsed)
}
