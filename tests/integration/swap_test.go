// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
)

func TestSwap_OdosQuote_Real(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()
	req := swap.QuoteRequest{
		Chain:       "ethereum",
		FromAddress: envOr(t, "TEST_FROM_ADDRESS", addrVitalik),
		SourceAsset: addrUSDCEth,
		TargetAsset: addrWETHEth,
		Amount:      "1000000000",
		Slippage:    0.5,
	}

	result, err := adapter.Quote(ctx, req)
	if err != nil {
		skipOnRateLimit(t, err)
		t.Fatalf("odos quote: %v", err)
	}
	validateOutputAmount(t, result.OutputAmount)
	t.Logf("odos: %s USDC → %s ETH", req.Amount, result.OutputAmount)
}

func TestSwap_ProtocolList(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	names := reg.List()

	const minProtocols = 6
	if len(names) < minProtocols {
		t.Errorf("expected at least %d protocols, got %d: %v", minProtocols, len(names), names)
	}
	t.Logf("registered protocols: %v", names)
}
