// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap/adapters/onchain"
)

func fakeRPCServer(t *testing.T, resultHex string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("want POST, got %s", r.Method)
		}
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["method"] != "eth_call" {

			fmt.Fprintln(w, `{"jsonrpc":"2.0","id":1,"result":"0x"}`)
			return
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"%s"}`, resultHex)
	}))
}

func fakeRPCServerError(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}`)
	}))
}

func TestCallV3Quoter_Success(t *testing.T) {

	amountOut := big.NewInt(2_300_000)

	result := make([]byte, 4*32)
	amountOut.FillBytes(result[0:32])
	hexResult := "0x" + fmt.Sprintf("%x", result)

	ts := fakeRPCServer(t, hexResult)
	defer ts.Close()

	quoter := "0x61fFE014bA17989E743c5F6cB21bF9697530B21e"
	tokenIn := "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	tokenOut := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	got, err := onchain.CallV3Quoter(context.Background(), ts.URL, quoter, tokenIn, tokenOut, 500, big.NewInt(1_000_000_000_000_000))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Cmp(amountOut) != 0 {
		t.Errorf("got %s, want %s", got, amountOut)
	}
}

func TestCallV3Quoter_Revert(t *testing.T) {
	ts := fakeRPCServerError(t)
	defer ts.Close()

	_, err := onchain.CallV3Quoter(context.Background(), ts.URL,
		"0x61fFE014bA17989E743c5F6cB21bF9697530B21e",
		"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		500, big.NewInt(1_000_000_000_000_000))
	if err == nil {
		t.Fatal("expected error on RPC revert")
	}
}

func TestCallV4Quoter_Success(t *testing.T) {

	amountOut := big.NewInt(2_290_000)
	result := make([]byte, 2*32)
	amountOut.FillBytes(result[0:32])
	hexResult := "0x" + fmt.Sprintf("%x", result)

	ts := fakeRPCServer(t, hexResult)
	defer ts.Close()

	quoter := "0x52F0E24D1c21C8A0cB1e5a5dD6198556BD9E1203"
	tokenIn := "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	tokenOut := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	got, err := onchain.CallV4Quoter(context.Background(), ts.URL, quoter, tokenIn, tokenOut, 500, big.NewInt(1_000_000_000_000_000))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Cmp(amountOut) != 0 {
		t.Errorf("got %s, want %s", got, amountOut)
	}
}

func TestCallV2Router_Success(t *testing.T) {

	amountOut := big.NewInt(2_280_000)
	amountIn := big.NewInt(1_000_000_000_000_000)

	result := make([]byte, 4*32)
	big.NewInt(0x20).FillBytes(result[0:32])
	big.NewInt(2).FillBytes(result[32:64])
	amountIn.FillBytes(result[64:96])
	amountOut.FillBytes(result[96:128])
	hexResult := "0x" + fmt.Sprintf("%x", result)

	ts := fakeRPCServer(t, hexResult)
	defer ts.Close()

	router := "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"
	tokenIn := "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	tokenOut := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	got, err := onchain.CallV2Router(context.Background(), ts.URL, router, tokenIn, tokenOut, amountIn)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Cmp(amountOut) != 0 {
		t.Errorf("got %s, want %s", got, amountOut)
	}
}

func TestCallV2ReservesQuote_Success(t *testing.T) {

	callCount := 0

	pairAddr := "0x1234567890123456789012345678901234567890"
	token0 := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)

		callParams := req["params"].([]interface{})
		callObj := callParams[0].(map[string]interface{})
		data := strings.ToLower(callObj["data"].(string))

		switch {
		case strings.HasPrefix(data, "0xe6a43905"):
			addrBytes := make([]byte, 32)
			copy(addrBytes[12:], common20(pairAddr))
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, addrBytes)
		case strings.HasPrefix(data, "0x0dfe1681"):
			addrBytes := make([]byte, 32)
			copy(addrBytes[12:], common20(token0))
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, addrBytes)
		case strings.HasPrefix(data, "0x0902f1ac"):

			result := make([]byte, 3*32)
			big.NewInt(500_000_000_000).FillBytes(result[0:32])
			reserve1, _ := new(big.Int).SetString("200000000000000000000", 10)
			reserve1.FillBytes(result[32:64])
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, result)
		default:
			fmt.Fprintln(w, `{"jsonrpc":"2.0","id":1,"result":"0x"}`)
		}
	}))
	defer ts.Close()

	factory := "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f"
	tokenIn := "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	tokenOut := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"

	amountIn := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	got, err := onchain.CallV2Reserves(context.Background(), ts.URL, factory, tokenIn, tokenOut, amountIn, 997, 1000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.Sign() <= 0 {
		t.Errorf("expected positive amountOut, got %v", got)
	}
}

func common20(addr string) []byte {
	addr = strings.TrimPrefix(addr, "0x")
	b := make([]byte, 20)
	for i := 0; i < 20; i++ {
		fmt.Sscanf(addr[i*2:i*2+2], "%02x", &b[i])
	}
	return b
}
