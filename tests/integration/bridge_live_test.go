// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
	"github.com/blocksecteam/web3-companion/internal/config"
)

func liveConfig() *config.Config {
	cfg := config.LoadFromEnv()
	if len(cfg.Chains) == 0 {
		cfg.Chains = []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH"},
			{Name: "base", ChainID: 8453, NativeSymbol: "ETH"},
			{Name: "polygon", ChainID: 137, NativeSymbol: "POL"},
			{Name: "bsc", ChainID: 56, NativeSymbol: "BNB"},
		}
	}
	return cfg
}

// newLiveBridgeRegistry mirrors how the production server wires bridges in
// internal/server/router.go: build an empty Registry, then populate via the
// adapters package. The resolver is nil — adapters that need it (currently
// relay) error at Quote time, which is fine for these smoke checks.
func newLiveBridgeRegistry() *bridge.Registry {
	reg := bridge.NewRegistry()
	bridgeadapters.RegisterAll(reg, nil)
	return reg
}

func TestBridge_ListProtocols(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	names := newLiveBridgeRegistry().List()
	got := make(map[string]bool, len(names))
	for _, name := range names {
		got[name] = true
	}
	for _, want := range []string{"bungee", "relay"} {
		if !got[want] {
			t.Errorf("missing production bridge protocol %q: %v", want, names)
		}
	}
	t.Logf("bridge protocols: %v", names)
}

func TestBridge_StubQuote_EthereumToBase(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	req := bridge.QuoteRequest{
		FromChain:   "ethereum",
		ToChain:     "base",
		FromToken:   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		ToToken:     "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		Amount:      "1000000000",
		FromAddress: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		Slippage:    0.5,
	}

	for _, a := range newLiveBridgeRegistry().All() {
		quote, err := a.Quote(integrationCtx(t), req)
		if err != nil {
			t.Logf("%s quote error: %v", a.Name(), err)
			continue
		}
		t.Logf("%s: %s→%s, estimated=%s", a.Name(), req.FromChain, req.ToChain, quote.EstimatedReceive)
		if quote.Bridge != a.Name() {
			t.Errorf("%s: bridge name mismatch in quote", a.Name())
		}
	}
}

func TestBridge_StubQuote_CrossChainPairs(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	pairs := []struct {
		from, to string
	}{
		{"ethereum", "arbitrum"},
		{"ethereum", "optimism"},
		{"ethereum", "polygon"},
		{"ethereum", "base"},
		{"ethereum", "bsc"},
	}

	adapters := newLiveBridgeRegistry().All()
	for _, p := range pairs {
		req := bridge.QuoteRequest{
			FromChain:   p.from,
			ToChain:     p.to,
			FromToken:   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			Amount:      "1000000000",
			FromAddress: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
			Slippage:    0.5,
		}
		for _, a := range adapters {

			supported := false
			for _, sp := range a.SupportedPairs() {
				if sp.FromChain == p.from && sp.ToChain == p.to {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
			quote, err := a.Quote(integrationCtx(t), req)
			if err != nil {
				t.Logf("%s quote %s→%s: %v (external API, may be rate-limited)", a.Name(), p.from, p.to, err)
				continue
			}
			t.Logf("%s: %s→%s, estimated=%s", a.Name(), p.from, p.to, quote.EstimatedReceive)
		}
	}
}

func TestBridge_StubBuild_NilQuote_Errors(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	for _, a := range newLiveBridgeRegistry().All() {
		_, err := a.Build(integrationCtx(t), bridge.BuildRequest{Quote: nil})
		if err == nil {
			t.Errorf("%s: expected error for nil quote, got success", a.Name())
		} else {
			t.Logf("%s: nil quote → %v (expected)", a.Name(), err)
		}
	}
}
