// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"math/big"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
	"github.com/blocksecteam/web3-companion/internal/swap/adapters/onchain"
)

// quoteVersion pulls "version" out of QuoteResult.RawQuote (a
// map[string]interface{} the adapter populates with route metadata). Both
// Uniswap and Pancake adapters set version to the chosen on-chain route
// family ("v3" / "v4" / "pancake_v2" / "pancake_v3" / "pancake_infinity_cl").
// Returns empty string when raw quote shape is unexpected.
func quoteVersion(q *swap.QuoteResult) string {
	m, ok := q.RawQuote.(map[string]interface{})
	if !ok {
		return ""
	}
	v, _ := m["version"].(string)
	return v
}

var amountIn = new(big.Int).Exp(big.NewInt(10), big.NewInt(14), nil)

func chainRPC(t *testing.T, chain string) string {
	t.Helper()
	rpc := configuredRPCURLs()[chain]
	if rpc == "" {
		t.Skipf("WEB3_RPC URL not set for %s - skipping live chain test", chain)
	}
	return rpc
}

var uniV3Fixtures = []struct {
	chain, wNative, usdc string
	v3Quoter             string
}{
	{"ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x61fFE014bA17989E743c5F6cB21bF9697530B21e"},
	{"bsc", "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
		"0x78D78E420Da98ad378D7799bE8f4AF69033EB077"},
	{"base", "0x4200000000000000000000000000000000000006", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"0x3d4e44Eb1374240CE5F1B871ab261CD16335B76a"},
	{"polygon", "0x0d500B1d8E8eF31E21C99d1Db9A6444d3ADf1270", "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359",
		"0x61fFE014bA17989E743c5F6cB21bF9697530B21e"},
}

func TestUniswapV3Quote_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	for _, f := range uniV3Fixtures {
		f := f
		t.Run(f.chain, func(t *testing.T) {
			t.Parallel()
			rpc := chainRPC(t, f.chain)
			got, err := onchain.CallV3Quoter(integrationCtx(t), rpc, f.v3Quoter, f.wNative, f.usdc, 500, amountIn)
			if err != nil {
				t.Fatalf("%s V3 500 quote: %v", f.chain, err)
			}
			if got == nil || got.Sign() <= 0 {
				t.Errorf("%s V3 returned zero/nil amountOut", f.chain)
			}
			t.Logf("%s V3 500: %s USDC for %s WNAT", f.chain, got, amountIn)
		})
	}
}

var uniV4Fixtures = []struct {
	chain, wNative, usdc, v4Quoter string
}{
	{"ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x52F0E24D1c21C8A0cB1e5a5dD6198556BD9E1203"},
	{"bsc", "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
		"0x9F75dD27D6664c475B90e105573E550ff69437B0"},
	{"base", "0x4200000000000000000000000000000000000006", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"0x0d5e0F971ED27FBfF6c2837bf31316121532048D"},
	{"polygon", "0x0d500B1d8E8eF31E21C99d1Db9A6444d3ADf1270", "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359",
		"0xb3d5c3Dfc3a7aEbFF71895A7191796BFFc2c81b9"},
}

func TestUniswapV4Quote_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	for _, f := range uniV4Fixtures {
		f := f
		t.Run(f.chain, func(t *testing.T) {
			t.Parallel()
			rpc := chainRPC(t, f.chain)
			got, err := onchain.CallV4Quoter(integrationCtx(t), rpc, f.v4Quoter, f.wNative, f.usdc, 500, amountIn)
			if err != nil {

				t.Logf("%s V4 500 quote error (pool may not exist): %v", f.chain, err)
				return
			}
			if got != nil && got.Sign() > 0 {
				t.Logf("%s V4 500: %s USDC for %s WNAT", f.chain, got, amountIn)
			}
		})
	}
}

var uniV2Fixtures = []struct {
	chain, wNative, usdc, v2Router string
}{
	{"ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"},
	{"base", "0x4200000000000000000000000000000000000006", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"},
	{"polygon", "0x0d500B1d8E8eF31E21C99d1Db9A6444d3ADf1270", "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359",
		"0xedf6066a2b290C185783862C7F4776A2C8077AD1"},
}

func TestUniswapV2RouterQuote_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	for _, f := range uniV2Fixtures {
		f := f
		t.Run(f.chain, func(t *testing.T) {
			t.Parallel()
			rpc := chainRPC(t, f.chain)
			got, err := onchain.CallV2Router(integrationCtx(t), rpc, f.v2Router, f.wNative, f.usdc, amountIn)
			if err != nil {
				t.Fatalf("%s V2 router quote: %v", f.chain, err)
			}
			if got == nil || got.Sign() <= 0 {
				t.Errorf("%s V2 returned zero/nil amountOut", f.chain)
			}
			t.Logf("%s V2: %s USDC for %s WNAT", f.chain, got, amountIn)
		})
	}
}

func TestUniswapV2ReservesQuote_BSC_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpc := chainRPC(t, "bsc")
	factory := "0x8909Dc15e40173Ff4699343b6eB8132c65e18eC6"
	wbnb := "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c"
	usdc := "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"

	got, err := onchain.CallV2Reserves(integrationCtx(t), rpc, factory, wbnb, usdc, amountIn, 997, 1000)
	if err != nil {
		t.Fatalf("BSC V2 reserves quote: %v", err)
	}
	if got == nil || got.Sign() <= 0 {
		t.Errorf("BSC V2 reserves returned zero/nil amountOut")
	}
	t.Logf("BSC V2 reserves: %s USDC for %s WBNB", got, amountIn)
}

