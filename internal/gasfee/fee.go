// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

// Package gasfee centralises the formula used to derive gas fee parameters
// from a chain's live RPC, so the signer's actual fee and the build-phase
// simulation estimate never drift apart.
//
// Callers compose the primitives differently:
//
//	vault.FetchFeeParams       -> MaxFeePerGas = 4*baseFee + tip, TipCap = tip
//	build-phase simulation     -> effectiveGasPrice = baseFee + tip
//
// When the RPC is unreachable, both sides fall back to the same constants
// declared here. Build-phase callers should propagate the isFallback flag
// so security checks can warn that the displayed fee is an estimate, not
// a live-RPC value.
package gasfee

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/ethclient"
)

// Fallback values mirror what vault.FetchFeeParams returns when its own
// RPC probe fails. Keeping the numbers here (and consumed by both the
// signer and the build-phase simulator) ensures the two paths agree on
// "what we use when we can't ask the chain".
var (
	// FallbackBaseFee = 24 gwei, FallbackTip = 2 gwei. These are an offline
	// guess of what the chain's live values might be when the RPC is
	// unavailable, not derived from the MaxFeePerGas multiplier. With the
	// current 4x multiplier, MaxFeePerGas under fallback is 98 gwei
	// (24*4+2). This only affects the reserve a wallet must hold to
	// broadcast — actual fees paid are still (baseFee + tip) × gasUsed.
	FallbackBaseFee = big.NewInt(24_000_000_000)
	FallbackTip     = big.NewInt(2_000_000_000)
	MinTip          = big.NewInt(100_000_000) // 0.1 gwei — floor to avoid "gas tip cap 0" rejection
)

// FetchBaseFeeAndTip dials rpcURL and returns the latest block's base fee
// plus the node's tip suggestion. Returns an error on any RPC failure;
// callers decide whether to surface the failure or substitute fallback
// values via ResolveEffectiveGasPriceOrFallback.
func FetchBaseFeeAndTip(ctx context.Context, rpcURL string) (baseFee, tip *big.Int, err error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, nil, fmt.Errorf("dial rpc: %w", err)
	}
	defer client.Close()

	tip, err = client.SuggestGasTipCap(ctx)
	if err != nil || tip == nil {
		return nil, nil, fmt.Errorf("suggest tip cap: %w", err)
	}
	if tip.Cmp(MinTip) < 0 {
		tip = new(big.Int).Set(MinTip)
	}

	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("latest header: %w", err)
	}
	if header.BaseFee == nil {
		return nil, nil, fmt.Errorf("latest header missing base fee")
	}

	return header.BaseFee, tip, nil
}

// ResolveEffectiveGasPriceOrFallback returns baseFee + tip as a single
// effective gas price suitable for build-phase fee estimation. On RPC
// failure it substitutes the package-level fallback values and signals
// the substitution via isFallback so downstream security checks can warn
// that the displayed fee is an offline estimate.
//
// The return value is always non-nil; callers can store the result
// directly without nil-checking.
func ResolveEffectiveGasPriceOrFallback(ctx context.Context, rpcURL string) (effective *big.Int, isFallback bool) {
	baseFee, tip, err := FetchBaseFeeAndTip(ctx, rpcURL)
	if err == nil {
		return new(big.Int).Add(baseFee, tip), false
	}
	return new(big.Int).Add(FallbackBaseFee, FallbackTip), true
}

// ResolveForIntent wraps ResolveEffectiveGasPriceOrFallback into the
// (gasPrice, source) tuple build_handlers store on TransactionIntent. The
// source string is "rpc" when the chain answered, "fallback" otherwise;
// security.GasCostSanity downgrades pass->warn on "fallback" so the user
// is told the displayed fee is an unverified estimate.
func ResolveForIntent(ctx context.Context, rpcURL string) (gasPrice, source string) {
	effective, isFallback := ResolveEffectiveGasPriceOrFallback(ctx, rpcURL)
	if isFallback {
		return effective.String(), "fallback"
	}
	return effective.String(), "rpc"
}
