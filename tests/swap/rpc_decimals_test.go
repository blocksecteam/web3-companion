// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

const usdcDecimalsHexResult = "0x0000000000000000000000000000000000000000000000000000000000000006"

const wbtcDecimalsHexResult = "0x0000000000000000000000000000000000000000000000000000000000000008"

func fakeRPC(t *testing.T, response string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("malformed JSON-RPC request: %v", err)
		}
		if req["method"] != "eth_call" {
			t.Errorf("method = %v, want eth_call", req["method"])
		}
		params, ok := req["params"].([]interface{})
		if !ok || len(params) < 1 {
			t.Errorf("malformed params: %v", req["params"])
		} else {
			call, _ := params[0].(map[string]interface{})
			if data, _ := call["data"].(string); !strings.EqualFold(data, "0x313ce567") {
				t.Errorf("call.data = %q, want decimals() selector 0x313ce567", data)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
}

func TestRPCDecimals_USDC(t *testing.T) {
	ts := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"result":"`+usdcDecimalsHexResult+`"}`, 200)
	defer ts.Close()

	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": ts.URL}, http.DefaultClient)
	d, err := src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d != 6 {
		t.Errorf("got %d, want 6", d)
	}
}

func TestRPCDecimalsUsesLatestRPCProviderSnapshot(t *testing.T) {
	first := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"result":"`+usdcDecimalsHexResult+`"}`, 200)
	defer first.Close()
	second := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"result":"`+wbtcDecimalsHexResult+`"}`, 200)
	defer second.Close()
	current := map[string]string{"ethereum": first.URL}
	src := swap.NewRPCDecimalsSourceWithProvider(func() map[string]string {
		return current
	}, http.DefaultClient)

	d, err := src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err != nil {
		t.Fatalf("first decimals err = %v", err)
	}
	if d != 6 {
		t.Fatalf("first decimals = %d, want 6", d)
	}
	current = map[string]string{"ethereum": second.URL}
	d, err = src.Decimals(context.Background(), "ethereum", "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599")
	if err != nil {
		t.Fatalf("second decimals err = %v", err)
	}
	if d != 8 {
		t.Fatalf("second decimals = %d, want 8", d)
	}
}

func TestRPCDecimals_WBTC(t *testing.T) {
	ts := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"result":"`+wbtcDecimalsHexResult+`"}`, 200)
	defer ts.Close()

	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": ts.URL}, http.DefaultClient)
	d, err := src.Decimals(context.Background(), "ethereum", "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d != 8 {
		t.Errorf("got %d, want 8", d)
	}
}

func TestRPCDecimals_UnknownChain(t *testing.T) {
	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": "http://unused"}, http.DefaultClient)
	_, err := src.Decimals(context.Background(), "solana", "0x1234")
	if err == nil {
		t.Fatal("expected error for unknown chain")
	}
}

func TestRPCDecimals_RPCErrorPayload(t *testing.T) {
	ts := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"execution reverted"}}`, 200)
	defer ts.Close()

	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": ts.URL}, http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xnotacontract11111111111111111111111111111")
	if err == nil {
		t.Fatal("expected error when RPC returns JSON-RPC error")
	}
	if !strings.Contains(err.Error(), "execution reverted") {
		t.Errorf("error should carry RPC message; got %v", err)
	}
}

func TestRPCDecimals_HTTPError(t *testing.T) {
	ts := fakeRPC(t, `internal server error`, 500)
	defer ts.Close()

	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": ts.URL}, http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestRPCDecimals_MalformedResult(t *testing.T) {
	ts := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"result":"0x"}`, 200)
	defer ts.Close()

	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": ts.URL}, http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err == nil {
		t.Fatal("expected error on truncated result")
	}
}

func TestRPCDecimals_EmptyResult(t *testing.T) {

	ts := fakeRPC(t, `{"jsonrpc":"2.0","id":1,"result":"0x"}`, 200)
	defer ts.Close()

	src := swap.NewRPCDecimalsSource(map[string]string{"ethereum": ts.URL}, http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	if err == nil {
		t.Fatal("expected error on empty result (EOA case)")
	}
}
