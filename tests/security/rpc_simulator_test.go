// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

type stubTokenValuator struct {
	values map[string]valuation
}

type valuation struct {
	amount string
	usd    string
}

func (v stubTokenValuator) TokenAmount(ctx context.Context, chain, tokenAddress, rawAmount string) (string, string, bool) {
	key := strings.ToLower(strings.TrimSpace(chain)) + ":" + strings.ToLower(strings.TrimSpace(tokenAddress)) + ":" + strings.TrimSpace(rawAmount)
	value, ok := v.values[key]
	return value.amount, value.usd, ok
}

func TestRPCSimulatorBuildsTraceAndERC20BalanceChanges(t *testing.T) {
	requests := make([]map[string]interface{}, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		requests = append(requests, req)
		method := req["method"].(string)
		params := req["params"].([]interface{})
		w.Header().Set("Content-Type", "application/json")
		if method == "eth_call" {
			call := params[0].(map[string]interface{})
			data := strings.ToLower(call["data"].(string))
			switch data {
			case "0x06fdde03":
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + abiString("USD Coin") + `"}`))
			case "0x95d89b41":
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + abiString("USDC") + `"}`))
			default:
				t.Fatalf("unexpected eth_call data %q", data)
			}
			return
		}
		tracer := params[2].(map[string]interface{})["tracer"].(string)
		switch tracer {
		case "callTracer":
			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"type": "CALL",
					"from": "` + testOwner + `",
					"to": "` + testRouter + `",
					"input": "0xdeadbeef",
					"gas": "0x493e0",
					"gasUsed": "0x186a0",
					"value": "0x0",
					"logs": [{
						"address": "` + testToken + `",
						"topics": [
							"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
							"0x000000000000000000000000` + strings.ToLower(testOwner[2:]) + `",
							"0x000000000000000000000000` + strings.ToLower(testAccount[2:]) + `"
						],
						"data": "0x00000000000000000000000000000000000000000000000000000000000f4240"
					}]
				}
			}`))
		case "prestateTracer":
			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
						"pre": {"` + strings.ToLower(testOwner) + `": {"balance": "0x4563918244f40000"}},
						"post": {"` + strings.ToLower(testOwner) + `": {"balance": "0x44004c09e76a0000"}}
				}
			}`))
		default:
			t.Fatalf("unexpected tracer %q", tracer)
		}
	}))
	t.Cleanup(server.Close)

	sim := security.NewRPCSimulator(map[int]string{1: server.URL}, server.Client(), stubTokenValuator{values: map[string]valuation{
		"ethereum:" + strings.ToLower(testToken) + ":1000000":                    {amount: "1", usd: "1"},
		"ethereum:0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee:100000000000000000": {amount: "0.1", usd: "250"},
	}})
	result, _, err := sim.Simulate(t.Context(), security.CheckRequest{
		ChainID:  1,
		Chain:    "ethereum",
		Owner:    testOwner,
		To:       testRouter,
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 300000,
		GasPrice: "1000000000",
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if result.Data == nil || !result.Data.Status {
		t.Fatalf("simulation status not successful: %#v", result)
	}
	if result.Data.GasUsed != 100000 || result.Data.TransactionFee != "100000000000000" {
		t.Fatalf("gas normalization failed: %#v", result.Data)
	}
	if result.Data.Trace == nil {
		t.Fatal("trace is nil")
	}
	if result.Data.Trace.Func != "0xdeadbeef" {
		t.Fatalf("trace func = %q, want selector only", result.Data.Trace.Func)
	}
	if len(requests) != 4 {
		t.Fatalf("rpc requests = %d, want 2 traces + 2 metadata calls", len(requests))
	}
	if len(result.Data.BalanceChanges) != 1 || len(result.Data.BalanceChanges[0].Assets) != 2 {
		t.Fatalf("balance changes not normalized: %#v", result.Data.BalanceChanges)
	}
	erc20 := result.Data.BalanceChanges[0].Assets[0]
	if erc20.Address != testToken || erc20.BaseUnits != "-1000000" || erc20.Amount != "-1" ||
		erc20.Value != "1" || erc20.Type != "erc20" || erc20.TokenName != "USD Coin" || erc20.TokenSymbol != "USDC" {
		t.Fatalf("erc20 transfer not normalized as owner spend: %#v", erc20)
	}
	native := result.Data.BalanceChanges[0].Assets[1]
	if native.Address != "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" ||
		native.BaseUnits != "-100000000000000000" || native.Amount != "-0.1" || native.Value != "250" ||
		native.Type != "native" || native.TokenName != "ETH" || native.TokenSymbol != "ETH" {
		t.Fatalf("native diff not normalized as owner spend: %#v", native)
	}
	if strings.Contains(string(result.Raw), `"sign"`) || strings.Contains(string(result.Raw), `"isERC721"`) || strings.Contains(string(result.Raw), `"isERC1155"`) {
		t.Fatalf("raw simulation contains old balance-change fields: %s", string(result.Raw))
	}
	var raw struct {
		Provider string                   `json:"provider"`
		Trace    *security.SimulationCall `json:"trace"`
	}
	if err := json.Unmarshal(result.Raw, &raw); err != nil {
		t.Fatalf("raw simulation parse: %v", err)
	}
	if raw.Provider != "rpc" || raw.Trace == nil {
		t.Fatalf("raw simulation not simplified: %s", string(result.Raw))
	}
	if raw.Trace.Func != "0xdeadbeef" {
		t.Fatalf("raw trace func = %q, want selector only", raw.Trace.Func)
	}
	if strings.Contains(string(result.Raw), "call_trace") || strings.Contains(string(result.Raw), "state_diff") || strings.Contains(string(result.Raw), "output") || strings.Contains(string(result.Raw), "input") {
		t.Fatalf("raw simulation contains unsimplified fields: %s", string(result.Raw))
	}
}

func TestRPCSimulatorUsesLatestRPCProviderSnapshot(t *testing.T) {
	first := minimalTraceRPC(t)
	second := minimalTraceRPC(t)
	current := map[int]string{1: first.URL}
	sim := security.NewRPCSimulatorWithProvider(func() map[int]string {
		return current
	}, first.Client(), nil)

	req := security.CheckRequest{
		ChainID:  1,
		Chain:    "ethereum",
		Owner:    testOwner,
		To:       testRouter,
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 300000,
		GasPrice: "1000000000",
	}
	if _, _, err := sim.Simulate(t.Context(), req); err != nil {
		t.Fatalf("simulate first rpc: %v", err)
	}
	current = map[int]string{1: second.URL}
	if _, _, err := sim.Simulate(t.Context(), req); err != nil {
		t.Fatalf("simulate second rpc: %v", err)
	}
	if firstRequests := first.Config.Handler.(*requestCountingHandler).count(); firstRequests != 2 {
		t.Fatalf("first rpc requests = %d, want 2", firstRequests)
	}
	if secondRequests := second.Config.Handler.(*requestCountingHandler).count(); secondRequests != 2 {
		t.Fatalf("second rpc requests = %d, want 2", secondRequests)
	}
}

func abiString(value string) string {
	hexValue := fmt.Sprintf("%x", []byte(value))
	return "0x" + fmt.Sprintf("%064x%064x", 32, len(value)) + hexValue + strings.Repeat("0", (64-len(hexValue)%64)%64)
}

type requestCountingHandler struct {
	n int
}

func (h *requestCountingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.n++
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	params := req["params"].([]interface{})
	tracer := params[2].(map[string]interface{})["tracer"].(string)
	w.Header().Set("Content-Type", "application/json")
	switch tracer {
	case "callTracer":
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"type":"CALL","from":"` + testOwner + `","to":"` + testRouter + `","input":"0xdeadbeef","gas":"0x493e0","gasUsed":"0x186a0","value":"0x0"}}`))
	case "prestateTracer":
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"pre":{"` + strings.ToLower(testOwner) + `":{"balance":"0x1"}},"post":{"` + strings.ToLower(testOwner) + `":{"balance":"0x1"}}}}`))
	default:
		http.Error(w, "unexpected tracer", http.StatusBadRequest)
	}
}

