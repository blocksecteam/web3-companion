// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package onchain

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const CmdV4SwapByte = byte(0x10)

var v4InnerActions = []byte{0x06, 0x0C, 0x0F}

func encodeV4Actions(actions []byte, params [][]byte) []byte {
	bt, _ := abi.NewType("bytes", "", nil)
	bst, _ := abi.NewType("bytes[]", "", nil)
	args := abi.Arguments{{Type: bt}, {Type: bst}}
	out, _ := args.Pack(actions, params)
	return out
}

func encodeSettleOrTakeParam(currency common.Address, amount *big.Int) []byte {
	at, _ := abi.NewType("address", "", nil)
	ut, _ := abi.NewType("uint256", "", nil)
	args := abi.Arguments{{Type: at}, {Type: ut}}
	out, _ := args.Pack(currency, amount)
	return out
}

func EncodeUniV4SwapInput(
	currency0, currency1 common.Address,
	fee, tickSpacing int,
	zeroForOne bool,
	amountIn, amountOutMin *big.Int,
) []byte {
	poolKeyType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "Currency0", Type: "address"},
		{Name: "Currency1", Type: "address"},
		{Name: "Fee", Type: "uint24"},
		{Name: "TickSpacing", Type: "int24"},
		{Name: "Hooks", Type: "address"},
	})
	swapParamType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "PoolKey", Type: "tuple", Components: []abi.ArgumentMarshaling{
			{Name: "Currency0", Type: "address"},
			{Name: "Currency1", Type: "address"},
			{Name: "Fee", Type: "uint24"},
			{Name: "TickSpacing", Type: "int24"},
			{Name: "Hooks", Type: "address"},
		}},
		{Name: "ZeroForOne", Type: "bool"},
		{Name: "AmountIn", Type: "uint128"},
		{Name: "AmountOutMinimum", Type: "uint128"},
		{Name: "HookData", Type: "bytes"},
	})
	_ = poolKeyType

	swapArgs := abi.Arguments{{Type: swapParamType}}
	swapParam, _ := swapArgs.Pack(map[string]interface{}{
		"PoolKey": map[string]interface{}{
			"Currency0":   currency0,
			"Currency1":   currency1,
			"Fee":         big.NewInt(int64(fee)),
			"TickSpacing": big.NewInt(int64(tickSpacing)),
			"Hooks":       common.HexToAddress(zeroAddr),
		},
		"ZeroForOne":       zeroForOne,
		"AmountIn":         amountIn,
		"AmountOutMinimum": amountOutMin,
		"HookData":         []byte{},
	})

	var currencyIn, currencyOut common.Address
	if zeroForOne {
		currencyIn, currencyOut = currency0, currency1
	} else {
		currencyIn, currencyOut = currency1, currency0
	}

	return encodeV4Actions(v4InnerActions, [][]byte{
		swapParam,
		encodeSettleOrTakeParam(currencyIn, amountIn),
		encodeSettleOrTakeParam(currencyOut, amountOutMin),
	})
}

func EncodePancakeInfinitySwapInput(
	currency0, currency1 common.Address,
	fee, tickSpacing int,
	poolManager common.Address,
	zeroForOne bool,
	amountIn, amountOutMin *big.Int,
	currencyIn, currencyOut common.Address,
) []byte {

	params := make([]byte, 32)
	new(big.Int).Lsh(big.NewInt(int64(tickSpacing)), 16).FillBytes(params)
	var params32 [32]byte
	copy(params32[:], params)

	swapParamType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "PoolKey", Type: "tuple", Components: []abi.ArgumentMarshaling{
			{Name: "Currency0", Type: "address"},
			{Name: "Currency1", Type: "address"},
			{Name: "Hooks", Type: "address"},
			{Name: "PoolManager", Type: "address"},
			{Name: "Fee", Type: "uint24"},
			{Name: "Parameters", Type: "bytes32"},
		}},
		{Name: "ZeroForOne", Type: "bool"},
		{Name: "ExactAmount", Type: "uint128"},
		{Name: "AmountOutMinimum", Type: "uint128"},
		{Name: "HookData", Type: "bytes"},
	})

	swapArgs := abi.Arguments{{Type: swapParamType}}
	swapParam, _ := swapArgs.Pack(map[string]interface{}{
		"PoolKey": map[string]interface{}{
			"Currency0":   currency0,
			"Currency1":   currency1,
			"Hooks":       common.HexToAddress(zeroAddr),
			"PoolManager": poolManager,
			"Fee":         big.NewInt(int64(fee)),
			"Parameters":  params32,
		},
		"ZeroForOne":       zeroForOne,
		"ExactAmount":      amountIn,
		"AmountOutMinimum": amountOutMin,
		"HookData":         []byte{},
	})

	return encodeV4Actions(v4InnerActions, [][]byte{
		swapParam,
		encodeSettleOrTakeParam(currencyIn, amountIn),
		encodeSettleOrTakeParam(currencyOut, amountOutMin),
	})
}

func resolveV4PoolKey(tokenIn, tokenOut string) (c0, c1 common.Address, zeroForOne bool) {
	ti := common.HexToAddress(tokenIn)
	to := common.HexToAddress(tokenOut)
	inNum := new(big.Int).SetBytes(ti.Bytes())
	outNum := new(big.Int).SetBytes(to.Bytes())
	if inNum.Cmp(outNum) < 0 {
		return ti, to, true
	}
	return to, ti, false
}

func normNativeForV4(addr string) string {
	addr = strings.ToLower(strings.TrimPrefix(addr, "0x"))
	if addr == strings.Repeat("e", 40) || addr == strings.Repeat("0", 40) {
		return zeroAddr
	}
	return "0x" + addr
}
