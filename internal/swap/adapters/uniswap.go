// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/swap/adapters/onchain"
	"github.com/blocksecteam/web3-companion/internal/token"
)

const (
	msgSender   = "0x0000000000000000000000000000000000000001"
	addressThis = "0x0000000000000000000000000000000000000002"

	cmdV3SwapExactIn = byte(0x00)
	cmdV2SwapExactIn = byte(0x08)
	cmdWrapETH       = byte(0x0b)
	cmdUnwrapWETH    = byte(0x0c)
)

var uniswapUniversalRouter = map[int]string{
	1:     "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
	56:    "0x1906c1d672b88cD1B9aC7593301cA990F94Eae07",
	8453:  "0x6fF5693b99212Da76ad316178A184AB56D299b43",
	137:   "0x1095692A6237d83C6a72F3F5eFEdb9A670C49223",
	42161: "0x5E325eDA8064b456f4781070C0738d849c824258",
	10:    "0x851116D9223fabED8E56C0E6b8Ad0c31d98B3507",
}

type UniswapAdapter struct {
	rpcURLs     map[string]string
	rpcProvider func() map[string]string
}

func NewUniswapAdapter(rpcURLs map[string]string) *UniswapAdapter {
	return NewUniswapAdapterWithRPCProvider(func() map[string]string {
		return rpcURLs
	})
}

func NewUniswapAdapterWithRPCProvider(provider func() map[string]string) *UniswapAdapter {
	return &UniswapAdapter{rpcProvider: provider}
}

func (a *UniswapAdapter) Name() string { return "uniswap" }

// SupportedChains lists the chains this adapter routes on, aligned with
// the deployment chain whitelist in docker/config.toml. Uniswap also has
// routers on arbitrum/optimism upstream, but those chains are not enabled
// in this project — listing them here would mislead the /swap/protocols
// surface without any user-reachable effect. Extend only after adding the
// chain to config.toml.
func (a *UniswapAdapter) SupportedChains() []string {
	return []string{"ethereum", "base", "polygon", "bsc"}
}

// Capabilities: Universal Router accepts/outputs both native and WETH on both
// sides, and enforces amountOutMin on-chain so user-passed slippage maps to
// the amountOutMin baked into the calldata.
func (a *UniswapAdapter) Capabilities() swap.SwapCapabilities {
	return swap.SwapCapabilities{
		Native: swap.NativeCapability{
			AcceptsNativeIn: true,
			AcceptsWETHIn:   true,
			OutputsNative:   true,
			OutputsWETH:     true,
		},
		SupportsSlippage: true,
	}
}

