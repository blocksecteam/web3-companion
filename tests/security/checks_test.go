// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/security"
)

func TestSecurity_DecodeOperations_DirectTransaction(t *testing.T) {
	ops, check := security.DecodeOperations(security.CheckRequest{
		To:    testRouter,
		Value: "0",
		Data:  "0xa9059cbb",
	})
	if check.Rule != "calldata_decode" || check.Status != "pass" {
		t.Fatalf("check = %#v, want calldata_decode pass", check)
	}
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ops))
	}
	if ops[0].To != common.HexToAddress(testRouter) {
		t.Fatalf("op.to = %s, want %s", ops[0].To.Hex(), testRouter)
	}
}

func TestSecurity_DecodeOperations_ERC7821(t *testing.T) {
	approve := mustDecodeHex(t, "0x095ea7b3000000000000000000000000"+testRouter[2:]+"0000000000000000000000000000000000000000000000000000000000000064")
	action := mustDecodeHex(t, "0xdeadbeef")
	data, err := erc7821.EncodeExecute([]erc7821.Call{
		{To: common.HexToAddress(testToken), Value: big.NewInt(0), Data: approve},
		{To: common.HexToAddress(testRouter), Value: big.NewInt(0), Data: action},
	})
	if err != nil {
		t.Fatalf("EncodeExecute: %v", err)
	}
	ops, check := security.DecodeOperations(security.CheckRequest{
		To:    testOwner,
		Value: "0",
		Data:  data,
	})
	if check.Rule != "calldata_decode" || check.Status != "pass" {
		t.Fatalf("check = %#v, want calldata_decode pass", check)
	}
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2", len(ops))
	}
	if ops[0].To != common.HexToAddress(testToken) || ops[1].To != common.HexToAddress(testRouter) {
		t.Fatalf("unexpected ops: %#v", ops)
	}
}

func TestSecurity_DecodeOperations_MalformedERC7821(t *testing.T) {
	_, check := security.DecodeOperations(security.CheckRequest{
		To:    testOwner,
		Value: "0",
		Data:  "0xe9ae5c53deadbeef",
	})
	if check.Rule != "calldata_decode" || check.Status != "fail" {
		t.Fatalf("check = %#v, want calldata_decode fail", check)
	}
}

func TestSecurity_HandlerExists(t *testing.T) {
	if security.NewHandler() == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func mustDecodeHex(t *testing.T, data string) []byte {
	t.Helper()
	req := security.CheckRequest{To: testOwner, Value: "0", Data: data}
	ops, check := security.DecodeOperations(req)
	if check.Status != "pass" || len(ops) != 1 {
		t.Fatalf("decode helper failed: %#v ops=%d", check, len(ops))
	}
	return ops[0].Data
}
