// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package onchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/blocksecteam/web3-companion/internal/token"
)

var ethHTTPPool = &http.Client{Timeout: 10 * time.Second}

func ethCall(ctx context.Context, rpcURL, to, calldata string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []interface{}{
			map[string]string{"to": to, "data": calldata},
			"latest",
		},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ethHTTPPool.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("eth_call error: %s", out.Error.Message)
	}
	return out.Result, nil
}

func keccak4(sig string) string {
	sum := crypto.Keccak256([]byte(sig))
	return fmt.Sprintf("%02x%02x%02x%02x", sum[0], sum[1], sum[2], sum[3])
}

var (
	selV3Quote = sync.OnceValue(func() string {
		return keccak4("quoteExactInputSingle((address,address,uint256,uint24,uint160))")
	})

	selV4Quote = sync.OnceValue(func() string {
		return keccak4("quoteExactInputSingle(((address,address,uint24,int24,address),bool,uint128,bytes))")
	})

	selV2GetAmountsOut = sync.OnceValue(func() string {
		return keccak4("getAmountsOut(uint256,address[])")
	})

	selGetPair = sync.OnceValue(func() string {
		return keccak4("getPair(address,address)")
	})

	selToken0 = sync.OnceValue(func() string {
		return keccak4("token0()")
	})

	selGetReserves = sync.OnceValue(func() string {
		return keccak4("getReserves()")
	})

	selPancakeInfinityQuote = sync.OnceValue(func() string {
		return keccak4("quoteExactInputSingle(((address,address,address,address,uint24,bytes32),bool,uint128,bytes))")
	})
)

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func encodeAddress(addr string) []byte {
	addr = strings.TrimPrefix(addr, "0x")
	b := make([]byte, 20)
	n, _ := new(big.Int).SetString(addr, 16)
	n.FillBytes(b)
	return pad32(b)
}

func encodeUint256(n *big.Int) []byte {
	b := make([]byte, 32)
	n.FillBytes(b)
	return b
}

func encodeUint24(fee int) []byte {
	b := make([]byte, 32)
	new(big.Int).SetInt64(int64(fee)).FillBytes(b)
	return b
}

func encodeInt24(v int) []byte {
	return encodeUint24(v)
}

func encodeBool(v bool) []byte {
	b := make([]byte, 32)
	if v {
		b[31] = 1
	}
	return b
}

func encodeUint128(n *big.Int) []byte {
	return encodeUint256(n)
}

func encodeBytes32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out, b)
	return out
}

func encodeEmptyBytesAtOffset(offset int) []byte {

	offsetWord := encodeUint256(big.NewInt(int64(offset)))
	length := make([]byte, 32)
	return append(offsetWord, length...)
}

func buildCalldata(sel string, args []byte) string {
	return "0x" + sel + fmt.Sprintf("%x", args)
}

func decodeUint256From(hex string, slot int) (*big.Int, error) {
	hex = strings.TrimPrefix(hex, "0x")
	start := slot * 64
	if len(hex) < start+64 {
		return nil, fmt.Errorf("result too short (%d chars) for slot %d", len(hex), slot)
	}
	n, ok := new(big.Int).SetString(hex[start:start+64], 16)
	if !ok {
		return nil, fmt.Errorf("invalid uint256 hex at slot %d", slot)
	}
	return n, nil
}

func decodeAddress(hex string, slot int) (string, error) {
	hex = strings.TrimPrefix(hex, "0x")
	start := slot * 64
	if len(hex) < start+64 {
		return "", fmt.Errorf("result too short")
	}
	return "0x" + hex[start+24:start+64], nil
}

func CallV3Quoter(ctx context.Context, rpcURL, quoterAddr, tokenIn, tokenOut string, fee int, amountIn *big.Int) (*big.Int, error) {

	args := concat(
		encodeAddress(tokenIn),
		encodeAddress(tokenOut),
		encodeUint256(amountIn),
		encodeUint24(fee),
		make([]byte, 32),
	)
	calldata := buildCalldata(selV3Quote(), args)
	result, err := ethCall(ctx, rpcURL, quoterAddr, calldata)
	if err != nil {
		return nil, err
	}
	return decodeUint256From(result, 0)
}

func CallV4Quoter(ctx context.Context, rpcURL, quoterAddr, tokenIn, tokenOut string, fee int, amountIn *big.Int) (*big.Int, error) {
	tickSpacing := V3FeeToTickSpacing(fee)

	tokenInNorm := normalizeForV4(tokenIn)
	tokenOutNorm := normalizeForV4(tokenOut)

	var currency0, currency1 string
	var zeroForOne bool
	inNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenInNorm, "0x"), 16)
	outNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenOutNorm, "0x"), 16)
	if inNum.Cmp(outNum) < 0 {
		currency0, currency1 = tokenInNorm, tokenOutNorm
		zeroForOne = true
	} else {
		currency0, currency1 = tokenOutNorm, tokenInNorm
		zeroForOne = false
	}

	bytesOffset := 8 * 32

	static := concat(
		encodeAddress(currency0),
		encodeAddress(currency1),
		encodeUint24(fee),
		encodeInt24(tickSpacing),
		encodeAddress(zeroAddr),
		encodeBool(zeroForOne),
		encodeUint128(amountIn),
		encodeUint256(big.NewInt(int64(bytesOffset))),
	)
	dynamic := make([]byte, 32)

	args := concat(static, dynamic)
	calldata := buildCalldata(selV4Quote(), args)
	result, err := ethCall(ctx, rpcURL, quoterAddr, calldata)
	if err != nil {
		return nil, err
	}

	return decodeUint256From(result, 0)
}

