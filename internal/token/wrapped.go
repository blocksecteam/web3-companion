// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"encoding/hex"
	"math/big"
	"strings"
)

// wrappedNativeByChain maps a chain name to its canonical wrapped-native (WETH/WBNB/...) contract address.
// This is the single source of truth; callers in swap, bridge, etc. should look up via WrappedNativeAddress.
var wrappedNativeByChain = map[string]string{
	"ethereum": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
	"optimism": "0x4200000000000000000000000000000000000006",
	"bsc":      "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c",
	"polygon":  "0x0d500B1d8E8eF31E21C99d1Db9A6444d3ADf1270",
	"base":     "0x4200000000000000000000000000000000000006",
	"arbitrum": "0x82aF49447D8a07e3bd95BD0d56f35241523fBab1",
}

// WrappedNativeAddress returns the wrapped-native contract address for the given chain,
// or ("", false) when the chain is unknown.
func WrappedNativeAddress(chain string) (string, bool) {
	addr, ok := wrappedNativeByChain[strings.ToLower(strings.TrimSpace(chain))]
	return addr, ok
}

// WETHDepositSelector is the calldata for WETH.deposit() — payable, no args.
// The selector and ABI are identical across every EVM chain WETH ships on, so
// callers in swap, bridge, etc. should reference this constant rather than
// hard-coding the four bytes.
const WETHDepositSelector = "0xd0e30db0"

// EncodeWETHWithdraw encodes WETH.withdraw(uint256 wad) calldata: selector
// 0x2e1a7d4d followed by the 32-byte big-endian amount.
//
// Panics on nil or non-positive amount — a zero or negative withdraw is
// almost certainly a caller bug (the resulting tx would be a no-op or
// revert) and the defensive panic surfaces it at the call site rather than
// at simulation time.
func EncodeWETHWithdraw(amount *big.Int) string {
	if amount == nil || amount.Sign() <= 0 {
		panic("token.EncodeWETHWithdraw: amount must be positive")
	}
	padded := make([]byte, 32)
	amount.FillBytes(padded)
	return "0x2e1a7d4d" + hex.EncodeToString(padded)
}
