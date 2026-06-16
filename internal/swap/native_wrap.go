// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"fmt"
	"math/big"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/token"
)

// NativeWrap describes the native/wrapped token conversions required outside the router for a swap.
// The zero value (all false) means no extra operations are needed.
//
// WrapIn:    Protocol accepts only WETH, user provides ETH  → WETH.deposit(amountIn) before swap
// UnwrapIn:  Protocol accepts only ETH,  user provides WETH → WETH.withdraw(amountIn) before swap
// WrapOut:   Protocol outputs ETH, user expects WETH        → WETH.deposit(amountOutMin) after swap
// UnwrapOut: Protocol outputs WETH, user expects ETH        → WETH.withdraw(amountOutMin) after swap
type NativeWrap struct {
	WrapIn    bool
	UnwrapIn  bool
	WrapOut   bool
	UnwrapOut bool
}

// WrapCalls generates pre/post erc7821.HexCall lists based on NativeWrap flags.
// wethAddr: WETH contract address on the current chain
// amountIn: swap input amount
// amountOutMin: minimum swap output amount (after slippage deduction)
func WrapCalls(wethAddr string, nw NativeWrap, amountIn, amountOutMin *big.Int) (pre, post []erc7821.HexCall) {
	if nw.WrapIn {
		pre = append(pre, erc7821.HexCall{
			To:    wethAddr,
			Value: new(big.Int).Set(amountIn),
			Data:  token.WETHDepositSelector,
		})
	}
	if nw.UnwrapIn {
		pre = append(pre, erc7821.HexCall{
			To:    wethAddr,
			Value: big.NewInt(0),
			Data:  token.EncodeWETHWithdraw(amountIn),
		})
	}
	if nw.WrapOut {
		// WrapOut: deposit ETH into WETH after the swap. amountOutMin is used as the deposit amount —
		// the actual swap output is only known at execution time; any positive-slippage surplus stays as ETH.
		post = append(post, erc7821.HexCall{
			To:    wethAddr,
			Value: new(big.Int).Set(amountOutMin),
			Data:  token.WETHDepositSelector,
		})
	}
	if nw.UnwrapOut {
		post = append(post, erc7821.HexCall{
			To:    wethAddr,
			Value: big.NewInt(0),
			Data:  token.EncodeWETHWithdraw(amountOutMin),
		})
	}
	return
}

// ResolveWrapCalls derives the WETH wrap/unwrap calls required before and after a swap,
// given the chain, NativeWrap flags, and amounts.
// Handles WETH address lookup, amount parsing, and amountOutMin calculation internally.
// nw determines which calls are generated; a zero value (all false) returns empty slices without error.
func ResolveWrapCalls(chain string, nw NativeWrap, inputAmount, outputAmount string, slippageBPS int) (pre, post []erc7821.HexCall, err error) {
	if nw == (NativeWrap{}) {
		return nil, nil, nil
	}
	wethAddr, ok := token.WrappedNativeAddress(chain)
	if !ok {
		return nil, nil, fmt.Errorf("ResolveWrapCalls: no WETH address for chain %q", chain)
	}
	amountIn, ok := new(big.Int).SetString(inputAmount, 10)
	if !ok {
		return nil, nil, fmt.Errorf("ResolveWrapCalls: invalid InputAmount %q", inputAmount)
	}
	out, ok := new(big.Int).SetString(outputAmount, 10)
	if !ok {
		return nil, nil, fmt.Errorf("ResolveWrapCalls: invalid OutputAmount %q", outputAmount)
	}
	amountOutMin := new(big.Int).Mul(out, big.NewInt(int64(10000-slippageBPS)))
	amountOutMin.Div(amountOutMin, big.NewInt(10000))

	pre, post = WrapCalls(wethAddr, nw, amountIn, amountOutMin)
	return pre, post, nil
}

// DeriveNativeWrap derives the NativeWrap flags from the request shape and protocol capabilities.
//
// Request side:
//
//	isNativeIn  - does the user provide a native token?
//	isNativeOut - does the user expect to receive a native token?
//
// Protocol capabilities (declared by the adapter via Capabilities().Native):
//
//	cap.AcceptsNativeIn / cap.AcceptsWETHIn / cap.OutputsNative / cap.OutputsWETH
//
// Derivation rule: user's token form != protocol's accepted form → conversion required.
// When the protocol supports both forms (accepts/outputs both true), no external wrap is ever
// needed because the protocol can adapt regardless of what the user provides.
func DeriveNativeWrap(isNativeIn, isNativeOut bool, cap NativeCapability) NativeWrap {
	return NativeWrap{
		WrapIn:    isNativeIn && !cap.AcceptsNativeIn, // user provides native, protocol won't accept → deposit before swap
		UnwrapIn:  !isNativeIn && !cap.AcceptsWETHIn,  // user provides WETH, protocol won't accept → withdraw before swap
		WrapOut:   !isNativeOut && !cap.OutputsWETH,   // user wants WETH, protocol won't output → deposit after swap
		UnwrapOut: isNativeOut && !cap.OutputsNative,  // user wants native, protocol won't output → withdraw after swap
	}
}
