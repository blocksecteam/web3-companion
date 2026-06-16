// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"fmt"
	"math/big"

	"github.com/blocksecteam/web3-companion/internal/token"
)

// Bridge-side wrap handling. Unlike swap, bridge can only intervene on the SOURCE chain:
//   - Input side: the handler can prepend WETH.deposit / WETH.withdraw before the bridge call.
//   - Output side: tokens land on the destination chain; we have no transaction there, so
//                  any mismatch must be detected at quote time and surfaced as an error.
//
// This file owns its own implementation rather than importing from internal/swap to keep
// the two domains independent — they may evolve in different directions.

// NativeWrapIn describes the source-chain conversion required before the bridge call.
type NativeWrapIn struct {
	Wrap   bool // user provides native, bridge wants WETH → WETH.deposit(amountIn) first
	Unwrap bool // user provides WETH, bridge wants native → WETH.withdraw(amountIn) first
}

// DeriveNativeWrapIn computes the input-side wrap action.
//
//	isNativeIn: does the user wallet provide native (not WETH)?
//	cap:        the bridge's declared capability
//
// When the bridge accepts both forms, no wrap is needed regardless of what the user provided.
func DeriveNativeWrapIn(isNativeIn bool, cap NativeCapability) NativeWrapIn {
	return NativeWrapIn{
		Wrap:   isNativeIn && !cap.AcceptsNativeIn, // native in, but bridge needs WETH
		Unwrap: !isNativeIn && !cap.AcceptsWETHIn,  // WETH in, but bridge needs native
	}
}

// CheckOutputCompatible verifies the bridge can deliver the user's requested output form.
// Returns an error suitable for surfacing as a quote-level failure when there is a mismatch.
func CheckOutputCompatible(isNativeOut bool, cap NativeCapability) error {
	if isNativeOut && !cap.OutputsNative {
		return fmt.Errorf("bridge does not deliver native token on destination chain")
	}
	if !isNativeOut && !cap.OutputsWETH {
		return fmt.Errorf("bridge does not deliver wrapped native (WETH) on destination chain")
	}
	return nil
}

// BuildWrapInCalls produces the source-chain pre-call(s) required by the wrap derivation.
// chain selects the WETH contract; amountIn is the raw integer string the user is sending.
// Returns nil (no calls) when no conversion is required. The handler queries the
// WETH address itself (via token.WrappedNativeAddress) at the one site that needs it
// — keeping this function focused on producing calls.
func BuildWrapInCalls(chain string, nw NativeWrapIn, amountIn string) ([]Call, error) {
	if !nw.Wrap && !nw.Unwrap {
		return nil, nil
	}
	wethAddr, ok := token.WrappedNativeAddress(chain)
	if !ok {
		return nil, fmt.Errorf("no WETH address known for chain %q", chain)
	}
	amount, ok := new(big.Int).SetString(amountIn, 10)
	if !ok || amount.Sign() <= 0 {
		return nil, fmt.Errorf("invalid wrap-in amount %q", amountIn)
	}
	if nw.Wrap {
		// WETH.deposit() — payable, attach native value.
		return []Call{{
			To:    wethAddr,
			Value: amount.String(),
			Data:  token.WETHDepositSelector,
		}}, nil
	}
	// nw.Unwrap: WETH.withdraw(uint256).
	return []Call{{
		To:    wethAddr,
		Value: "0",
		Data:  token.EncodeWETHWithdraw(amount),
	}}, nil
}
