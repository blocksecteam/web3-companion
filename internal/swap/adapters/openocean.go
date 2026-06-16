// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

type OpenOceanAdapter struct {
	client  *http.Client
	baseURL string
}

func NewOpenOceanAdapter() *OpenOceanAdapter {
	return &OpenOceanAdapter{client: &http.Client{Timeout: 30 * time.Second}, baseURL: envOrDefault("OPENOCEAN_API_URL", "https://open-api.openocean.finance")}
}

func (a *OpenOceanAdapter) Name() string { return "openocean" }

func (a *OpenOceanAdapter) SupportedChains() []string {
	return []string{"ethereum", "base", "polygon", "bsc"}
}

// Capabilities: OpenOcean accepts/outputs both native and WETH on both sides;
// the /quote API takes slippage and respects it server-side.
func (a *OpenOceanAdapter) Capabilities() swap.SwapCapabilities {
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

func (a *OpenOceanAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	code := openOceanChainCode(req.Chain)
	if code == "" {
		return nil, fmt.Errorf("openocean: unsupported chain %s", req.Chain)
	}
	src := normalizeNativeToken(req.SourceAsset, eip155NativeToken)
	dst := normalizeNativeToken(req.TargetAsset, eip155NativeToken)
	params := url.Values{}
	params.Set("inTokenAddress", src)
	params.Set("outTokenAddress", dst)
	params.Set("amount", req.DisplayAmount)
	params.Set("gasPrice", "5")
	params.Set("slippage", fmt.Sprintf("%.4f", slippagePercent(req.Slippage)))

	var q openOceanQuoteResponse
	if err := getJSON(ctx, a.client, a.baseURL, "/v3/"+code+"/quote", params, &q, nil); err != nil {
		return nil, fmt.Errorf("openocean: quote request: %w", err)
	}
	if q.Code != 200 {
		return nil, fmt.Errorf("openocean API error %d: %s", q.Code, q.Message)
	}
	if bigString(q.Data.OutAmount).Sign() <= 0 {
		return nil, fmt.Errorf("openocean: quote returned zero output amount")
	}
	raw := map[string]interface{}{
		"chain_code":   code,
		"token_in":     src,
		"token_out":    dst,
		"amount_human": params.Get("amount"),
		"slippage":     params.Get("slippage"),
		"swapper":      req.FromAddress,
	}
	return &swap.QuoteResult{
		Protocol:     "openocean",
		SourceAsset:  req.SourceAsset,
		TargetAsset:  req.TargetAsset,
		InputAmount:  req.Amount,
		OutputAmount: q.Data.OutAmount,
		ApproveStyle: "erc20",
		// Spender equals the swap router, which is only returned by /v3/{chain}/swap_quote
		// during Build. Leaving ApproveTarget empty lets the handler fall back to
		// build.To at approve time.
		ApproveTarget: "",
		RawQuote:      raw,
	}, nil
}

func (a *OpenOceanAdapter) Build(ctx context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("openocean: invalid quote format")
	}
	code := stringValue(raw, "chain_code")
	if code == "" {
		return nil, fmt.Errorf("openocean: missing chain_code in quote")
	}
	params := url.Values{}
	params.Set("inTokenAddress", stringValue(raw, "token_in"))
	params.Set("outTokenAddress", stringValue(raw, "token_out"))
	params.Set("amount", stringValue(raw, "amount_human"))
	params.Set("gasPrice", "5")
	params.Set("slippage", stringValue(raw, "slippage"))
	params.Set("account", stringValue(raw, "swapper"))

	var b openOceanBuildResponse
	if err := getJSON(ctx, a.client, a.baseURL, "/v3/"+code+"/swap_quote", params, &b, nil); err != nil {
		return nil, fmt.Errorf("openocean: build request: %w", err)
	}
	if b.Code != 200 {
		return nil, fmt.Errorf("openocean build error %d: %s", b.Code, b.Message)
	}
	if b.Data.To == "" || b.Data.Data == "" {
		return nil, fmt.Errorf("openocean: build response missing executable calldata")
	}
	return &swap.BuildResult{
		To:       b.Data.To,
		Value:    hexFromString(b.Data.Value),
		Data:     b.Data.Data,
		GasLimit: parseGas(b.Data.Gas),
	}, nil
}

type openOceanQuoteResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		OutAmount string `json:"outAmount"`
	} `json:"data"`
}

type openOceanBuildResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		To    string `json:"to"`
		Data  string `json:"data"`
		Value string `json:"value"`
		Gas   string `json:"gas"`
	} `json:"data"`
}

func openOceanChainCode(chain string) string {
	switch strings.ToLower(chain) {
	case "ethereum":
		return "eth"
	case "bsc", "base", "polygon":
		return strings.ToLower(chain)
	default:
		return ""
	}
}
