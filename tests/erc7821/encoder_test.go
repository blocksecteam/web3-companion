// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package erc7821_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
)

func TestEncodeExecuteRejectsEmptyCallsAndEncodesSelector(t *testing.T) {
	if _, err := erc7821.EncodeExecute(nil); err == nil {
		t.Fatal("EncodeExecute(nil) error = nil, want error")
	}

	data, err := erc7821.EncodeExecute([]erc7821.Call{
		{
			To:    common.HexToAddress("0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae"),
			Value: big.NewInt(1),
			Data:  []byte{0xde, 0xad, 0xbe, 0xef},
		},
	})
	if err != nil {
		t.Fatalf("EncodeExecute: %v", err)
	}
	if !strings.HasPrefix(data, "0xe9ae5c53") {
		t.Fatalf("selector = %s, want 0xe9ae5c53", data[:10])
	}
}

func TestEncodeExecuteHexAcceptsStringCalls(t *testing.T) {
	data, err := erc7821.EncodeExecuteHex([]erc7821.HexCall{
		{
			To:    "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
			Value: big.NewInt(0),
			Data:  "0x",
		},
	})
	if err != nil {
		t.Fatalf("EncodeExecuteHex: %v", err)
	}
	if !strings.HasPrefix(data, "0xe9ae5c53") {
		t.Fatalf("selector = %s, want 0xe9ae5c53", data[:10])
	}
}

func TestEncodeExecuteHexRejectsInvalidCalldata(t *testing.T) {
	_, err := erc7821.EncodeExecuteHex([]erc7821.HexCall{
		{
			To:    "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
			Value: big.NewInt(0),
			Data:  "0xnot-hex",
		},
	})
	if err == nil {
		t.Fatal("EncodeExecuteHex invalid calldata error = nil, want error")
	}
}

func TestBuildExecuteBundleSumsValueAndAppliesGasPolicy(t *testing.T) {
	bundle, err := erc7821.BuildExecuteBundle(
		"0xa9C0378137f494FFCddcf862B7b52e01f0b01482",
		[]erc7821.HexCall{
			{To: "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae", Value: big.NewInt(1_000_000_000_000_000_000), Data: "0xaa"},
			{To: "0x2231deb6f5749ef6ce6943a275a1d3e7486f4eae", Value: big.NewInt(500_000_000_000_000_000), Data: "0xbb"},
		},
		1_000_000,
		erc7821.MultiplyGasWithFloor(12, 10, 1_000_000),
	)
	if err != nil {
		t.Fatalf("BuildExecuteBundle: %v", err)
	}
	if bundle.To != "0xa9C0378137f494FFCddcf862B7b52e01f0b01482" {
		t.Fatalf("bundle.To = %q", bundle.To)
	}
	if !strings.HasPrefix(bundle.Data, "0xe9ae5c53") {
		t.Fatalf("bundle.Data selector = %s, want 0xe9ae5c53", bundle.Data[:10])
	}
	if bundle.ValueHex() != "0x14d1120d7b160000" {
		t.Fatalf("bundle.ValueHex() = %q", bundle.ValueHex())
	}
	if bundle.ValueDecimal() != "1500000000000000000" {
		t.Fatalf("bundle.ValueDecimal() = %q", bundle.ValueDecimal())
	}
	if bundle.GasLimit != 1_200_000 {
		t.Fatalf("bundle.GasLimit = %d, want 1200000", bundle.GasLimit)
	}
}

func TestBuildExecuteBundleRejectsInvalidWallet(t *testing.T) {
	_, err := erc7821.BuildExecuteBundle(
		"not-a-wallet",
		[]erc7821.HexCall{{To: "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae", Value: big.NewInt(0), Data: "0x"}},
		0,
		erc7821.MultiplyGasWithFloor(12, 10, 1_000_000),
	)
	if err == nil {
		t.Fatal("BuildExecuteBundle invalid wallet error = nil, want error")
	}
}
