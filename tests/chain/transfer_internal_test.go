// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestEncodeERC20Transfer_Selector(t *testing.T) {
	data := token.EncodeERC20Transfer(
		"0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		big.NewInt(100_000),
	)
	if !strings.HasPrefix(data, "0xa9059cbb") {
		t.Errorf("expected transfer selector 0xa9059cbb, got %s", data[:10])
	}
}

func TestEncodeERC20Transfer_Length(t *testing.T) {

	data := token.EncodeERC20Transfer(
		"0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		big.NewInt(100_000),
	)
	if len(data) != 138 {
		t.Errorf("expected length 138, got %d: %s", len(data), data)
	}
}

func TestEncodeERC20Transfer_RecipientPaddedTo32Bytes(t *testing.T) {
	recipient := "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510"
	data := token.EncodeERC20Transfer(recipient, big.NewInt(1))

	addrField := data[10:74]
	if !strings.HasPrefix(addrField, "000000000000000000000000") {
		t.Errorf("address not left-padded: %s", addrField)
	}
	if !strings.HasSuffix(strings.ToLower(addrField), strings.ToLower(recipient[2:])) {
		t.Errorf("recipient not in calldata: %s", addrField)
	}
}

func TestEncodeERC20Transfer_AmountEncoded(t *testing.T) {

	amount := big.NewInt(100_000)
	data := token.EncodeERC20Transfer("0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510", amount)
	if !strings.Contains(strings.ToLower(data), "186a0") {
		t.Errorf("amount 100000 (0x186a0) not found in calldata: %s", data)
	}
}

func TestEncodeERC20Transfer_ZeroAmount(t *testing.T) {
	data := token.EncodeERC20Transfer("0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510", big.NewInt(0))
	if !strings.HasPrefix(data, "0xa9059cbb") {
		t.Errorf("expected transfer selector, got %s", data[:10])
	}
	if !strings.HasSuffix(data, strings.Repeat("0", 64)) {
		t.Errorf("zero amount should be 64 zero hex chars at end")
	}
}

func TestIsNativeToken_NativeKeyword(t *testing.T) {
	if !token.IsNativeToken("native") {
		t.Error("'native' should be recognized as native")
	}
}

func TestIsNativeToken_ChainSpecificAliases_Rejected(t *testing.T) {

	for _, alias := range []string{"eth", "bnb", "pol", "matic"} {
		if token.IsNativeToken(alias) {
			t.Errorf("%q should NOT be native (use 'native' keyword)", alias)
		}
	}
}

func TestIsNativeToken_ERC20Address_ReturnsFalse(t *testing.T) {
	if token.IsNativeToken("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48") {
		t.Error("ERC-20 address should not be native")
	}
}

func TestIsNativeToken_BareSybol_ReturnsFalse(t *testing.T) {

	if token.IsNativeToken("USDC") {
		t.Error("bare symbol USDC should not be native")
	}
}

func TestIsNativeToken_Empty_ReturnsFalse(t *testing.T) {
	if token.IsNativeToken("") {
		t.Error("empty string should not be native (require explicit token)")
	}
}