func (h *requestCountingHandler) count() int {
	return h.n
}

func minimalTraceRPC(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(&requestCountingHandler{})
	t.Cleanup(server.Close)
	return server
}

func TestRPCSimulatorBuildsERC721BalanceChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		if req["method"] == "eth_call" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x"}`))
			return
		}
		params := req["params"].([]interface{})
		tracer := params[2].(map[string]interface{})["tracer"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tracer {
		case "callTracer":
			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"type": "CALL",
					"from": "` + testOwner + `",
					"to": "` + testRouter + `",
					"input": "0xdeadbeef",
					"gas": "0x493e0",
					"gasUsed": "0x186a0",
					"value": "0x0",
					"logs": [{
						"address": "0x2222222222222222222222222222222222222222",
						"topics": [
							"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
							"0x000000000000000000000000` + strings.ToLower(testOwner[2:]) + `",
							"0x000000000000000000000000` + strings.ToLower(testAccount[2:]) + `",
							"0x000000000000000000000000000000000000000000000000000000000000007b"
						],
						"data": "0x"
					}]
				}
			}`))
		case "prestateTracer":
			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"pre": {"` + strings.ToLower(testOwner) + `": {"balance": "0x4563918244f40000"}},
					"post": {"` + strings.ToLower(testOwner) + `": {"balance": "0x4563918244f40000"}}
				}
			}`))
		default:
			t.Fatalf("unexpected tracer %q", tracer)
		}
	}))
	t.Cleanup(server.Close)

	sim := security.NewRPCSimulator(map[int]string{1: server.URL}, server.Client(), nil)
	result, _, err := sim.Simulate(t.Context(), security.CheckRequest{
		ChainID:  1,
		Chain:    "ethereum",
		Owner:    testOwner,
		To:       testRouter,
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 300000,
		GasPrice: "1000000000",
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if len(result.Data.BalanceChanges) != 1 {
		t.Fatalf("balance change account count = %d, want 1", len(result.Data.BalanceChanges))
	}
	assets := result.Data.BalanceChanges[0].Assets
	if len(assets) != 1 {
		t.Fatalf("asset count = %d, want 1: %#v", len(assets), assets)
	}
	if assets[0].Type != "erc721" || assets[0].TokenID != "123" || assets[0].BaseUnits != "-1" || assets[0].Amount != "-1" {
		t.Fatalf("ERC-721 transfer not normalized as owner spend: %#v", assets[0])
	}
}

func TestRPCSimulatorBuildsERC1155SingleBalanceChanges(t *testing.T) {
	const erc1155Token = "0x3333333333333333333333333333333333333333"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		if req["method"] == "eth_call" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x"}`))
			return
		}
		params := req["params"].([]interface{})
		tracer := params[2].(map[string]interface{})["tracer"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tracer {
		case "callTracer":
			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"type": "CALL",
					"from": "` + testOwner + `",
					"to": "` + testRouter + `",
					"input": "0xdeadbeef",
					"gas": "0x493e0",
					"gasUsed": "0x186a0",
					"value": "0x0",
					"logs": [{
						"address": "` + erc1155Token + `",
						"topics": [
							"0xc3d58168c5ae7397731d063d5bbf3d017d58f9ed087d3e5f8bdbf03e00d0b8f7",
							"0x000000000000000000000000` + strings.ToLower(testRouter[2:]) + `",
							"0x000000000000000000000000` + strings.ToLower(testOwner[2:]) + `",
							"0x000000000000000000000000` + strings.ToLower(testAccount[2:]) + `"
						],
						"data": "0x00000000000000000000000000000000000000000000000000000000000001c80000000000000000000000000000000000000000000000000000000000000005"
					}]
				}
			}`))
		case "prestateTracer":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"pre":{"` + strings.ToLower(testOwner) + `":{"balance":"0x1"}},"post":{"` + strings.ToLower(testOwner) + `":{"balance":"0x1"}}}}`))
		default:
			t.Fatalf("unexpected tracer %q", tracer)
		}
	}))
	t.Cleanup(server.Close)

	sim := security.NewRPCSimulator(map[int]string{1: server.URL}, server.Client(), nil)
	result, _, err := sim.Simulate(t.Context(), security.CheckRequest{
		ChainID:  1,
		Chain:    "ethereum",
		Owner:    testOwner,
		To:       testRouter,
		Value:    "0",
		Data:     "0xdeadbeef",
		GasLimit: 300000,
		GasPrice: "1000000000",
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	assets := result.Data.BalanceChanges[0].Assets
	if len(assets) != 1 {
		t.Fatalf("asset count = %d, want 1: %#v", len(assets), assets)
	}
	if assets[0].Type != "erc1155" || assets[0].TokenID != "456" || assets[0].BaseUnits != "-5" || assets[0].Amount != "-5" {
		t.Fatalf("ERC-1155 single transfer not normalized as owner spend: %#v", assets[0])
	}
}
