// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
)

const (
	testOwner    = "0x1234567890123456789012345678901234567890"
	testRouter   = "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae"
	testApproval = "0xAaBbCcDdEeFf001122334455667788990011aabb"
	testUSDC     = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	testUSDCBase = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	testData     = "0xdeadbeef01020304"
	testValue    = "0x5af3107a4000"
)

// fakeRelayResolver returns 6 decimals for any ERC-20 (matches USDC) and 18
// for native. Tests that exercise the AmountFormatted path inject this so
// they remain deterministic.
type fakeRelayResolver struct{}

func (fakeRelayResolver) Resolve(_ context.Context, _, asset string) (uint8, error) {
	switch strings.ToLower(asset) {
	case "", "native", "0x0", strings.ToLower(eip155NativeToken), zeroNativeToken:
		return 18, nil
	}
	return 6, nil
}

const (
	eip155NativeToken = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"
	zeroNativeToken   = "0x0000000000000000000000000000000000000000"
)

func TestNormalizeNativeToken_ProtocolSpecificAddresses(t *testing.T) {
	if got := bridgeadapters.NormalizeNativeTokenForTest("native", eip155NativeToken); got != eip155NativeToken {
		t.Fatalf("Bungee native = %s, want %s", got, eip155NativeToken)
	}
	if got := bridgeadapters.NormalizeNativeTokenForTest(zeroNativeToken, eip155NativeToken); got != eip155NativeToken {
		t.Fatalf("Bungee zero native = %s, want %s", got, eip155NativeToken)
	}
	if got := bridgeadapters.NormalizeNativeTokenForTest(eip155NativeToken, zeroNativeToken); got != zeroNativeToken {
		t.Fatalf("Relay Eeee native = %s, want %s", got, zeroNativeToken)
	}
	if got := bridgeadapters.NormalizeNativeTokenForTest(testUSDC, eip155NativeToken); got != testUSDC {
		t.Fatalf("ERC-20 changed: %s", got)
	}
}

func TestBridgeCallBuilder_ApproveSequence(t *testing.T) {
	tests := []struct {
		name      string
		allowance *big.Int
		wantCalls int
		wantZero  bool
	}{
		{name: "zero allowance", allowance: big.NewInt(0), wantCalls: 1},
		{name: "partial allowance", allowance: big.NewInt(500000), wantCalls: 2, wantZero: true},
		{name: "sufficient allowance", allowance: big.NewInt(1000000), wantCalls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := allowanceRPC(t, tt.allowance)
			defer rpc.Close()
			calls := bridge.ComputeApproveSequence(context.Background(), rpc.URL, testOwner, testUSDC, testApproval, "", "erc20", big.NewInt(1000000))
			if len(calls) != tt.wantCalls {
				t.Fatalf("approve calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if tt.wantCalls > 0 {
				if calls[0].To != testUSDC {
					t.Fatalf("approve call target = %s, want token %s", calls[0].To, testUSDC)
				}
				if !strings.Contains(calls[0].Data, strings.ToLower(strings.TrimPrefix(testApproval, "0x"))) {
					t.Fatalf("approve calldata does not contain spender: %s", calls[0].Data)
				}
			}
			if tt.wantZero && !strings.HasSuffix(calls[0].Data, strings.Repeat("0", 64)) {
				t.Fatalf("first partial-allowance approve should reset to zero: %s", calls[0].Data)
			}
		})
	}
}

func TestBridgeCallBuilder_ApproveFallbackWhenRPCUnavailable(t *testing.T) {
	calls := bridge.ComputeApproveSequence(context.Background(), "", testOwner, testUSDC, testApproval, "", "erc20", big.NewInt(1000000))
	if len(calls) != 1 {
		t.Fatalf("fallback approve calls = %d, want 1", len(calls))
	}
	if !strings.HasSuffix(calls[0].Data, big.NewInt(1000000).Text(16)) {
		t.Fatalf("fallback approve should be for exact amount: %s", calls[0].Data)
	}
}

func TestBungeeQuote_ParsesAutoRouteAndNormalizesNative(t *testing.T) {
	var inputToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inputToken = r.URL.Query().Get("inputToken")
		writeJSON(t, w, map[string]interface{}{
			"success": true,
			"result": map[string]interface{}{
				"autoRoute": map[string]interface{}{
					"output": map[string]interface{}{"amount": "99000000"},
					"txData": map[string]interface{}{
						"to":    testRouter,
						"data":  testData,
						"value": "0",
					},
				},
			},
		})
	}))
	defer server.Close()

	adapter := bridgeadapters.NewHTTPBridgeAdapterForTest("bungee", server.URL, server.Client(), nil)
	quote, err := adapter.Quote(context.Background(), bridge.QuoteRequest{
		FromChain: "ethereum", ToChain: "base", FromToken: zeroNativeToken, ToToken: testUSDCBase,
		Amount: "100000000000000", FromAddress: testOwner,
	})
	if err != nil {
		t.Fatalf("Quote error: %v", err)
	}
	if quote.EstimatedReceive != "99000000" {
		t.Fatalf("unexpected bungee amounts: %+v", quote)
	}
	if inputToken != eip155NativeToken {
		t.Fatalf("inputToken = %s, want %s", inputToken, eip155NativeToken)
	}
}