func (a *UniswapAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	cid, ok := chainID(req.Chain)
	if !ok {
		return nil, fmt.Errorf("uniswap: unsupported chain %s", req.Chain)
	}
	router := uniswapUniversalRouter[cid]
	if router == "" {
		return nil, fmt.Errorf("uniswap: unsupported chain %s", req.Chain)
	}
	rpcURL, ok := a.currentRPCURLs()[strings.ToLower(req.Chain)]
	if !ok {
		return nil, fmt.Errorf("uniswap: no RPC URL configured for chain %s", req.Chain)
	}

	weth := wrappedNativeForChain(req.Chain)
	isNativeIn := token.IsNativeToken(req.SourceAsset)
	isNativeOut := token.IsNativeToken(req.TargetAsset)
	tokenIn := req.SourceAsset
	tokenOut := req.TargetAsset
	if isNativeIn {
		tokenIn = weth
	}
	if isNativeOut {
		tokenOut = weth
	}

	amountIn := bigString(req.Amount)
	_, amountOut, routeParams, err := onchain.BestUniswapRoute(ctx, rpcURL, cid, tokenIn, tokenOut, amountIn)
	if err != nil {
		return nil, fmt.Errorf("uniswap: %w", err)
	}

	slipBPS := int64(slippagePercent(req.Slippage) * 100)
	amountOutMin := new(big.Int).Mul(amountOut, big.NewInt(10000-slipBPS))
	amountOutMin.Div(amountOutMin, big.NewInt(10000))

	// Extract individual fields from routeParams (different versions populate different subsets)
	version := ""
	fee := 0
	tickSpacing := 0
	c0, c1 := "", ""
	var zfo bool
	if routeParams != nil {
		if v, ok := routeParams["version"]; ok {
			version, _ = v.(string)
		}
		fee = intValue(routeParams, "fee")
		tickSpacing = intValue(routeParams, "tick_spacing")
		c0, _ = routeParams["currency0"].(string)
		c1, _ = routeParams["currency1"].(string)
		if zfoRaw, ok := routeParams["zero_for_one"]; ok {
			zfo, _ = zfoRaw.(bool)
		}
	}

	raw := map[string]interface{}{
		"token_in":       tokenIn,
		"token_out":      tokenOut,
		"amount_in":      req.Amount,
		"amount_out_min": amountOutMin.String(),
		"deadline":       deadlineSeconds(),
		"is_native_in":   isNativeIn,
		"is_native_out":  isNativeOut,
		"swap_router":    router,
		"weth":           weth,
		"version":        version,
		"fee":            fee,
		"tick_spacing":   tickSpacing,
		"currency0":      c0,
		"currency1":      c1,
		"zero_for_one":   zfo,
	}
	return &swap.QuoteResult{
		Protocol:      "uniswap",
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  amountOut.String(),
		ApproveStyle:  "permit2",
		ApproveTarget: token.Permit2Address,
		RawQuote:      raw,
	}, nil
}

func (a *UniswapAdapter) Build(ctx context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("uniswap: invalid quote format")
	}
	data, value, err := buildUniswapCalldata(raw)
	if err != nil {
		return nil, err
	}
	router := stringValue(raw, "swap_router")
	if router == "" {
		return nil, fmt.Errorf("uniswap: quote missing swap_router")
	}
	return &swap.BuildResult{
		To:       router,
		Value:    hexFromString(value.String()),
		Data:     data,
		GasLimit: defaultGasLimit,
	}, nil
}

func (a *UniswapAdapter) currentRPCURLs() map[string]string {
	if a == nil {
		return nil
	}
	if a.rpcProvider != nil {
		return a.rpcProvider()
	}
	return a.rpcURLs
}

