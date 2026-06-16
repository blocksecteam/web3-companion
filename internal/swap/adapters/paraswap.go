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

type ParaSwapAdapter struct {
	client   *http.Client
	baseURL  string
	resolver *swap.DecimalsResolver
}

func NewParaSwapAdapter(resolver *swap.DecimalsResolver) *ParaSwapAdapter {
	return &ParaSwapAdapter{
		client:   &http.Client{Timeout: 30 * time.Second},
		baseURL:  envOrDefault("PARASWAP_API_URL", "https://api.paraswap.io"),
		resolver: resolver,
	}
}

func (a *ParaSwapAdapter) Name() string { return "paraswap" }

func (a *ParaSwapAdapter) SupportedChains() []string {
	return []string{"ethereum", "base", "polygon", "bsc"}
}

// Capabilities: ParaSwap accepts/outputs both native and WETH on both sides;
// the build API takes slippage and respects it server-side.
func (a *ParaSwapAdapter) Capabilities() swap.SwapCapabilities {
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

func (a *ParaSwapAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	network, ok := chainID(req.Chain)
	if !ok || !paraswapSupported(req.Chain) {
		return nil, fmt.Errorf("paraswap: unsupported chain %s", req.Chain)
	}
	src := normalizeNativeToken(req.SourceAsset, eip155NativeToken)
	dst := normalizeNativeToken(req.TargetAsset, eip155NativeToken)
	if a.resolver == nil {
		return nil, fmt.Errorf("paraswap: decimals resolver not configured")
	}
	srcDec, err := a.resolver.Resolve(ctx, req.Chain, src)
	if err != nil {
		return nil, fmt.Errorf("paraswap: resolve src decimals: %w", err)
	}
	dstDec, err := a.resolver.Resolve(ctx, req.Chain, dst)
	if err != nil {
		return nil, fmt.Errorf("paraswap: resolve dest decimals: %w", err)
	}
	params := url.Values{}
	params.Set("srcToken", src)
	params.Set("destToken", dst)
	params.Set("amount", req.Amount)
	params.Set("srcDecimals", fmt.Sprintf("%d", srcDec))
	params.Set("destDecimals", fmt.Sprintf("%d", dstDec))
	params.Set("side", "SELL")
	params.Set("network", fmt.Sprintf("%d", network))

	var q paraswapQuoteResponse
	if err := getJSON(ctx, a.client, a.baseURL, "/prices", params, &q, nil); err != nil {
		return nil, fmt.Errorf("paraswap: quote request: %w", err)
	}
	if q.Error != "" {
		return nil, fmt.Errorf("paraswap API error: %s", q.Error)
	}
	var priceRouteFields struct {
		DestAmount         string `json:"destAmount"`
		TokenTransferProxy string `json:"tokenTransferProxy"`
	}
	if err := json.Unmarshal(q.PriceRoute, &priceRouteFields); err != nil {
		return nil, fmt.Errorf("paraswap: parse priceRoute: %w", err)
	}
	if bigString(priceRouteFields.DestAmount).Sign() <= 0 {
		return nil, fmt.Errorf("paraswap: quote returned zero output amount")
	}
	raw := map[string]interface{}{
		"price_route": q.PriceRoute,
		"network":     network,
		"src_token":   src,
		"dest_token":  dst,
		"src_amount":  req.Amount,
		"swapper":     req.FromAddress,
	}
	return &swap.QuoteResult{
		Protocol:      "paraswap",
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		InputAmount:   req.Amount,
		OutputAmount:  priceRouteFields.DestAmount,
		ApproveStyle:  "erc20",
		ApproveTarget: priceRouteFields.TokenTransferProxy,
		RawQuote:      raw,
	}, nil
}

func (a *ParaSwapAdapter) Build(ctx context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("paraswap: invalid quote format")
	}
	priceRoute := raw["price_route"]
	if priceRoute == nil {
		return nil, fmt.Errorf("paraswap: missing price_route in quote")
	}
	network := intValue(raw, "network")
	slippage := defaultSlippageBPS
	if req.SlippageBPS > 0 {
		slippage = req.SlippageBPS
	}
	body := map[string]interface{}{
		"srcToken":    stringValue(raw, "src_token"),
		"destToken":   stringValue(raw, "dest_token"),
		"srcAmount":   stringValue(raw, "src_amount"),
		"priceRoute":  priceRoute,
		"userAddress": stringValue(raw, "swapper"),
		"slippage":    slippage,
	}
	params := url.Values{}
	params.Set("ignoreChecks", "true")
	requestPath := fmt.Sprintf("/transactions/%d?%s", network, params.Encode())
	var b paraswapBuildResponse
	if err := postJSON(ctx, a.client, a.baseURL, requestPath, body, &b, nil); err != nil {
		return nil, fmt.Errorf("paraswap: build request: %w", err)
	}
	if b.Error != "" {
		return nil, fmt.Errorf("paraswap build error: %s", b.Error)
	}
	if b.To == "" || b.Data == "" {
		return nil, fmt.Errorf("paraswap: build response missing executable calldata")
	}
	return &swap.BuildResult{
		To:       b.To,
		Value:    hexFromString(b.Value),
		Data:     b.Data,
		GasLimit: parseGas(b.Gas),
	}, nil
}

type paraswapQuoteResponse struct {
	PriceRoute json.RawMessage `json:"priceRoute"`
	Error      string          `json:"error"`
}

type paraswapBuildResponse struct {
	To    string `json:"to"`
	Data  string `json:"data"`
	Value string `json:"value"`
	Gas   string `json:"gas"`
	Error string `json:"error"`
}

func paraswapSupported(chain string) bool {
	switch strings.ToLower(chain) {
	case "ethereum", "base", "polygon", "bsc":
		return true
	default:
		return false
	}
}