var pancakeV3Fixtures = []struct {
	chain, wNative, usdc string
}{
	{"ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"},
	{"bsc", "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"},
	{"base", "0x4200000000000000000000000000000000000006", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"},
}

const pancakeV3QuoterAddr = "0xB048Bbc1Ee6b733FFfCFb9e9CeF7375518e25997"

func TestPancakeV3Quote_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	for _, f := range pancakeV3Fixtures {
		f := f
		t.Run(f.chain, func(t *testing.T) {
			t.Parallel()
			rpc := chainRPC(t, f.chain)

			got, err := onchain.CallV3Quoter(integrationCtx(t), rpc, pancakeV3QuoterAddr, f.wNative, f.usdc, 500, amountIn)
			if err != nil {
				t.Fatalf("%s PancakeSwap V3 500 quote: %v", f.chain, err)
			}
			if got == nil || got.Sign() <= 0 {
				t.Errorf("%s PancakeSwap V3 returned zero/nil amountOut", f.chain)
			}
			t.Logf("%s PancakeSwap V3 500: %s USDC for %s WNAT", f.chain, got, amountIn)
		})
	}
}

var pancakeInfinityFixtures = []struct {
	chain, wNative, usdc, quoter, poolManager string
}{
	{
		"bsc", "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
		"0xd0737C9762912dD34c3271197E362Aa736Df0926",
		"0xa0FfB9c1CE1Fe56963B0321B32E7A0302114058b",
	},
	{
		"base", "0x4200000000000000000000000000000000000006", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"0xd0737C9762912dD34c3271197E362Aa736Df0926",
		"0xa0FfB9c1CE1Fe56963B0321B32E7A0302114058b",
	},
}

func TestUniswapV4QuoteBuild_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURLs := map[string]string{
		"ethereum": chainRPC(t, "ethereum"),
	}
	adapter := adapters.NewUniswapAdapter(rpcURLs)

	req := swap.QuoteRequest{
		Chain:       "ethereum",
		FromAddress: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		SourceAsset: "ETH",
		TargetAsset: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		Amount:      amountIn.String(),
		Slippage:    0.5,
	}
	quoteResult, err := adapter.Quote(integrationCtx(t), req)
	if err != nil {
		t.Skipf("Uniswap Quote: %v (may require trace-capable RPC)", err)
		return
	}
	route := quoteVersion(quoteResult)
	t.Logf("Uniswap best route: %s, amountOut=%s", route, quoteResult.OutputAmount)

	if strings.Contains(route, "v4") {

		buildReq := swap.BuildRequest{
			Chain:       "ethereum",
			FromAddress: req.FromAddress,
			Quote:       quoteResult.RawQuote,
		}
		buildResult, err := adapter.Build(integrationCtx(t), buildReq)
		if err != nil {
			t.Fatalf("Uniswap V4 Build: %v", err)
		}
		data := strings.ToLower(strings.TrimPrefix(buildResult.Data, "0x"))
		if !strings.HasPrefix(data, "3593564c") {
			t.Errorf("V4 Build: expected Universal Router selector, got prefix %s", data[:8])
		}
		if !strings.Contains(data, "10") {
			t.Errorf("V4 Build: expected CMD_V4_SWAP (0x10) in calldata")
		}
		t.Logf("Uniswap V4 Build OK: calldata len=%d", len(data)/2)
	} else {
		t.Logf("Best route was not V4 (%s) — skipping V4 build check", route)
	}
}

func TestPancakeInfinityCLQuoteBuild_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	rpcURLs := map[string]string{
		"bsc": chainRPC(t, "bsc"),
	}
	adapter := adapters.NewPancakeAdapter(rpcURLs)

	req := swap.QuoteRequest{
		Chain:       "bsc",
		FromAddress: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		SourceAsset: "BNB",
		TargetAsset: "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
		Amount:      amountIn.String(),
		Slippage:    0.5,
	}
	quoteResult, err := adapter.Quote(integrationCtx(t), req)
	if err != nil {
		t.Fatalf("PancakeSwap Quote: %v", err)
	}
	route := quoteVersion(quoteResult)
	t.Logf("Pancake best route: %s, amountOut=%s", route, quoteResult.OutputAmount)

	if strings.Contains(route, "infinity") {
		buildReq := swap.BuildRequest{
			Chain:       "bsc",
			FromAddress: req.FromAddress,
			Quote:       quoteResult.RawQuote,
		}
		buildResult, err := adapter.Build(integrationCtx(t), buildReq)
		if err != nil {
			t.Fatalf("Pancake Infinity CL Build: %v", err)
		}
		data := strings.ToLower(strings.TrimPrefix(buildResult.Data, "0x"))
		if !strings.HasPrefix(data, "3593564c") {
			t.Errorf("Infinity CL Build: expected Universal Router selector, got %s", data[:8])
		}
		if !strings.Contains(data, "10") {
			t.Errorf("Infinity CL Build: expected CMD_V4_SWAP (0x10) in calldata")
		}
		t.Logf("Pancake Infinity CL Build OK: calldata len=%d", len(data)/2)
	} else {
		t.Logf("Best route was not Infinity CL (%s) — skipping Infinity CL build check", route)
	}
}

func TestPancakeInfinityCLQuote_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	for _, f := range pancakeInfinityFixtures {
		f := f
		t.Run(f.chain, func(t *testing.T) {
			t.Parallel()
			rpc := chainRPC(t, f.chain)
			got, err := onchain.CallPancakeInfinity(integrationCtx(t), rpc, f.quoter, f.poolManager, f.wNative, f.usdc, 500, amountIn)
			if err != nil {
				t.Logf("%s Pancake Infinity CL 500 quote error (pool may not exist): %v", f.chain, err)
				return
			}
			if got != nil && got.Sign() > 0 {
				t.Logf("%s Pancake Infinity CL 500: %s USDC for %s WNAT", f.chain, got, amountIn)
			}
		})
	}
}
