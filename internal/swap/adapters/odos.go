// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

type OdosAdapter struct {
	client  *http.Client
	baseURL string
}

func NewOdosAdapter() *OdosAdapter {
	return &OdosAdapter{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: envOrDefault("ODOS_API_URL", "https://api.odos.xyz"),
	}
}

func (a *OdosAdapter) Name() string { return "odos" }

// SupportedChains lists the chains this adapter routes on, aligned with
// the deployment chain whitelist in docker/config.toml. Odos' upstream
// API also covers arbitrum/optimism/avalanche, but those chains are not
// enabled here — listing them would mislead the /swap/protocols surface
// without any user-reachable effect. Extend only after adding the chain
// to config.toml.
func (a *OdosAdapter) SupportedChains() []string {
	return []string{"ethereum", "base", "polygon", "bsc"}
}

// Capabilities: Odos accepts/outputs both native and WETH on both sides; the
// /quote API takes slippageLimitPercent and respects it server-side.
func (a *OdosAdapter) Capabilities() swap.SwapCapabilities {
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

var odosChainMap = map[string]int{
	"ethereum": 1, "base": 8453, "polygon": 137, "arbitrum": 42161,
	"optimism": 10, "bsc": 56, "avalanche": 43114,
}

type odosTokenAmount struct {
	TokenAddress string `json:"tokenAddress"`
	Amount       string `json:"amount"`
}

type odosTokenProportion struct {
	TokenAddress string `json:"tokenAddress"`
	Proportion   int    `json:"proportion"`
}

type odosQuoteRequest struct {
	ChainID              int                   `json:"chainId"`
	InputTokens          []odosTokenAmount     `json:"inputTokens"`
	OutputTokens         []odosTokenProportion `json:"outputTokens"`
	UserAddr             string                `json:"userAddr"`
	SlippageLimitPercent float64               `json:"slippageLimitPercent"`
	Compact              bool                  `json:"compact"`
	ReferralCode         int                   `json:"referralCode"`
}

type odosQuoteResponse struct {
	InTokens    []string `json:"inTokens"`
	OutTokens   []string `json:"outTokens"`
	InAmounts   []string `json:"inAmounts"`
	OutAmounts  []string `json:"outAmounts"`
	GasEstimate float64  `json:"gasEstimate"`
	PathID      string   `json:"pathId"`
	PriceImpact float64  `json:"priceImpact"`
	PercentDiff float64  `json:"percentDiff"`
}

func (a *OdosAdapter) Quote(ctx context.Context, req swap.QuoteRequest) (*swap.QuoteResult, error) {
	chainID, ok := odosChainMap[req.Chain]
	if !ok {
		return nil, fmt.Errorf("odos: unsupported chain %s", req.Chain)
	}

	body := odosQuoteRequest{
		ChainID: chainID,
		InputTokens: []odosTokenAmount{
			{TokenAddress: normalizeNativeToken(req.SourceAsset, zeroAddress), Amount: req.Amount},
		},
		OutputTokens: []odosTokenProportion{
			{TokenAddress: normalizeNativeToken(req.TargetAsset, zeroAddress), Proportion: 1},
		},
		UserAddr:             req.FromAddress,
		SlippageLimitPercent: slippagePercent(req.Slippage),
		Compact:              true,
		ReferralCode:         0,
	}

	resp, err := a.doPost(ctx, "/sor/quote/v2", body)
	if err != nil {
		return nil, err
	}

	var q odosQuoteResponse
	if err := json.Unmarshal(resp, &q); err != nil {
		return nil, fmt.Errorf("odos: parse quote: %w", err)
	}
	if q.PathID == "" {
		return nil, fmt.Errorf("odos: quote missing pathId")
	}

	outAmount := "0"
	if len(q.OutAmounts) > 0 {
		outAmount = q.OutAmounts[0]
	}
	if bigString(outAmount).Sign() <= 0 {
		return nil, fmt.Errorf("odos: quote returned zero output amount")
	}

	return &swap.QuoteResult{
		Protocol:     "odos",
		SourceAsset:  req.SourceAsset,
		TargetAsset:  req.TargetAsset,
		InputAmount:  req.Amount,
		OutputAmount: outAmount,
		ApproveStyle: "erc20",
		// Spender equals the swap router, which is only returned by /sor/assemble
		// during Build. Leaving ApproveTarget empty lets the handler fall back to
		// build.To at approve time.
		ApproveTarget: "",
		RawQuote: map[string]interface{}{
			"path_id": q.PathID,
		},
	}, nil
}

type odosBuildRequest struct {
	UserAddr string `json:"userAddr"`
	PathID   string `json:"pathId"`
	Simulate bool   `json:"simulate"`
}

type odosBuildResponse struct {
	Transaction struct {
		To    string `json:"to"`
		Value string `json:"value"`
		Data  string `json:"data"`
		Gas   uint64 `json:"gas"`
	} `json:"transaction"`
}

func (a *OdosAdapter) Build(ctx context.Context, req swap.BuildRequest) (*swap.BuildResult, error) {
	if req.FromAddress == "" {
		return nil, fmt.Errorf("odos: from_address is required")
	}
	raw := rawQuoteMap(req.Quote)
	pathID := ""
	if raw != nil {
		pathID = stringValue(raw, "path_id")
	}
	body := odosBuildRequest{
		UserAddr: req.FromAddress,
		PathID:   pathID,
		Simulate: false,
	}
	if body.PathID == "" {
		return nil, fmt.Errorf("odos: quote missing pathId")
	}
	resp, err := a.doPost(ctx, "/sor/assemble", body)
	if err != nil {
		return nil, err
	}
	var b odosBuildResponse
	if err := json.Unmarshal(resp, &b); err != nil {
		return nil, fmt.Errorf("odos: parse build: %w", err)
	}
	return &swap.BuildResult{
		To:       b.Transaction.To,
		Value:    hexFromString(b.Transaction.Value),
		Data:     b.Transaction.Data,
		GasLimit: b.Transaction.Gas,
	}, nil
}

func (a *OdosAdapter) doPost(ctx context.Context, path string, body interface{}) ([]byte, error) {
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("odos: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("odos: request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("odos: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("odos API error (HTTP %d) for %s: %s", resp.StatusCode, a.baseURL+path, string(data))
	}

	return data, nil
}
