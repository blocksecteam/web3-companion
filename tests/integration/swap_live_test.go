// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
)

func skipOnRateLimit(t *testing.T, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "Rate limit")) {
		t.Skipf("external API rate limited: %v", err)
	}
}

func validateOutputAmount(t *testing.T, output string) {
	t.Helper()
	if output == "" {
		t.Fatal("output amount is empty")
	}
	amt, ok := new(big.Int).SetString(output, 10)
	if !ok {
		t.Fatalf("output amount %q is not a valid integer", output)
	}
	if amt.Sign() <= 0 {
		t.Fatalf("output amount %s is not positive", output)
	}
}

func TestSwap_Odos_Quote_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()
	req := swap.QuoteRequest{
		Chain:         "ethereum",
		FromAddress:   addrVitalik,
		SourceAsset:   addrWETHEth,
		TargetAsset:   addrUSDCEth,
		Amount:        "100000000000000000",
		DisplayAmount: "0.1",
		Slippage:      0.5,
	}
	result, err := adapter.Quote(ctx, req)
	if err != nil {
		skipOnRateLimit(t, err)
		t.Fatalf("odos quote: %v", err)
	}
	validateOutputAmount(t, result.OutputAmount)
	t.Logf("odos: 0.1 ETH → %s USDC", result.OutputAmount)
}

func TestSwap_Odos_USDC_to_ETH_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()
	req := swap.QuoteRequest{
		Chain:         "ethereum",
		FromAddress:   addrVitalik,
		SourceAsset:   addrUSDCEth,
		TargetAsset:   addrWETHEth,
		Amount:        "1000000000",
		DisplayAmount: "1000",
		Slippage:      0.5,
	}
	result, err := adapter.Quote(ctx, req)
	if err != nil {
		skipOnRateLimit(t, err)
		t.Fatalf("odos quote: %v", err)
	}
	validateOutputAmount(t, result.OutputAmount)
	t.Logf("odos: 1000 USDC → %s ETH", result.OutputAmount)
}

func TestSwap_Odos_Base_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()
	req := swap.QuoteRequest{
		Chain:         "base",
		FromAddress:   addrVitalik,
		SourceAsset:   addrWETHBase,
		TargetAsset:   addrUSDCBase,
		Amount:        "100000000000000000",
		DisplayAmount: "0.1",
		Slippage:      0.5,
	}
	result, err := adapter.Quote(ctx, req)
	if err != nil {
		skipOnRateLimit(t, err)
		t.Fatalf("odos base quote: %v", err)
	}
	validateOutputAmount(t, result.OutputAmount)
	t.Logf("odos base: 0.1 ETH → %s USDC", result.OutputAmount)
}

func TestSwap_Odos_SlippageControl_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()

	for _, slippage := range []float64{0.1, 1.0, 3.0} {
		req := swap.QuoteRequest{
			Chain:         "ethereum",
			FromAddress:   addrVitalik,
			SourceAsset:   addrWETHEth,
			TargetAsset:   addrUSDCEth,
			Amount:        "100000000000000000",
			DisplayAmount: "0.1",
			Slippage:      slippage,
		}
		result, err := adapter.Quote(ctx, req)
		if err != nil {
			skipOnRateLimit(t, err)
			t.Errorf("odos quote with slippage %.1f: %v", slippage, err)
			continue
		}
		validateOutputAmount(t, result.OutputAmount)
		t.Logf("slippage %.1f%%: output=%s", slippage, result.OutputAmount)
	}
}

func TestSwap_Odos_UnsupportedChain_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()
	req := swap.QuoteRequest{
		Chain:         "solana",
		FromAddress:   "5p2i4HR8HqziBbv6xvF5VxurRp3rZ4BVEfY2H5xy3Lrs",
		SourceAsset:   "11111111111111111111111111111111",
		TargetAsset:   "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		Amount:        "100000000",
		DisplayAmount: "0.1",
		Slippage:      0.5,
	}
	_, err := adapter.Quote(ctx, req)
	if err == nil {
		t.Fatal("expected error for unsupported chain")
	}
	t.Logf("expected error: %v", err)
}

func TestSwap_Odos_ZeroAmount_Errors(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	ctx := integrationCtx(t)
	adapter := adapters.NewOdosAdapter()
	req := swap.QuoteRequest{
		Chain:         "ethereum",
		FromAddress:   addrVitalik,
		SourceAsset:   addrWETHEth,
		TargetAsset:   addrUSDCEth,
		Amount:        "0",
		DisplayAmount: "0",
		Slippage:      0.5,
	}
	_, err := adapter.Quote(ctx, req)
	if err == nil {
		t.Error("expected error for zero amount")
	}
}

func TestSwap_ProtocolList_Consistency(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	reg := swap.NewRegistry()
	adapters.RegisterAll(reg, nil)
	names := reg.List()

	const minProtocols = 6
	if len(names) < minProtocols {
		t.Errorf("expected at least %d protocols, got %d", minProtocols, len(names))
	}

	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			t.Errorf("duplicate protocol: %s", name)
		}
		seen[name] = true

		adapter, err := reg.Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if adapter.Name() != name {
			t.Errorf("adapter name mismatch: %s vs %s", adapter.Name(), name)
		}
	}
	t.Logf("all %d protocols consistent", len(names))
}