func CallV2Router(ctx context.Context, rpcURL, routerAddr, tokenIn, tokenOut string, amountIn *big.Int) (*big.Int, error) {

	pathOffset := 2 * 32
	args := concat(
		encodeUint256(amountIn),
		encodeUint256(big.NewInt(int64(pathOffset))),
		encodeUint256(big.NewInt(2)),
		encodeAddress(tokenIn),
		encodeAddress(tokenOut),
	)
	calldata := buildCalldata(selV2GetAmountsOut(), args)
	result, err := ethCall(ctx, rpcURL, routerAddr, calldata)
	if err != nil {
		return nil, err
	}

	return decodeUint256From(result, 3)
}

func CallV2Reserves(ctx context.Context, rpcURL, factoryAddr, tokenIn, tokenOut string, amountIn *big.Int, feeNumer, feeDenom int64) (*big.Int, error) {

	args := concat(encodeAddress(tokenIn), encodeAddress(tokenOut))
	result, err := ethCall(ctx, rpcURL, factoryAddr, buildCalldata(selGetPair(), args))
	if err != nil {
		return nil, err
	}
	pairAddr, err := decodeAddress(result, 0)
	if err != nil {
		return nil, err
	}
	if strings.TrimPrefix(strings.ToLower(pairAddr), "0x") == strings.Repeat("0", 40) {
		return nil, fmt.Errorf("pair does not exist")
	}

	result, err = ethCall(ctx, rpcURL, pairAddr, "0x"+selToken0())
	if err != nil {
		return nil, err
	}
	token0, err := decodeAddress(result, 0)
	if err != nil {
		return nil, err
	}

	result, err = ethCall(ctx, rpcURL, pairAddr, "0x"+selGetReserves())
	if err != nil {
		return nil, err
	}
	reserve0, err := decodeUint256From(result, 0)
	if err != nil {
		return nil, err
	}
	reserve1, err := decodeUint256From(result, 1)
	if err != nil {
		return nil, err
	}

	var reserveIn, reserveOut *big.Int
	if strings.EqualFold(tokenIn, token0) {
		reserveIn, reserveOut = reserve0, reserve1
	} else {
		reserveIn, reserveOut = reserve1, reserve0
	}
	if reserveIn.Sign() == 0 || reserveOut.Sign() == 0 {
		return nil, fmt.Errorf("empty reserves")
	}

	fee := big.NewInt(feeNumer)
	denom := big.NewInt(feeDenom)
	amountInWithFee := new(big.Int).Mul(amountIn, fee)
	numerator := new(big.Int).Mul(amountInWithFee, reserveOut)
	denominator := new(big.Int).Add(
		new(big.Int).Mul(reserveIn, denom),
		amountInWithFee,
	)
	return new(big.Int).Div(numerator, denominator), nil
}

func CallPancakeInfinity(ctx context.Context, rpcURL, quoterAddr, poolManagerAddr, tokenIn, tokenOut string, fee int, amountIn *big.Int) (*big.Int, error) {
	tickSpacing := PancakeTickSpacing(fee)

	params := make([]byte, 32)
	big.NewInt(int64(tickSpacing) << 16).FillBytes(params)

	var currency0, currency1 string
	var zeroForOne bool
	inNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenIn, "0x"), 16)
	outNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenOut, "0x"), 16)
	if inNum.Cmp(outNum) < 0 {
		currency0, currency1 = tokenIn, tokenOut
		zeroForOne = true
	} else {
		currency0, currency1 = tokenOut, tokenIn
		zeroForOne = false
	}

	bytesOffset := 9 * 32

	static := concat(
		encodeAddress(currency0),
		encodeAddress(currency1),
		encodeAddress(zeroAddr),
		encodeAddress(poolManagerAddr),
		encodeUint24(fee),
		encodeBytes32(params),
		encodeBool(zeroForOne),
		encodeUint128(amountIn),
		encodeUint256(big.NewInt(int64(bytesOffset))),
	)
	dynamic := make([]byte, 32)

	args := concat(static, dynamic)
	calldata := buildCalldata(selPancakeInfinityQuote(), args)
	result, err := ethCall(ctx, rpcURL, quoterAddr, calldata)
	if err != nil {
		return nil, err
	}

	return decodeUint256From(result, 0)
}

const zeroAddr = "0x0000000000000000000000000000000000000000"

func concat(slices ...[]byte) []byte {
	var total int
	for _, s := range slices {
		total += len(s)
	}
	out := make([]byte, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}

func normalizeForV4(tokenAddr string) string {
	if token.IsNativeToken(tokenAddr) && !strings.EqualFold(tokenAddr, zeroAddr) {
		return zeroAddr
	}
	return tokenAddr
}

func V3FeeToTickSpacing(fee int) int {
	switch fee {
	case 100:
		return 1
	case 500:
		return 10
	case 3000:
		return 60
	case 10000:
		return 200
	default:
		return 60
	}
}

func PancakeTickSpacing(fee int) int {
	switch fee {
	case 100:
		return 1
	case 500:
		return 10
	case 2500:
		return 50
	case 10000:
		return 200
	default:
		return 50
	}
}