func buildUniswapCalldata(raw map[string]interface{}) (string, *big.Int, error) {
	amountIn := bigString(stringValue(raw, "amount_in"))
	amountOutMin := bigString(stringValue(raw, "amount_out_min"))
	deadline := big.NewInt(int64(intValue(raw, "deadline")))
	isNativeIn := boolValue(raw, "is_native_in")
	isNativeOut := boolValue(raw, "is_native_out")
	tokenIn := common.HexToAddress(stringValue(raw, "token_in"))
	tokenOut := common.HexToAddress(stringValue(raw, "token_out"))
	weth := common.HexToAddress(stringValue(raw, "weth"))
	// Required route metadata: onchain.BestUniswapRoute always populates version, and
	// version-specific fields (fee for v3/v4; tick_spacing for v4 is derived
	// from fee when missing). Missing values here mean the quote crossed a
	// serialization boundary and lost data, or routing returned a malformed
	// meta — surface that as a hard error rather than silently defaulting to
	// v3 + 3000-bps, which would route through the wrong pool.
	version := stringValue(raw, "version")
	if version == "" {
		return "", nil, fmt.Errorf("uniswap: quote missing version field")
	}
	fee := intValue(raw, "fee")
	if fee == 0 && version != "v2" {
		return "", nil, fmt.Errorf("uniswap: quote missing fee field for version %s", version)
	}

	commands := make([]byte, 0, 3)
	inputs := make([][]byte, 0, 3)
	needsUnwrap := isNativeOut
	swapRecipient := common.HexToAddress(msgSender)
	if needsUnwrap {
		swapRecipient = common.HexToAddress(addressThis)
	}

	if version == "v4" {
		tickSpacing := intValue(raw, "tick_spacing")
		if tickSpacing == 0 {
			tickSpacing = onchain.V3FeeToTickSpacing(fee)
		}
		c0Str := stringValue(raw, "currency0")
		c1Str := stringValue(raw, "currency1")
		zfoRaw := raw["zero_for_one"]
		zfo, _ := zfoRaw.(bool)
		c0 := common.HexToAddress(c0Str)
		c1 := common.HexToAddress(c1Str)

		if isNativeIn {
			commands = append(commands, cmdWrapETH)
			inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(addressThis), amountIn))
		}
		v4Input := onchain.EncodeUniV4SwapInput(c0, c1, fee, tickSpacing, zfo, amountIn, amountOutMin)
		commands = append(commands, onchain.CmdV4SwapByte)
		inputs = append(inputs, v4Input)
		if needsUnwrap {
			commands = append(commands, cmdUnwrapWETH)
			inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(msgSender), amountOutMin))
		}
		txValue := big.NewInt(0)
		if isNativeIn {
			txValue = amountIn
		}
		return encodeUniversalRouterExecute(commands, inputs, deadline), txValue, nil
	}

	if isNativeIn {
		commands = append(commands, cmdWrapETH)
		inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(addressThis), amountIn))
		if version == "v2" {
			commands = append(commands, cmdV2SwapExactIn)
			inputs = append(inputs, encodeV2SwapInput(swapRecipient, amountIn, amountOutMin, []common.Address{weth, tokenOut}, false))
		} else {
			commands = append(commands, cmdV3SwapExactIn)
			inputs = append(inputs, encodeV3SwapInput(swapRecipient, weth, tokenOut, fee, amountIn, amountOutMin, false))
		}
		if needsUnwrap {
			commands = append(commands, cmdUnwrapWETH)
			inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(msgSender), amountOutMin))
		}
		return encodeUniversalRouterExecute(commands, inputs, deadline), amountIn, nil
	}

	if version == "v2" {
		commands = append(commands, cmdV2SwapExactIn)
		inputs = append(inputs, encodeV2SwapInput(swapRecipient, amountIn, amountOutMin, []common.Address{tokenIn, tokenOut}, true))
	} else {
		commands = append(commands, cmdV3SwapExactIn)
		inputs = append(inputs, encodeV3SwapInput(swapRecipient, tokenIn, tokenOut, fee, amountIn, amountOutMin, true))
	}
	if needsUnwrap {
		commands = append(commands, cmdUnwrapWETH)
		inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(msgSender), amountOutMin))
	}
	return encodeUniversalRouterExecute(commands, inputs, deadline), big.NewInt(0), nil
}

func encodeV2SwapInput(recipient common.Address, amountIn, amountOutMin *big.Int, path []common.Address, payerIsUser bool) []byte {
	return packArgs([]string{"address", "uint256", "uint256", "address[]", "bool"}, recipient, amountIn, amountOutMin, path, payerIsUser)
}

func encodeV3SwapInput(recipient, tokenIn, tokenOut common.Address, fee int, amountIn, amountOutMin *big.Int, payerIsUser bool) []byte {
	path := make([]byte, 0, 43)
	path = append(path, tokenIn.Bytes()...)
	path = append(path, byte(fee>>16), byte(fee>>8), byte(fee))
	path = append(path, tokenOut.Bytes()...)
	return packArgs([]string{"address", "uint256", "uint256", "bytes", "bool"}, recipient, amountIn, amountOutMin, path, payerIsUser)
}

func encodeUniversalRouterExecute(commands []byte, inputs [][]byte, deadline *big.Int) string {
	return selector("3593564c", packArgs([]string{"bytes", "bytes[]", "uint256"}, commands, inputs, deadline))
}
