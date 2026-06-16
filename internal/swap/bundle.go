// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"encoding/json"
	"math/big"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/token"
)

type SwapBundle struct {
	To       string
	Data     string
	Value    string
	GasLimit uint64
}

func ExtractApprovalInfo(quote map[string]interface{}) (tokenAddr, spender, amount string, needed bool) {
	selected := quote
	if best, ok := quote["best"].(map[string]interface{}); ok {
		selected = best
	}
	tokenAddr, _ = selected["source_asset"].(string)
	needed = !token.IsNativeToken(tokenAddr)
	if !needed {
		return "", "", "", false
	}
	spender, _ = selected["approve_target"].(string)
	amount, _ = selected["input_amount"].(string)
	return tokenAddr, spender, amount, true
}

func ParseApproveCallsResponse(body []byte) []ApproveCall {
	var resp struct {
		ApproveCalls []struct {
			To    string `json:"to"`
			Data  string `json:"data"`
			Value string `json:"value"`
		} `json:"approve_calls"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	if len(resp.ApproveCalls) == 0 {
		return nil
	}
	calls := make([]ApproveCall, len(resp.ApproveCalls))
	for i, ac := range resp.ApproveCalls {
		v := new(big.Int)
		if ac.Value != "" && ac.Value != "0x" && ac.Value != "0x0" {
			v.SetString(strings.TrimPrefix(ac.Value, "0x"), 16)
		}
		calls[i] = ApproveCall{To: ac.To, Data: ac.Data, Value: v}
	}
	return calls
}

// ApproveCallsToHex converts []ApproveCall to []erc7821.HexCall.
// A nil Value is treated as 0; Value is deep-copied to prevent caller mutations from affecting batch contents.
func ApproveCallsToHex(calls []ApproveCall) []erc7821.HexCall {
	out := make([]erc7821.HexCall, len(calls))
	for i, c := range calls {
		v := c.Value
		if v == nil {
			v = big.NewInt(0)
		}
		out[i] = erc7821.HexCall{To: c.To, Value: new(big.Int).Set(v), Data: c.Data}
	}
	return out
}

// BuildExecuteBundle wraps calls into a single ERC-7821 batch transaction sent to walletAddress.
// gasLimit is the adapter's estimate; the function applies ×1.25 with a 200K floor.
func BuildExecuteBundle(walletAddress string, calls []erc7821.HexCall, gasLimit uint64) SwapBundle {
	bundle, err := erc7821.BuildExecuteBundle(
		walletAddress,
		calls,
		gasLimit,
		erc7821.MultiplyGasWithFloor(125, 100, 200_000),
	)
	if err != nil {
		panic("BuildExecuteBundle: encode ERC-7821 execute: " + err.Error())
	}

	return SwapBundle{
		To:       bundle.To,
		Data:     bundle.Data,
		Value:    bundle.ValueHex(),
		GasLimit: bundle.GasLimit,
	}
}
