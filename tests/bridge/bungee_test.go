// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
)

func fakeBungeeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/bungee/quote":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"statusCode": 200,
				"result": map[string]interface{}{
					"autoRoute": map[string]interface{}{
						"output": map[string]interface{}{
							"amount":       "997000000000000000",
							"minAmountOut": "950000000000000000",
						},
						"txData": map[string]interface{}{
							"type":    "evm",
							"to":      "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae",
							"data":    "0xdeadbeef",
							"value":   "1000000000000000",
							"chainId": 1,
						},
						"approvalData": nil,
					},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(srv.Close)
	return srv
}

func TestBungee_Build_UsesTxDataFromQuote(t *testing.T) {
	srv := fakeBungeeServer(t)
	t.Setenv("BUNGEE_API_URL", srv.URL+"/api/v1")

	adapter := bridgeadapters.NewBungeeBridge()
	owner := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"

	quoteReq := bridge.QuoteRequest{
		FromChain: "ethereum", ToChain: "base",
		FromToken: "native", ToToken: "native",
		Amount: "1000000000000000", FromAddress: owner, Slippage: 0.5,
	}
	quote, err := adapter.Quote(context.Background(), quoteReq)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if quote.EstimatedReceive == "" || quote.EstimatedReceive == "0" {
		t.Fatalf("quote returned no receive amount")
	}

	buildReq := bridge.BuildRequest{
		Quote: quote.RawQuote,
	}
	result, err := adapter.Build(context.Background(), buildReq)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if result.To != "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae" {
		t.Errorf("To = %q, want LiFi Diamond contract address from txData", result.To)
	}
	if result.Data != "0xdeadbeef" {
		t.Errorf("Data = %q, want 0xdeadbeef from txData", result.Data)
	}
	if result.Value != "1000000000000000" {
		t.Errorf("Value = %q, want 1000000000000000 from txData", result.Value)
	}
}
