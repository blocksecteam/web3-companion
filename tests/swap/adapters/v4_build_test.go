// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters_test

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
	"github.com/blocksecteam/web3-companion/internal/swap/adapters/onchain"
)

const (
	_weth = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	_usdc = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
)

const universalRouterSelector = "3593564c"

func TestUniswapV4Build_CallDataHasCorrectSelector(t *testing.T) {
	adapter := adapters.NewUniswapAdapter(nil)

	flatData := map[string]interface{}{
		"token_in":       _weth,
		"token_out":      _usdc,
		"amount_in":      "1000000000000000",
		"amount_out_min": "2278550",
		"deadline":       int(9999999999),
		"is_native_in":   true,
		"is_native_out":  false,
		"swap_router":    "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		"weth":           _weth,
		"version":        "v4",
		"fee":            500,
		"tick_spacing":   10,
		"currency0":      _usdc,
		"currency1":      _weth,
		"zero_for_one":   false,
	}

	req := swap.BuildRequest{
		Chain:       "ethereum",
		FromAddress: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		Quote:       flatData,
	}

	result, err := adapter.Build(nil, req) //nolint:staticcheck
	if err != nil {
		t.Fatalf("Build(v4 route) err = %v", err)
	}

	data := strings.ToLower(strings.TrimPrefix(result.Data, "0x"))
	if !strings.HasPrefix(data, universalRouterSelector) {
		t.Errorf("data should start with Universal Router selector %s, got %s...", universalRouterSelector, data[:8])
	}

	if !strings.Contains(data, "10") {
		t.Errorf("calldata should contain CMD_V4_SWAP (0x10) but it doesn't")
	}
}

func TestPancakeInfinityCLBuild_CallDataHasCorrectSelector(t *testing.T) {
	adapter := adapters.NewPancakeAdapter(nil)

	poolManager := "0xa0FfB9c1CE1Fe56963B0321B32E7A0302114058b"

	req := swap.BuildRequest{
		Chain:       "bsc",
		FromAddress: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		Quote: map[string]interface{}{
			"chain_id":       float64(56),
			"token_in":       "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c",
			"token_out":      "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
			"amount_in":      "1000000000000000",
			"amount_out_min": "65670000000000000",
			"deadline":       float64(9999999999),
			"is_native_in":   true,
			"is_native_out":  false,
			"swap_router":    "0xd9C500DfF816a1Da21A48A732d3498Bf09dc9AEB",
			"weth":           "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c",
			"sender":         "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
			"version":        "pancake_infinity_cl",
			"fee":            float64(500),
			"tick_spacing":   float64(10),
			"currency0":      "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
			"currency1":      "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c",
			"zero_for_one":   false,
			"pool_manager":   poolManager,
		},
	}

	result, err := adapter.Build(nil, req) //nolint:staticcheck
	if err != nil {
		t.Fatalf("Build(pancake_infinity_cl) err = %v", err)
	}

	data := strings.ToLower(strings.TrimPrefix(result.Data, "0x"))
	if !strings.HasPrefix(data, universalRouterSelector) {
		t.Errorf("expected Universal Router selector, got %s...", data[:8])
	}

	if !strings.Contains(data, "10") {
		t.Errorf("calldata should contain CMD_V4_SWAP (0x10) for Infinity CL")
	}
}

func TestEncodeUniV4SwapInput_StructureIsValid(t *testing.T) {
	amountIn := big.NewInt(1_000_000_000_000_000)
	amountOutMin := big.NewInt(2_280_000)

	out := onchain.EncodeUniV4SwapInput(
		hexToAddress(_usdc), hexToAddress(_weth),
		500, 10, false,
		amountIn, amountOutMin,
	)
	if len(out) == 0 {
		t.Fatal("encodeUniV4SwapInput returned empty bytes")
	}

	hex := fmt.Sprintf("%x", out)

	if !strings.Contains(hex, "060c0f") {
		t.Errorf("expected actions bytes 060c0f in encoded output, got prefix: %s", hex[:64])
	}
}

func hexToAddress(addr string) common.Address {
	return common.HexToAddress(addr)
}
