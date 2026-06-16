// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

func TestRPC_DebugTraceCall_Live(t *testing.T) {
	if !checkIntegrationEnabledFromDotenv(t) {
		return
	}
	rpcURLs := configuredRPCURLs()
	for _, chain := range []struct {
		name    string
		chainID int
	}{
		{name: "ethereum", chainID: 1},
		{name: "bsc", chainID: 56},
		{name: "polygon", chainID: 137},
		{name: "base", chainID: 8453},
	} {
		chain := chain
		t.Run(chain.name, func(t *testing.T) {
			rpcURL := rpcURLs[chain.name]
			if rpcURL == "" {
				t.Skipf("RPC URL not configured for %s", chain.name)
			}
			sim := security.NewRPCSimulator(map[int]string{chain.chainID: rpcURL}, nil, nil)
			ctx := integrationCtx(t)
			result, _, err := sim.Simulate(ctx, security.CheckRequest{
				ChainID:  chain.chainID,
				Chain:    chain.name,
				Owner:    "0x000000000000000000000000000000000000dEaD",
				To:       "0x000000000000000000000000000000000000dEaD",
				Value:    "0",
				Data:     "0x",
				GasLimit: 21000,
				GasPrice: "0",
			})
			if err != nil {
				t.Fatalf("debug_traceCall: %v", err)
			}
			if result == nil || result.Data == nil || !result.Data.Status {
				t.Fatalf("simulation failed: %#v", result)
			}
		})
	}
}
