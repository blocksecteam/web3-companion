// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

func containsAddrFF(addrs []string, target string) bool {
	t := strings.ToLower(target)
	for _, a := range addrs {
		if strings.ToLower(a) == t {
			return true
		}
	}
	return false
}

func makeERC20TransferLog(tokenAddr, from, to string) rpcLog {
	pad := func(addr string) string {
		return "0x000000000000000000000000" + strings.TrimPrefix(strings.ToLower(addr), "0x")
	}
	return rpcLog{
		Address: tokenAddr,
		Topics:  []string{erc20TransferTopic, pad(from), pad(to)},
		Data:    "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000",
	}
}

func TestCollectFundFlowAddresses_TokenTransfer(t *testing.T) {
	owner := "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	tokenFrom := "0xbeef000000000000000000000000000000000001"
	tokenTo := "0xbeef000000000000000000000000000000000002"
	call := rpcCall{
		From:  owner,
		To:    "0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad",
		Value: "0x0",
		Logs:  []rpcLog{makeERC20TransferLog("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", tokenFrom, tokenTo)},
	}
	got := collectFundFlowAddresses(call, owner)
	if !containsAddrFF(got, tokenFrom) {
		t.Errorf("want tokenFrom %s in %v", tokenFrom, got)
	}
	if !containsAddrFF(got, tokenTo) {
		t.Errorf("want tokenTo %s in %v", tokenTo, got)
	}
}

func TestCollectFundFlowAddresses_ETHValue(t *testing.T) {
	owner := "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	ethRecipient := "0xbeef000000000000000000000000000000000003"
	call := rpcCall{
		From:  owner,
		To:    "0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad",
		Value: "0x0",
		Calls: []rpcCall{{
			From:  owner,
			To:    ethRecipient,
			Value: "0x16345785d8a0000",
		}},
	}
	got := collectFundFlowAddresses(call, owner)
	if !containsAddrFF(got, owner) {
		t.Errorf("want ETH sender %s in %v", owner, got)
	}
	if !containsAddrFF(got, ethRecipient) {
		t.Errorf("want ETH recipient %s in %v", ethRecipient, got)
	}
}

func TestCollectFundFlowAddresses_ETHZeroValue_NotCollected(t *testing.T) {
	owner := "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	noEthTarget := "0xbeef000000000000000000000000000000000004"
	call := rpcCall{
		From:  owner,
		To:    noEthTarget,
		Value: "0x0",
	}
	got := collectFundFlowAddresses(call, owner)
	if containsAddrFF(got, noEthTarget) {
		t.Errorf("zero-value target %s should not be in %v", noEthTarget, got)
	}
}

func TestCollectFundFlowAddresses_OwnerApprove(t *testing.T) {
	owner := "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	spender := "0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad"
	input := primitives.SelectorApprove +
		"000000000000000000000000" + strings.TrimPrefix(strings.ToLower(spender), "0x") +
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	call := rpcCall{
		From:  owner,
		To:    "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
		Value: "0x0",
		Input: input,
	}
	got := collectFundFlowAddresses(call, owner)
	if !containsAddrFF(got, spender) {
		t.Errorf("want approve spender %s in %v", spender, got)
	}
}

func TestCollectFundFlowAddresses_NonOwnerApprove_NotCollected(t *testing.T) {
	owner := "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	spender := "0xbeef000000000000000000000000000000000005"
	router := "0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad"
	input := primitives.SelectorApprove +
		"000000000000000000000000" + strings.TrimPrefix(strings.ToLower(spender), "0x") +
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	call := rpcCall{
		From:  owner,
		To:    router,
		Value: "0x0",
		Calls: []rpcCall{{
			From:  router,
			To:    "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			Value: "0x0",
			Input: input,
		}},
	}
	got := collectFundFlowAddresses(call, owner)
	if containsAddrFF(got, spender) {
		t.Errorf("non-owner approve spender %s should not be in %v", spender, got)
	}
}

func TestCollectFundFlowAddresses_Dedup(t *testing.T) {
	owner := "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	addr := "0xbeef000000000000000000000000000000000006"
	call := rpcCall{
		From:  owner,
		To:    "0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad",
		Value: "0x16345785d8a0000",
		Calls: []rpcCall{{
			From:  owner,
			To:    addr,
			Value: "0x16345785d8a0000",
		}},
	}
	got := collectFundFlowAddresses(call, owner)
	count := 0
	for _, a := range got {
		if strings.ToLower(a) == strings.ToLower(addr) {
			count++
		}
	}
	if count > 1 {
		t.Errorf("address %s appears %d times, want at most 1", addr, count)
	}
}
