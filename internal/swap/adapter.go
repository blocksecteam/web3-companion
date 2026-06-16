// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"fmt"
	"sync"
)

type QuoteRequest struct {
	Chain         string  `json:"chain"`
	ToChain       string  `json:"to_chain,omitempty"`
	FromAddress   string  `json:"from_address"`
	SourceAsset   string  `json:"source_asset"`
	TargetAsset   string  `json:"target_asset"`
	Amount        string  `json:"amount"`
	DisplayAmount string  `json:"display_amount"`
	Slippage      float64 `json:"slippage"`
}

type QuoteResult struct {
	Protocol      string      `json:"protocol"`
	SourceAsset   string      `json:"source_asset"`
	TargetAsset   string      `json:"target_asset"`
	InputAmount   string      `json:"input_amount"`
	OutputAmount  string      `json:"output_amount"`
	RawQuote      interface{} `json:"raw_quote"`
	ApproveTarget string      `json:"approve_target,omitempty"`
	ApproveStyle  string      `json:"approve_style,omitempty"`
}

type BuildRequest struct {
	Chain       string      `json:"chain"`
	FromAddress string      `json:"from_address"`
	Quote       interface{} `json:"quote"`
	SlippageBPS int         `json:"slippage_bps"`
}

type BuildResult struct {
	To       string `json:"to"`
	Value    string `json:"value"`
	Data     string `json:"data"`
	GasLimit uint64 `json:"gas_limit"`
}

// NativeCapability describes which token forms a protocol handles on each side.
// Used to derive NativeWrap: when the user's token form differs from what the protocol
// accepts, an external wrap/unwrap is required.
// When the protocol supports both forms (accepts/outputs both true), no external wrap is
// ever needed because the protocol can adapt regardless of what the user provides.
type NativeCapability struct {
	AcceptsNativeIn bool // can the protocol accept native input directly?
	AcceptsWETHIn   bool // can the protocol accept WETH input directly?
	OutputsNative   bool // can the protocol output native directly?
	OutputsWETH     bool // can the protocol output WETH directly?
}

// SwapCapabilities is the single unified capability surface a swap adapter
// exposes to the handler, skill, and HTTP /swap/protocols endpoint. Sub-structs
// hold orthogonal concerns:
//
//	Native           — how the adapter handles native / WETH on each side;
//	                   drives the handler's WETH wrap/unwrap insertion.
//	SupportsSlippage — whether a user-passed slippage percent actually affects
//	                   execution. true for AMM-style aggregators (current 7
//	                   adapters); false for RFQ / limit-order / Dutch-auction
//	                   protocols (e.g. 1inch Fusion, CowSwap, UniswapX) where
//	                   the user signs an intent at a fixed rate.
//
// Future user-facing protocol metadata lands as new fields on SwapCapabilities
// rather than as new interface methods, so adapter implementations stay flat.
type SwapCapabilities struct {
	Native           NativeCapability `json:"native"`
	SupportsSlippage bool             `json:"supports_slippage"`
}

type ProtocolAdapter interface {
	Name() string
	SupportedChains() []string
	Capabilities() SwapCapabilities
	Quote(ctx context.Context, req QuoteRequest) (*QuoteResult, error)
	Build(ctx context.Context, req BuildRequest) (*BuildResult, error)
}

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]ProtocolAdapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]ProtocolAdapter)}
}

func (r *Registry) Register(a ProtocolAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Name()] = a
}

func (r *Registry) Get(name string) (ProtocolAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown protocol: %s", name)
	}
	return a, nil
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

func (r *Registry) All() []ProtocolAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]ProtocolAdapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		all = append(all, a)
	}
	return all
}

func chainSupported(supported []string, chain string) bool {
	for _, c := range supported {
		if c == chain {
			return true
		}
	}
	return false
}
