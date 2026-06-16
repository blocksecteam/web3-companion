// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestEncodeApprove_ZeroAmount(t *testing.T) {
	data := token.EncodeERC20Approve("0x6131B5fae19EA4f9D964eAc0408E4408b66337b5", big.NewInt(0))
	if !strings.HasPrefix(data, "0x095ea7b3") {
		t.Errorf("expected approve selector 0x095ea7b3, got %s", data[:10])
	}

	if !strings.HasSuffix(data, strings.Repeat("0", 64)) {
		t.Errorf("expected zero amount suffix, got %s", data[len(data)-64:])
	}
}

func TestEncodeApprove_NonZeroAmount(t *testing.T) {
	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	amount := new(big.Int).SetUint64(1_000_000)
	data := token.EncodeERC20Approve(spender, amount)

	if !strings.HasPrefix(data, "0x095ea7b3") {
		t.Errorf("expected approve selector 0x095ea7b3, got %s", data[:10])
	}

	if len(data) != 138 {
		t.Errorf("expected calldata length 138, got %d", len(data))
	}
}

func TestEncodeApprove_SpenderPaddedTo32Bytes(t *testing.T) {
	spender := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	data := token.EncodeERC20Approve(spender, big.NewInt(1))

	spenderField := data[10:74]
	if !strings.HasPrefix(spenderField, "000000000000000000000000") {
		t.Errorf("spender not left-padded: %s", spenderField)
	}
	if !strings.HasSuffix(strings.ToLower(spenderField), strings.ToLower(spender[2:])) {
		t.Errorf("spender address not present in calldata: %s", spenderField)
	}
}

func TestBuildApproveSequence_SufficientAllowance_ReturnsEmpty(t *testing.T) {
	token := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	amount := big.NewInt(1_000_000)
	current := new(big.Int).Set(amount)

	calls := swap.BuildApproveSequence(token, spender, amount, current)
	if len(calls) != 0 {
		t.Errorf("expected 0 approve calls when allowance sufficient, got %d", len(calls))
	}
}

func TestBuildApproveSequence_AllowanceExceedsAmount_ReturnsEmpty(t *testing.T) {
	token := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	amount := big.NewInt(1_000_000)
	current := big.NewInt(999_999_999)

	calls := swap.BuildApproveSequence(token, spender, amount, current)
	if len(calls) != 0 {
		t.Errorf("expected 0 approve calls when allowance > amount, got %d", len(calls))
	}
}

func TestBuildApproveSequence_ZeroAllowance_ReturnsSingleApprove(t *testing.T) {
	token := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	amount := big.NewInt(1_000_000)
	current := big.NewInt(0)

	calls := swap.BuildApproveSequence(token, spender, amount, current)
	if len(calls) != 1 {
		t.Fatalf("expected 1 approve call, got %d", len(calls))
	}
	if calls[0].To != token {
		t.Errorf("approve call to wrong address: %s", calls[0].To)
	}
	if !strings.HasPrefix(calls[0].Data, "0x095ea7b3") {
		t.Errorf("not an approve call: %s", calls[0].Data[:10])
	}
}

func TestBuildApproveSequence_PartialAllowance_ReturnsTwoApprovesForSafeApprove(t *testing.T) {

	tokenAddr := "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	amount := big.NewInt(5_000_000)
	current := big.NewInt(1_000_000)

	calls := swap.BuildApproveSequence(tokenAddr, spender, amount, current)
	if len(calls) != 2 {
		t.Fatalf("expected 2 approve calls (reset + set), got %d", len(calls))
	}

	zeroApprove := token.EncodeERC20Approve(spender, big.NewInt(0))
	if calls[0].Data != zeroApprove {
		t.Errorf("first call should be approve(0), got %s", calls[0].Data)
	}

	fullApprove := token.EncodeERC20Approve(spender, amount)
	if calls[1].Data != fullApprove {
		t.Errorf("second call should be approve(amount), got %s", calls[1].Data)
	}
}

func TestBuildApproveSequence_CallsTargetToken(t *testing.T) {
	token := "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"

	calls := swap.BuildApproveSequence(token, spender, big.NewInt(1_000_000), big.NewInt(0))
	for _, c := range calls {
		if !strings.EqualFold(c.To, token) {
			t.Errorf("approve call should target token %s, got %s", token, c.To)
		}
		if c.Value != nil && c.Value.Sign() != 0 {
			t.Errorf("approve call should have zero value")
		}
	}
}
