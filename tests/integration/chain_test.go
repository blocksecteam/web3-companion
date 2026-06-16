// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/chain"
)

func TestChain_GasPrice_Real(t *testing.T) {
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
		t.Error("invalid gas price")
	}
	t.Logf("gas price: %s wei", gasPrice)
}

func TestChain_EstimateGas_Real(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	estimated, err := client.EstimateGas(ctx, map[string]interface{}{
		"from":  addrVitalik,
		"to":    addrVitalik,
		"value": "0",
	})
	if err != nil {
		t.Fatalf("estimate gas: %v", err)
	}
	if estimated < 21000 {
		t.Errorf("gas estimate too low: %d", estimated)
	}
	t.Logf("estimated gas: %d", estimated)
}

func TestChain_TransactionReceipt_Real(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURL := rpcURLForChain(t, "ethereum")
	client := chain.NewRPCClientForTest(rpcURL)
	ctx := integrationCtx(t)

	// TX may be pruned on public RPCs; graceful skip.
	receipt, err := client.TransactionReceipt(ctx,
		"0xc0ec4975c8b3f2827be7fef37f4fa2480ccb7547169a2a1e03e39d268154a9e5")
	if err != nil {
		t.Logf("receipt lookup error (may be pruned): %v", err)
		return
	}
	if receipt == nil {
		t.Error("nil receipt")
	} else {
		t.Logf("receipt status: %d, block: %d", receipt.Status, receipt.BlockNumber)
	}
}
