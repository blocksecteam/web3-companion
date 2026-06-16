// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"fmt"
	"sync"
)

type Adapter interface {
	Name() string
	SupportedPairs() []ChainPair
	// Capabilities returns the unified capability surface for the bridge.
	// See BridgeCapabilities below — the handler reads Approve.Mode to dispatch
	// approve/deposit flows, reads Native to insert source-chain wrap/unwrap
	// calls, and surfaces SupportsSlippage through /bridge/protocols so the
	// skill / agent knows whether the user's --slippage actually does anything.
	Capabilities() BridgeCapabilities
	Quote(ctx context.Context, req QuoteRequest) (*QuoteResult, error)
	Build(ctx context.Context, req BuildRequest) (*BuildResult, error)
}

// BridgeCapabilities is the single unified capability surface a bridge adapter
// exposes. Sub-structs hold orthogonal concerns:
//
//	Native           — wrap/unwrap dispatch + destination-side rejection.
//	Approve          — how the bridge collects user funds (Mode + Style);
//	                   drives the handler's approve-sequence computation.
//	SupportsSlippage — whether a user-passed slippage percent actually affects
//	                   execution. Correlated with Approve.Mode but NOT 1:1:
//	                   counter-examples include canonical L2 bridges (Optimism,
//	                   Arbitrum, Polygon) which are Mode="approve" + 1:1
//	                   lock-and-mint with no slippage concept, and intent-based
//	                   bridges (Across, UniswapX cross-chain) where the rate is
//	                   an off-chain spread. Declare explicitly per adapter; do
//	                   NOT derive from Mode.
//
// Future user-facing bridge metadata lands as new fields here rather than as
// new interface methods, so adapter implementations stay flat.
type BridgeCapabilities struct {
	Native           NativeCapability  `json:"native"`
	Approve          ApproveCapability `json:"approve"`
	SupportsSlippage bool              `json:"supports_slippage"`
}

// NativeCapability describes which native/wrapped token forms the bridge supports.
// AcceptsXxxIn fields apply to source-chain input; OutputsXxx fields apply to
// destination-chain output.
type NativeCapability struct {
	AcceptsNativeIn bool
	AcceptsWETHIn   bool
	OutputsNative   bool
	OutputsWETH     bool
}

const (
	ApproveModeApprove = "approve"
	ApproveModeDeposit = "deposit"

	ApproveStyleERC20   = "erc20"
	ApproveStylePermit2 = "permit2"
)

// ApproveCapability declares how a bridge collects user funds. Used by the
// handler at build time to either compute an approve sequence (Mode="approve")
// or surface a deposit address to the security agent (Mode="deposit"). Style
// only applies when Mode="approve" and selects the approval ABI.
//
// Slippage support is NOT in this struct — it lives on the parent
// BridgeCapabilities because it is not always implied by Mode (e.g. canonical
// L2 bridges are Mode="approve" but do not honor slippage).
type ApproveCapability struct {
	Mode  string `json:"mode"`            // "approve" | "deposit"
	Style string `json:"style,omitempty"` // "erc20" | "permit2" (only for Mode=approve)
}

type ChainPair struct {
	FromChain string `json:"from_chain"`
	ToChain   string `json:"to_chain"`
}

type QuoteRequest struct {
	Bridge      string `json:"bridge,omitempty"`
	FromChain   string `json:"from_chain" binding:"required"`
	ToChain     string `json:"to_chain" binding:"required"`
	FromToken   string `json:"from_token" binding:"required"`
	ToToken     string `json:"to_token"`
	Amount      string `json:"amount" binding:"required"`
	FromAddress string `json:"from_address"`
	// Slippage is forwarded only to adapters that swap on the source chain
	// (i.e. approve-mode bridges that run their own on-chain router call,
	// where amountOutMin is enforced by the contract). Pure deposit-mode
	// bridges accept whatever rate the off-chain service quotes at execution
	// time, so source-chain slippage is meaningless and is silently dropped;
	// future deposit-mode adapters should ignore this field by design, not
	// surface it as a half-working parameter.
	Slippage float64 `json:"slippage"`
}

type QuoteResult struct {
	Bridge           string `json:"bridge"`
	InputAmount      string `json:"input_amount"`
	EstimatedReceive string `json:"estimated_receive"`
	// ApproveTarget is the spender for approve-mode bridges; handler reads it
	// during Build to compute the approve sequence. Empty for deposit-mode.
	ApproveTarget string `json:"approve_target,omitempty"`
	// DepositAddress is the per-quote deposit address for deposit-mode bridges;
	// handler surfaces it in the transaction-intent summary so the security
	// agent can verify the simulated transfer target matches. Empty for
	// approve-mode bridges. Both ApproveTarget and DepositAddress are
	// quote-time, per-quote metadata — sibling fields, same lifecycle.
	DepositAddress string      `json:"deposit_address,omitempty"`
	RawQuote       interface{} `json:"raw_quote"`
}

type BuildRequest struct {
	Quote interface{} `json:"quote,omitempty"`
}

type Call struct {
	To    string `json:"to"`
	Value string `json:"value"`
	Data  string `json:"data"`
}

// BuildResult describes the single bridge call the adapter constructed.
// Any wrap/approve prefixes are appended by the handler (it owns the ERC-7821 batch
// assembly) — adapters must NOT bundle them in. If a future bridge protocol genuinely
// needs multiple internal calls, introduce an explicit extension interface instead of
// re-adding a Calls field, so the contract stays unambiguous.
type BuildResult struct {
	To       string `json:"to"`
	Value    string `json:"value"`
	Data     string `json:"data"`
	GasLimit uint64 `json:"gas_limit"`
}

// Registry holds all registered bridge adapters; thread-safe.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

func (r *Registry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Name()] = a
}

func (r *Registry) Get(name string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown bridge: %s", name)
	}
	return a, nil
}

func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		all = append(all, a)
	}
	return all
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
