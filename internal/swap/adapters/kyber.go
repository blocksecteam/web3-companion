// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

type KyberAdapter struct {
	client  *http.Client
	baseURL string
}

func NewKyberAdapter() *KyberAdapter {
	return &KyberAdapter{client: &http.Client{Timeout: 30 * time.Second}, baseURL: envOrDefault("KYBER_API_URL", "https://aggregator-api.kyberswap.com")}
}

func (a *KyberAdapter) Name() string { return "kyber" }

func (a *KyberAdapter) SupportedChains() []string {
	return []string{"ethereum", "base", "polygon", "bsc"}
}

// Capabilities: KyberSwap accepts/outputs both native and WETH on both sides;
// the build API takes slippageTolerance and respects it server-side.
func (a *KyberAdapter) Capabilities() swap.SwapCapabilities {
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

func (a *KyberAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	slug := kyberChainSlug(req.Chain)
	if slug == "" {
		return nil, fmt.Errorf("kyber: unsupported chain %s", req.Chain)
	}

	var q kyberQuoteResponse
	params := url.Values{}
	params.Set("tokenIn", normalizeNativeToken(req.SourceAsset, eip155NativeToken))
	params.Set("tokenOut", normalizeNativeToken(req.TargetAsset, eip155NativeToken))
	params.Set("amountIn", req.Amount)
	params.Set("to", req.FromAddress)
	params.Set("gasInclude", "true")
	if err := getJSON(ctx, a.client, a.baseURL, "/"+slug+"/api/v1/routes", params, &q, map[string]string{"x-client-id": "web3-companion"}); err != nil {
		return nil, fmt.Errorf("kyber: request: %w", err)
	}
	if q.Code != 0 {
		return nil, fmt.Errorf("kyber API error %d: %s", q.Code, q.Message)
	}
	var summary struct {
		AmountOut string `json:"amountOut"`
		Gas       string `json:"gas"`
	}
	if err := json.Unmarshal(q.Data.RouteSummary, &summary); err != nil {
		return nil, fmt.Errorf("kyber: parse routeSummary: %w", err)
	}
	if bigString(summary.AmountOut).Sign() <= 0 {
		return nil, fmt.Errorf("kyber: quote returned zero output amount")
	}

	return &swap.QuoteResult{
		Protocol:      "kyber",
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  summary.AmountOut,
		ApproveStyle:  "erc20",
		ApproveTarget: q.Data.RouterAddress,
		RawQuote: map[string]interface{}{
			"route_summary": q.Data.RouteSummary,
			"chain_slug":    slug,
		},
	}, nil
}

type kyberQuoteResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RouteSummary  json.RawMessage        `json:"routeSummary"`
		RouterAddress string                 `json:"routerAddress"`
		Internal      map[string]interface{} `json:"-"`
	} `json:"data"`
}

func (a *KyberAdapter) Build(ctx context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("kyber: invalid quote format")
	}
	routeSummary := raw["route_summary"]
	if routeSummary == nil {
		return nil, fmt.Errorf("kyber: missing route_summary in quote")
	}
	slug := stringValue(raw, "chain_slug")
	if slug == "" {
		slug = kyberChainSlug(req.Chain)
	}
	if slug == "" {
		return nil, fmt.Errorf("kyber: missing chain slug")
	}
	slippage := defaultSlippageBPS
	if req.SlippageBPS > 0 {
		slippage = req.SlippageBPS
	}
	body := map[string]interface{}{
		"routeSummary":      routeSummary,
		"sender":            req.FromAddress,
		"recipient":         req.FromAddress,
		"slippageTolerance": slippage,
	}

	// KyberSwap /route/build response shape (per official docs):
	//   data.routerAddress    — target contract for the swap call (no separate "to" field)
	//   data.data             — encoded calldata
	//   data.transactionValue — msg.value to attach; set to amountIn when source is native,
	//                           "0" for ERC-20 input
	//   data.gas              — gas estimate
	var b struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			RouterAddress    string `json:"routerAddress"`
			Data             string `json:"data"`
			TransactionValue string `json:"transactionValue"`
			Gas              string `json:"gas"`
		} `json:"data"`
	}
	if err := postJSON(ctx, a.client, a.baseURL, "/"+slug+"/api/v1/route/build", body, &b, map[string]string{"x-client-id": "web3-companion"}); err != nil {
		return nil, fmt.Errorf("kyber: build request: %w", err)
	}
	if b.Code != 0 {
		return nil, fmt.Errorf("kyber build error %d: %s", b.Code, b.Message)
	}
	if b.Data.RouterAddress == "" || b.Data.Data == "" {
		return nil, fmt.Errorf("kyber: build response missing routerAddress or calldata")
	}
	return &swap.BuildResult{
		To:       b.Data.RouterAddress,
		Value:    hexFromString(b.Data.TransactionValue),
		Data:     b.Data.Data,
		GasLimit: parseGas(b.Data.Gas),
	}, nil
}

func kyberChainSlug(chain string) string {
	switch strings.ToLower(chain) {
	case "ethereum", "base", "polygon", "bsc":
		return strings.ToLower(chain)
	default:
		return ""
	}
}