func TestRelayQuote_NormalizesNativeToZeroAddress(t *testing.T) {
	var body struct {
		OriginCurrency string `json:"originCurrency"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		writeJSON(t, w, map[string]interface{}{
			"details": map[string]interface{}{"currencyOut": map[string]interface{}{"amountFormatted": "99.0"}},
			"steps": []interface{}{map[string]interface{}{"items": []interface{}{map[string]interface{}{
				"data": map[string]interface{}{"to": testRouter, "data": testData, "value": testValue},
			}}}},
		})
	}))
	defer server.Close()

	adapter := bridgeadapters.NewHTTPBridgeAdapterForTest("relay", server.URL, server.Client(), fakeRelayResolver{})
	if _, err := adapter.Quote(context.Background(), bridge.QuoteRequest{
		FromChain: "ethereum", ToChain: "base", FromToken: eip155NativeToken, ToToken: testUSDCBase,
		Amount: "100000000000000", FromAddress: testOwner,
	}); err != nil {
		t.Fatalf("Quote error: %v", err)
	}
	if body.OriginCurrency != zeroNativeToken {
		t.Fatalf("originCurrency = %s, want %s", body.OriginCurrency, zeroNativeToken)
	}
}

func TestRelayQuote_RejectsMultiStepRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"details": map[string]interface{}{"currencyOut": map[string]interface{}{"amount": "99000000"}},
			"steps": []interface{}{
				map[string]interface{}{"items": []interface{}{map[string]interface{}{"data": map[string]interface{}{"to": testRouter, "data": testData, "value": testValue}}}},
				map[string]interface{}{"items": []interface{}{map[string]interface{}{"data": map[string]interface{}{"to": testRouter, "data": testData, "value": "0x0"}}}},
			},
		})
	}))
	defer server.Close()

	adapter := bridgeadapters.NewHTTPBridgeAdapterForTest("relay", server.URL, server.Client(), fakeRelayResolver{})
	_, err := adapter.Quote(context.Background(), bridge.QuoteRequest{
		FromChain: "ethereum", ToChain: "base", FromToken: eip155NativeToken, ToToken: testUSDCBase,
		Amount: "100000000000000", FromAddress: testOwner,
	})
	if err == nil || !strings.Contains(err.Error(), "multi-step") {
		t.Fatalf("expected multi-step error, got %v", err)
	}
}

func allowanceRPC(t *testing.T, allowance *big.Int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("RPC method = %s, want POST", r.Method)
		}
		result := make([]byte, 32)
		allowance.FillBytes(result)
		writeJSON(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x" + hex.EncodeToString(result),
		})
	}))
}

func TestRelayQuote_AmountFormattedFallback_NativeOutput(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"details": map[string]interface{}{
				"currencyOut": map[string]interface{}{
					"amountFormatted": "0.995",
				},
			},
			"steps": []interface{}{map[string]interface{}{
				"items": []interface{}{map[string]interface{}{
					"data": map[string]interface{}{
						"to": testRouter, "data": testData,
						"value": "0xde0b6b3a7640000",
					},
				}},
			}},
		})
	}))
	defer server.Close()

	adapter := bridgeadapters.NewHTTPBridgeAdapterForTest("relay", server.URL, server.Client(), fakeRelayResolver{})
	quote, err := adapter.Quote(context.Background(), bridge.QuoteRequest{
		FromChain: "ethereum", ToChain: "base",
		FromToken: "native", ToToken: "native",
		Amount: "1000000000000000000", FromAddress: testOwner,
	})
	if err != nil {
		t.Fatalf("Quote error: %v", err)
	}
	if quote.EstimatedReceive != "995000000000000000" {
		t.Fatalf("EstimatedReceive = %s, want 995000000000000000 (18 decimals, not 6)", quote.EstimatedReceive)
	}
}

func TestRelayQuote_AmountFormattedFallback_ERC20Output(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"details": map[string]interface{}{
				"currencyOut": map[string]interface{}{
					"amountFormatted": "99.0",
				},
			},
			"steps": []interface{}{map[string]interface{}{
				"items": []interface{}{map[string]interface{}{
					"data": map[string]interface{}{
						"to": testRouter, "data": testData, "value": testValue,
					},
				}},
			}},
		})
	}))
	defer server.Close()

	adapter := bridgeadapters.NewHTTPBridgeAdapterForTest("relay", server.URL, server.Client(), fakeRelayResolver{})
	quote, err := adapter.Quote(context.Background(), bridge.QuoteRequest{
		FromChain: "ethereum", ToChain: "base",
		FromToken: "native", ToToken: testUSDCBase,
		Amount: "100000000000000", FromAddress: testOwner,
	})
	if err != nil {
		t.Fatalf("Quote error: %v", err)
	}
	if quote.EstimatedReceive != "99000000" {
		t.Fatalf("EstimatedReceive = %s, want 99000000 (6 decimals)", quote.EstimatedReceive)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}
