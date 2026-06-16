// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

// LiFiAdapter wraps li.quest /quote for same-chain swap aggregation.
//
// LI.FI exposes a single /quote endpoint that handles both same-chain and
// cross-chain quotes; the implementation matches whenever fromChain == toChain
// (see https://docs.li.fi/api-reference/get-a-quote-for-a-token-transfer).
// LI.FI returns a fully populated transactionRequest in the quote response, so
// Build does not need a second API call — it just unpacks the cached fields
// from RawQuote.
type LiFiAdapter struct {
	client  *http.Client
	baseURL string
}

func NewLiFiAdapter() *LiFiAdapter {
	return &LiFiAdapter{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: envOrDefault("LIFI_API_URL", "https://li.quest/v1"),
	}
}

func (a *LiFiAdapter) Name() string { return "lifi" }

func (a *LiFiAdapter) SupportedChains() []string {
	return []string{"ethereum", "base", "polygon", "bsc"}
}

// Capabilities: LiFi accepts/outputs both native and WETH on both sides via
// the fromToken / toToken parameters on /quote, so the handler never inserts
// WETH deposit/withdraw around the swap call. /quote also takes a slippage
// parameter that LiFi threads through to whichever underlying DEX it routes to.
func (a *LiFiAdapter) Capabilities() swap.SwapCapabilities {
	return swap.SwapCapabilities{
		Native: swap.NativeCapability{
			AcceptsNativeIn: true,
			AcceptsWETHIn:   true,
			OutputsNative:   true,
			OutputsWETH:     true,
		},
		SupportsSlippage: true,
	}
}

// lifiQuoteResponse mirrors only the fields the adapter actually reads from
// li.quest /quote. Source:
// https://docs.li.fi/api-reference/get-a-quote-for-a-token-transfer
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
}

func (a *LiFiAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	id, ok := chainID(req.Chain)
	if !ok {
		return nil, fmt.Errorf("lifi: unsupported chain %s", req.Chain)
	}
	chainStr := strconv.Itoa(id)

	params := url.Values{}
	params.Set("fromChain", chainStr)
	params.Set("toChain", chainStr) // same-chain: LI.FI selects DEX routes
	params.Set("fromToken", normalizeNativeToken(req.SourceAsset, eip155NativeToken))
	params.Set("toToken", normalizeNativeToken(req.TargetAsset, eip155NativeToken))
	params.Set("fromAmount", req.Amount)
	params.Set("fromAddress", req.FromAddress)
	// LI.FI expects slippage as a decimal fraction (0.005 = 0.5%); the request
	// surface uses percent. Only forward when caller set a non-default value
	// so LI.FI applies its own default otherwise.
	if req.Slippage > 0 {
		params.Set("slippage", strconv.FormatFloat(req.Slippage/100, 'f', -1, 64))
	}

	var q lifiQuoteResponse
	if err := getJSON(ctx, a.client, a.baseURL, "/quote", params, &q, nil); err != nil {
		return nil, fmt.Errorf("lifi: quote: %w", err)
	}
	if q.TransactionRequest.To == "" || q.TransactionRequest.Data == "" {
		return nil, fmt.Errorf("lifi: quote missing executable transactionRequest")
	}
	if bigString(q.Estimate.ToAmount).Sign() <= 0 {
		return nil, fmt.Errorf("lifi: quote returned zero output amount")
	}
	if q.Estimate.ApprovalAddress == "" {
		return nil, fmt.Errorf("lifi: quote missing estimate.approvalAddress")
	}

	return &swap.QuoteResult{
		Protocol:      a.Name(),
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  q.Estimate.ToAmount,
		ApproveStyle:  "erc20",
		ApproveTarget: q.Estimate.ApprovalAddress,
		// LI.FI returns the executable tx in /quote, so cache it for Build.
		// Slippage is already baked in at quote time — the BuildRequest's
		// SlippageBPS is ignored (same pattern as Odos, which bakes slippage
		// into the quote and reuses it via pathID at assemble time).
		RawQuote: map[string]interface{}{
			"to":    q.TransactionRequest.To,
			"value": q.TransactionRequest.Value,
			"data":  q.TransactionRequest.Data,
			"gas":   q.TransactionRequest.GasLimit,
		},
	}, nil
}

func (a *LiFiAdapter) Build(_ context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("lifi: invalid quote format")
	}
	to := stringValue(raw, "to")
	data := stringValue(raw, "data")
	if to == "" || data == "" {
		return nil, fmt.Errorf("lifi: cached quote missing transactionRequest fields")
	}
	return &swap.BuildResult{
		To:       to,
		Value:    hexFromString(stringValue(raw, "value")),
		Data:     data,
		GasLimit: parseGas(stringValue(raw, "gas")),
	}, nil
}
