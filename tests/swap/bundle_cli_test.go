// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestExtractApprovalInfo_NativeInput_NeedsNoApproval(t *testing.T) {
	quote := map[string]interface{}{
		"need_approval":  false,
		"approve_target": "",
		"source_asset":   "native",
		"input_amount":   "1000000000000000",
	}
	_, _, _, needed := swap.ExtractApprovalInfo(quote)
	if needed {
		t.Error("native input should not need approval")
	}
}

func TestExtractApprovalInfo_ERC20Input_ExtractsFields(t *testing.T) {
	quote := map[string]interface{}{
		"need_approval":  true,
		"approve_target": "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
		"source_asset":   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"input_amount":   "1000000",
	}
	token, spender, amount, needed := swap.ExtractApprovalInfo(quote)
	if !needed {
		t.Fatal("ERC-20 input should need approval")
	}
	if token != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("wrong token: %s", token)
	}
	if spender != "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5" {
		t.Errorf("wrong spender: %s", spender)
	}
	if amount != "1000000" {
		t.Errorf("wrong amount: %s", amount)
	}
}

func TestExtractApprovalInfo_QuoteWithBestKey_UsesNestedBest(t *testing.T) {

	quote := map[string]interface{}{
		"best": map[string]interface{}{
			"need_approval":  true,
			"approve_target": "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
			"source_asset":   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			"input_amount":   "5000000",
		},
	}
	_, _, amount, needed := swap.ExtractApprovalInfo(quote)
	if !needed {
		t.Fatal("should need approval from nested best")
	}
	if amount != "5000000" {
		t.Errorf("wrong amount from nested best: %s", amount)
	}
}

func TestParseApproveCallsResponse_EmptyList_ReturnsNil(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"approve_calls": []interface{}{},
	})
	calls := swap.ParseApproveCallsResponse(body)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestParseApproveCallsResponse_SingleApprove_ParsedCorrectly(t *testing.T) {
	approveData := token.EncodeERC20Approve("0x6131B5fae19EA4f9D964eAc0408E4408b66337b5", big.NewInt(1_000_000))
	body, _ := json.Marshal(map[string]interface{}{
		"approve_calls": []interface{}{
			map[string]interface{}{
				"to":    "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
				"data":  approveData,
				"value": "0x0",
			},
		},
	})
	calls := swap.ParseApproveCallsResponse(body)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].To != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("wrong To: %s", calls[0].To)
	}
	if calls[0].Data != approveData {
		t.Errorf("wrong Data")
	}
	if calls[0].Value == nil || calls[0].Value.Sign() != 0 {
		t.Errorf("value should be zero")
	}
}

func TestParseApproveCallsResponse_TwoApproves_BothParsed(t *testing.T) {

	body, _ := json.Marshal(map[string]interface{}{
		"approve_calls": []interface{}{
			map[string]interface{}{"to": "0xToken", "data": "0x095ea7b300", "value": "0x0"},
			map[string]interface{}{"to": "0xToken", "data": "0x095ea7b301", "value": "0x0"},
		},
	})
	calls := swap.ParseApproveCallsResponse(body)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
}

func TestParseApproveCallsResponse_MalformedJSON_ReturnsNil(t *testing.T) {
	calls := swap.ParseApproveCallsResponse([]byte("not json"))
	if calls != nil {
		t.Errorf("malformed JSON should return nil")
	}
}
