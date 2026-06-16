// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token_test

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestIsNativeToken_Native_IsNative(t *testing.T) {
	if !token.IsNativeToken("native") {
		t.Error("'native' must be recognised")
	}
}

func TestIsNativeToken_CaseInsensitive(t *testing.T) {
	for _, s := range []string{"Native", "NATIVE", "nAtIvE"} {
		if !token.IsNativeToken(s) {
			t.Errorf("%q must be recognised (case-insensitive)", s)
		}
	}
}

func TestIsNativeToken_ZeroAddress_IsNative(t *testing.T) {
	if !token.IsNativeToken("0x0000000000000000000000000000000000000000") {
		t.Error("zero address must be native")
	}
}

func TestIsNativeToken_EIP155Address_IsNative(t *testing.T) {
	if !token.IsNativeToken("0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE") {
		t.Error("EIP-155 native address must be native")
	}
}

func TestIsNativeToken_ChainSpecificSymbols_NotNative(t *testing.T) {

	for _, sym := range []string{"eth", "bnb", "pol", "matic", "avax", "ETH", "BNB"} {
		if token.IsNativeToken(sym) {
			t.Errorf("%q must NOT be native (use 'native' keyword instead)", sym)
		}
	}
}

func TestIsNativeToken_EmptyString_NotNative(t *testing.T) {
	if token.IsNativeToken("") {
		t.Error("empty string must NOT be native — callers must pass explicit token")
	}
}

func TestIsNativeToken_ERC20Address_NotNative(t *testing.T) {
	if token.IsNativeToken("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48") {
		t.Error("ERC-20 address must not be native")
	}
}

func TestIsNativeToken_BareSymbol_NotNative(t *testing.T) {
	for _, sym := range []string{"USDC", "USDT", "WBTC", "DAI"} {
		if token.IsNativeToken(sym) {
			t.Errorf("bare symbol %q must not be native", sym)
		}
	}
}
