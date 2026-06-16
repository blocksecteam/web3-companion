// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	bridge "github.com/blocksecteam/web3-companion/internal/bridge"
)

const lifiDefaultURL = "https://li.quest/v1"

type lifiBridge struct {
	bridgeHTTPAdapter
}

// LiFi /quote response — only the fields the adapter actually reads.
// Source: https://docs.li.fi/api-reference/get-a-quote-for-a-token-transfer
type lifiQuoteResponse struct {
	Estimate struct {
		ApprovalAddress string `json:"approvalAddress"`
		ToAmount        string `json:"toAmount"`
	} `json:"estimate"`
	TransactionRequest struct {
		To       string `json:"to"`
		Data     string `json:"data"`
		Value    string `json:"value"`
		GasLimit string `json:"gasLimit"`
	} `json:"transactionRequest"`
	// LiFi returns 4xx with {"message": "..."} on errors; the HTTP helper
	// already surfaces that as a Go error, so no error fields are needed here.
}

func NewLiFiBridge() bridge.Adapter {
	return &lifiBridge{
		bridgeHTTPAdapter: newBridgeHTTPAdapter("lifi", lifiDefaultURL, "LIFI_API_URL"),
	}
}

func (a *lifiBridge) Name() string { return "lifi" }

func (a *lifiBridge) SupportedPairs() []bridge.ChainPair {
	chains := []string{"ethereum", "base", "polygon", "bsc"}
	return bridgePairs(chains, chains)
}

// Capabilities: LiFi pulls source tokens via standard ERC-20 approve to the
// spender returned in estimate.approvalAddress, then calls the router on the
// source chain — slippage parameter is honored. Accepts and emits both native
// and WETH on both sides via the fromToken / toToken parameters on /quote.
func (a *lifiBridge) Capabilities() bridge.BridgeCapabilities {
	return bridge.BridgeCapabilities{
		Native: bridge.NativeCapability{
			AcceptsNativeIn: true,
			AcceptsWETHIn:   true,
			OutputsNative:   true,
			OutputsWETH:     true,
		},
		Approve:          bridge.ApproveCapability{Mode: bridge.ApproveModeApprove, Style: bridge.ApproveStyleERC20},
		SupportsSlippage: true,
	}
}

func (a *lifiBridge) Quote(ctx context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	fromChainID, err := resolveBridgeChainID(req.FromChain)
	if err != nil {
		return nil, fmt.Errorf("lifi: %w", err)
	}
	toChainID, err := resolveBridgeChainID(req.ToChain)
	if err != nil {
		return nil, fmt.Errorf("lifi: %w", err)
	}

	params := url.Values{}
	params.Set("fromChain", strconv.Itoa(fromChainID))
	params.Set("toChain", strconv.Itoa(toChainID))
	params.Set("fromToken", normalizeNativeToken(req.FromToken, eip155NativeToken))
	params.Set("toToken", resolveBridgeToToken(req, eip155NativeToken))
	params.Set("fromAmount", req.Amount)
	params.Set("fromAddress", req.FromAddress)
	if recv := receiveAddress(req); recv != "" {
		params.Set("toAddress", recv)
	}
	// LiFi expects slippage as a decimal fraction (0.005 = 0.5%); the request
	// surface uses percent. Only forward when the caller set a non-default
	// value to avoid overriding LiFi's own default.
	if req.Slippage > 0 {
		params.Set("slippage", strconv.FormatFloat(req.Slippage/100, 'f', -1, 64))
	}

	var quote lifiQuoteResponse
	if err := a.getJSON(ctx, "/quote", params, &quote); err != nil {
		return nil, err
	}
	if quote.TransactionRequest.To == "" || quote.TransactionRequest.Data == "" {
		return nil, fmt.Errorf("lifi: quote missing executable transactionRequest")
	}
	if quote.Estimate.ToAmount == "" || quote.Estimate.ToAmount == "0" {
		return nil, fmt.Errorf("lifi: no route")
	}
	if quote.Estimate.ApprovalAddress == "" {
		return nil, fmt.Errorf("lifi: quote missing estimate.approvalAddress (required for approve-mode bridge)")
	}

	raw := map[string]interface{}{
		"to":    quote.TransactionRequest.To,
		"value": quote.TransactionRequest.Value,
		"data":  quote.TransactionRequest.Data,
		"gas":   quote.TransactionRequest.GasLimit,
	}

	return &bridge.QuoteResult{
		Bridge:           a.Name(),
		InputAmount:      req.Amount,
		EstimatedReceive: quote.Estimate.ToAmount,
		ApproveTarget:    quote.Estimate.ApprovalAddress,
		RawQuote:         raw,
	}, nil
}

func (a *lifiBridge) Build(_ context.Context, req bridge.BuildRequest) (*bridge.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("lifi: invalid quote format")
	}
	to := stringFromMap(raw, "to")
	if to == "" {
		return nil, fmt.Errorf("lifi: quote response missing transactionRequest.to")
	}
	return buildBridgeResult("lifi", to, stringFromMap(raw, "value"), stringFromMap(raw, "data"), parseGas(stringFromMap(raw, "gas")))
}
