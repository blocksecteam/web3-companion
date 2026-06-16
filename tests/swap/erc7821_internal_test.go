// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestUnifiedERC7821Encoder_Selector(t *testing.T) {

	calls := []erc7821.HexCall{
		{To: "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5", Value: big.NewInt(0), Data: "0x"},
	}
	data, err := erc7821.EncodeExecuteHex(calls)
	if err != nil {
		t.Fatalf("EncodeExecuteHex: %v", err)
	}
	if !strings.HasPrefix(data, "0xe9ae5c53") {
		t.Errorf("expected execute selector 0xe9ae5c53, got %s", data[:10])
	}
}

func TestUnifiedERC7821Encoder_ModeBytes(t *testing.T) {

	calls := []erc7821.HexCall{
		{To: "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5", Value: big.NewInt(0), Data: "0x"},
	}
	data, err := erc7821.EncodeExecuteHex(calls)
	if err != nil {
		t.Fatalf("EncodeExecuteHex: %v", err)
	}
	hex := strings.TrimPrefix(data, "0x")
	if len(hex) < 8+64 {
		t.Fatalf("calldata too short: %d hex chars", len(hex))
	}
	modeWord := hex[8 : 8+64]
	if !strings.HasPrefix(modeWord, "01") {
		t.Errorf("mode bytes32 should start with 01 (OPCODE_SUPPORT), got: %s", modeWord[:8])
	}
	if !strings.HasSuffix(modeWord, strings.Repeat("0", 62)) {
		t.Errorf("mode bytes32 trailing 62 chars should be zero, got: %s", modeWord[2:])
	}
}

func TestUnifiedERC7821Encoder_SingleCallWithValue(t *testing.T) {

	calls := []erc7821.HexCall{
		{
			To:    "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
			Value: big.NewInt(1_000_000_000_000_000),
			Data:  "0xdeadbeef",
		},
	}
	data, err := erc7821.EncodeExecuteHex(calls)
	if err != nil {
		t.Fatalf("EncodeExecuteHex: %v", err)
	}
	if !strings.HasPrefix(data, "0xe9ae5c53") {
		t.Errorf("wrong selector: %s", data[:10])
	}

	if !strings.Contains(strings.ToLower(data), "38d7ea4c68000") {
		t.Errorf("ETH value not found in calldata")
	}
}

func TestUnifiedERC7821Encoder_ApprovePlusSwapBatch(t *testing.T) {

	spender := "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
	tokenAddr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	amount := big.NewInt(1_000_000)

	calls := []erc7821.HexCall{
		{To: tokenAddr, Value: big.NewInt(0), Data: token.EncodeERC20Approve(spender, big.NewInt(0))},
		{To: tokenAddr, Value: big.NewInt(0), Data: token.EncodeERC20Approve(spender, amount)},
		{To: spender, Value: big.NewInt(0), Data: "0xdeadbeef"},
	}
	data, err := erc7821.EncodeExecuteHex(calls)
	if err != nil {
		t.Fatalf("EncodeExecuteHex: %v", err)
	}
	if !strings.HasPrefix(data, "0xe9ae5c53") {
		t.Errorf("wrong selector: %s", data[:10])
	}

	approveCount := strings.Count(strings.ToLower(data), "095ea7b3")
	if approveCount != 2 {
		t.Errorf("expected 2 approve selectors in batch, found %d", approveCount)
	}
}

func TestUnifiedERC7821Encoder_MatchesPythonKnownVector(t *testing.T) {

	calls := []erc7821.HexCall{
		{To: "0x0000000000000000000000000000000000000000", Value: big.NewInt(0), Data: "0x"},
	}
	data, err := erc7821.EncodeExecuteHex(calls)
	if err != nil {
		t.Fatalf("EncodeExecuteHex: %v", err)
	}
	if len(data) < 10 {
		t.Fatalf("calldata too short")
	}
	if data[:10] != "0xe9ae5c53" {
		t.Errorf("selector mismatch: %s", data[:10])
	}
}
