// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/swap/adapters/onchain"
	"github.com/blocksecteam/web3-companion/internal/token"
)

var pancakeV2Router = map[int]string{
	1:    "0xEfF92A263d31888d860bD50809A8D171709b7b1c",
	56:   "0x10ED43C718714eb63d5aA57B78B54704E256024E",
	8453: "0x8cFe327CEc66d1C090Dd72bd0FF11d690C33a2Eb",
}

var pancakeV3Router = map[int]string{
	1:    "0x13f4EA83D0bd40E75C8222255bc855a974568Dd4",
	56:   "0x13f4EA83D0bd40E75C8222255bc855a974568Dd4",
	8453: "0x678Aa4bF4E210cf2166753e054d5b7c31cc7fa86",
}

type PancakeAdapter struct {
	rpcURLs     map[string]string
	rpcProvider func() map[string]string
}

func NewPancakeAdapter(rpcURLs map[string]string) *PancakeAdapter {
	return NewPancakeAdapterWithRPCProvider(func() map[string]string {
		return rpcURLs
	})
}

func NewPancakeAdapterWithRPCProvider(provider func() map[string]string) *PancakeAdapter {
	return &PancakeAdapter{rpcProvider: provider}
}

func (a *PancakeAdapter) Name() string { return "pancake" }

func (a *PancakeAdapter) SupportedChains() []string {
	return []string{"ethereum", "bsc", "base"}
}

// Capabilities: PancakeSwap router accepts/outputs both native and WETH on
// both sides, and enforces amountOutMin on-chain so user-passed slippage
// maps to the amountOutMin baked into the calldata.
func (a *PancakeAdapter) Capabilities() swap.SwapCapabilities {
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

func (a *PancakeAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	chainID, ok := chainID(req.Chain)
	if !ok || pancakeV3Router[chainID] == "" {
		return nil, fmt.Errorf("pancake: unsupported chain %s", req.Chain)
	}
	rpcURL, ok := a.currentRPCURLs()[strings.ToLower(req.Chain)]
	if !ok {
		return nil, fmt.Errorf("pancake: no RPC URL configured for chain %s", req.Chain)
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
	_, amountOut, routeParams, err := onchain.BestPancakeRoute(ctx, rpcURL, chainID, tokenIn, tokenOut, amountIn)
	if err != nil {
		return nil, fmt.Errorf("pancake: %w", err)
	}

	slipBPS := int64(slippagePercent(req.Slippage) * 100)
	amountOutMin := new(big.Int).Mul(amountOut, big.NewInt(10000-slipBPS))
	amountOutMin.Div(amountOutMin, big.NewInt(10000))

	// Extract individual fields from routeParams
	version := ""
	fee := 0
	tickSpacing := 0
	c0, c1, poolManager := "", "", ""
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
		poolManager, _ = routeParams["pool_manager"].(string)
	}
	// If version is infinity_cl and pool_manager was not obtained from routeParams, look it up by chain ID
	if poolManager == "" && version == "pancake_infinity_cl" {
		poolManager = onchain.PancakeInfinityCLPoolManager[chainID]
	}

	router := pancakeV3Router[chainID]
	if version == "pancake_v2" {
		router = pancakeV2Router[chainID]
	}

	raw := map[string]interface{}{
		"chain_id":       chainID,
		"token_in":       tokenIn,
		"token_out":      tokenOut,
		"amount_in":      req.Amount,
		"amount_out_min": amountOutMin.String(),
		"deadline":       deadlineSeconds(),
		"is_native_in":   isNativeIn,
		"is_native_out":  isNativeOut,
		"swap_router":    router,
		"weth":           weth,
		"sender":         req.FromAddress,
		"version":        version,
		"fee":            fee,
		"tick_spacing":   tickSpacing,
		"currency0":      c0,
		"currency1":      c1,
		"zero_for_one":   zfo,
		"pool_manager":   poolManager,
	}
	return &swap.QuoteResult{
		Protocol:      "pancake",
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  amountOut.String(),
		ApproveStyle:  "erc20",
		ApproveTarget: router,
		RawQuote:      raw,
	}, nil
}

func (a *PancakeAdapter) Build(ctx context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("pancake: invalid quote format")
	}
	data, value, err := buildPancakeCalldata(raw)
	if err != nil {
		return nil, err
	}
	router := stringValue(raw, "swap_router")
	if router == "" {
		return nil, fmt.Errorf("pancake: quote missing swap_router")
	}
	return &swap.BuildResult{
		To:       router,
		Value:    hexFromString(value.String()),
		Data:     data,
		GasLimit: defaultGasLimit,
	}, nil
}

func (a *PancakeAdapter) currentRPCURLs() map[string]string {
	if a == nil {
		return nil
	}
	if a.rpcProvider != nil {
		return a.rpcProvider()
	}
	return a.rpcURLs
}

func buildPancakeCalldata(raw map[string]interface{}) (string, *big.Int, error) {
	amountIn := bigString(stringValue(raw, "amount_in"))
	amountOutMin := bigString(stringValue(raw, "amount_out_min"))
	deadline := big.NewInt(int64(intValue(raw, "deadline")))
	isNativeIn := boolValue(raw, "is_native_in")
	isNativeOut := boolValue(raw, "is_native_out")
	tokenIn := common.HexToAddress(stringValue(raw, "token_in"))
	tokenOut := common.HexToAddress(stringValue(raw, "token_out"))
	weth := common.HexToAddress(stringValue(raw, "weth"))
	sender := common.HexToAddress(stringValue(raw, "sender"))
	// Required route metadata: onchain.BestPancakeRoute always populates version, and
	// fee for v3/infinity_cl paths. Missing values here mean the quote crossed
	// a serialization boundary and lost data, or routing returned a malformed
	// meta — surface that as a hard error rather than silently defaulting to
	// pancake_v3 + 500-bps, which would route through the wrong pool.
	version := stringValue(raw, "version")
	if version == "" {
		return "", nil, fmt.Errorf("pancake: quote missing version field")
	}
	fee := intValue(raw, "fee")
	if fee == 0 && version != "pancake_v2" {
		return "", nil, fmt.Errorf("pancake: quote missing fee field for version %s", version)
	}
	chainID := intValue(raw, "chain_id")
	commands := make([]byte, 0, 3)
	inputs := make([][]byte, 0, 3)

	switch version {
	case "pancake_v2":
		if isNativeIn {
			path := []common.Address{weth, tokenOut}
			return selector("7ff36ab5", packArgs([]string{"uint256", "address[]", "address", "uint256"}, amountOutMin, path, sender, deadline)), amountIn, nil
		}
		if isNativeOut {
			path := []common.Address{tokenIn, weth}
			return selector("18cbafe5", packArgs([]string{"uint256", "uint256", "address[]", "address", "uint256"}, amountIn, amountOutMin, path, sender, deadline)), big.NewInt(0), nil
		}
		path := []common.Address{tokenIn, tokenOut}
		return selector("38ed1739", packArgs([]string{"uint256", "uint256", "address[]", "address", "uint256"}, amountIn, amountOutMin, path, sender, deadline)), big.NewInt(0), nil
	case "pancake_v3":
		swapRecipient := sender
		if isNativeOut {
			// swap outputs WETH to router, then router unwraps via multicall
			swapRecipient = common.HexToAddress(addressThis)
		}
		packed := packV3ExactInputSingle(tokenIn, tokenOut, fee, swapRecipient, amountIn, amountOutMin)
		if isNativeOut {
			swapCalldata := append(common.Hex2Bytes("04e45aaf"), packed...)
			unwrapCalldata := append(
				common.Hex2Bytes("49404b7c"),
				packArgs([]string{"uint256", "address"}, amountOutMin, sender)...,
			)
			multicallData := packArgs([]string{"uint256", "bytes[]"}, deadline, [][]byte{swapCalldata, unwrapCalldata})
			return selector("5ae401dc", multicallData), big.NewInt(0), nil
		}
		if isNativeIn {
			return selector("04e45aaf", packed), amountIn, nil
		}
		return selector("04e45aaf", packed), big.NewInt(0), nil
	case "pancake_infinity_cl":
		ts := intValue(raw, "tick_spacing")
		if ts == 0 {
			ts = onchain.PancakeTickSpacing(fee)
		}
		c0 := common.HexToAddress(stringValue(raw, "currency0"))
		c1 := common.HexToAddress(stringValue(raw, "currency1"))
		zfoRaw := raw["zero_for_one"]
		zfo, _ := zfoRaw.(bool)

		pmAddr := stringValue(raw, "pool_manager")
		if pmAddr == "" {
			pmAddr = onchain.PancakeInfinityCLPoolManager[chainID]
		}
		if pmAddr == "" {
			return "", nil, fmt.Errorf("pancake: pancake_infinity_cl quote missing pool_manager (chain %d)", chainID)
		}
		poolManager := common.HexToAddress(pmAddr)

		var currencyIn, currencyOut common.Address
		if isNativeIn {
			currencyIn = weth
		} else {
			currencyIn = tokenIn
		}
		if isNativeOut {
			currencyOut = weth
		} else {
			currencyOut = tokenOut
		}

		if isNativeIn {
			commands = append(commands, cmdWrapETH)
			inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(addressThis), amountIn))
		}

		infiInput := onchain.EncodePancakeInfinitySwapInput(
			c0, c1, fee, ts, poolManager, zfo,
			amountIn, amountOutMin,
			currencyIn, currencyOut,
		)
		commands = append(commands, onchain.CmdV4SwapByte)
		inputs = append(inputs, infiInput)

		if isNativeOut {
			commands = append(commands, cmdUnwrapWETH)
			inputs = append(inputs, packArgs([]string{"address", "uint256"}, common.HexToAddress(msgSender), amountOutMin))
		}

		txValue := big.NewInt(0)
		if isNativeIn {
			txValue = amountIn
		}
		return encodeUniversalRouterExecute(commands, inputs, deadline), txValue, nil
	default:
		return "", nil, fmt.Errorf("pancake: unsupported route version %s", version)
	}
}

func packV3ExactInputSingle(tokenIn, tokenOut common.Address, fee int, recipient common.Address, amountIn, amountOutMin *big.Int) []byte {
	paramType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "tokenIn", Type: "address"},
		{Name: "tokenOut", Type: "address"},
		{Name: "fee", Type: "uint24"},
		{Name: "recipient", Type: "address"},
		{Name: "amountIn", Type: "uint256"},
		{Name: "amountOutMinimum", Type: "uint256"},
		{Name: "sqrtPriceLimitX96", Type: "uint160"},
	})
	args := abi.Arguments{{Type: paramType}}
	type params struct {
		TokenIn           common.Address
		TokenOut          common.Address
		Fee               *big.Int
		Recipient         common.Address
		AmountIn          *big.Int
		AmountOutMinimum  *big.Int
		SqrtPriceLimitX96 *big.Int
	}
	packed, err := args.Pack(params{
		TokenIn: tokenIn, TokenOut: tokenOut,
		Fee: big.NewInt(int64(fee)), Recipient: recipient,
		AmountIn: amountIn, AmountOutMinimum: amountOutMin,
		SqrtPriceLimitX96: big.NewInt(0),
	})
	if err != nil {
		panic(fmt.Sprintf("packV3ExactInputSingle: %v", err))
	}
	return packed
}
