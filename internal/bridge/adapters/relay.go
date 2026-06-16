// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	bridge "github.com/blocksecteam/web3-companion/internal/bridge"
)

type relayBridge struct {
	bridgeHTTPAdapter
	resolver bridge.Resolver
}

type relayQuoteRequest struct {
	User                 string `json:"user"`
	Recipient            string `json:"recipient,omitempty"`
	RefundTo             string `json:"refundTo,omitempty"`
	OriginChainID        int    `json:"originChainId"`
	DestinationChainID   int    `json:"destinationChainId"`
	OriginCurrency       string `json:"originCurrency"`
	DestinationCurrency  string `json:"destinationCurrency"`
	Amount               string `json:"amount"`
	TradeType            string `json:"tradeType"`
	UseExternalLiquidity bool   `json:"useExternalLiquidity,omitempty"`
	UseDepositAddress    bool   `json:"useDepositAddress,omitempty"`
}

type relayQuoteResponse struct {
	Details struct {
		CurrencyOut struct {
			Amount          string `json:"amount"`
			AmountFormatted string `json:"amountFormatted"`
		} `json:"currencyOut"`
	} `json:"details"`
	Steps []relayStep `json:"steps"`
	Error string      `json:"error"`
}

type relayStep struct {
	Items []relayItem `json:"items"`
}

type relayItem struct {
	Data relayTxData `json:"data"`
}

type relayTxData struct {
	To    string `json:"to"`
	Data  string `json:"data"`
	Value string `json:"value"`
	Gas   string `json:"gas"`
}

// NewRelayBridge constructs a Relay adapter. The resolver is used by Quote to
// look up destination-token decimals when Relay's /quote response only carries
// `Details.CurrencyOut.AmountFormatted` (a human-readable string). A nil
// resolver still produces working quotes when the API includes the raw
// CurrencyOut.Amount field; quotes that need decimals will error.
func NewRelayBridge(resolver bridge.Resolver) bridge.Adapter {
	return &relayBridge{
		bridgeHTTPAdapter: newBridgeHTTPAdapter("relay", relayDefaultURL, "RELAY_API_URL"),
		resolver:          resolver,
	}
}

func (a *relayBridge) Name() string { return "relay" }

func (a *relayBridge) SupportedPairs() []bridge.ChainPair {
	chains := []string{"ethereum", "base", "polygon", "bsc"}
	return bridgePairs(chains, chains)
}

// Capabilities: Relay is a deposit-address bridge (UseDepositAddress=true),
// so the user transfers tokens directly to a deposit address — no on-chain
// approve, no slippage parameter — the bridge service determines the
// destination amount at execution time. Accepts and emits both native and
// WETH on both sides via the origin/destination currency address passed to
// /quote.
func (a *relayBridge) Capabilities() bridge.BridgeCapabilities {
	return bridge.BridgeCapabilities{
		Native: bridge.NativeCapability{
			AcceptsNativeIn: true,
			AcceptsWETHIn:   true,
			OutputsNative:   true,
			OutputsWETH:     true,
		},
		Approve:          bridge.ApproveCapability{Mode: bridge.ApproveModeDeposit},
		SupportsSlippage: false,
	}
}

func (a *relayBridge) Quote(ctx context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	body, err := a.quoteBody(req)
	if err != nil {
		return nil, err
	}

	var quote relayQuoteResponse
	if err := a.postJSON(ctx, "/quote", body, &quote); err != nil {
		return nil, err
	}
	if quote.Error != "" {
		return nil, fmt.Errorf("relay quote error: %s", quote.Error)
	}

	amount, err := a.relayOutputAmount(ctx, req, quote, resolveBridgeToToken(req, zeroNativeToken))
	if err != nil {
		return nil, err
	}
	if amount == "" || amount == "0" {
		return nil, fmt.Errorf("relay: no route")
	}
	if len(quote.Steps) > 1 {
		return nil, fmt.Errorf("relay: multi-step routes are not supported in this version")
	}
	if len(quote.Steps) == 0 || len(quote.Steps[0].Items) == 0 {
		return nil, fmt.Errorf("relay: quote returned no executable step")
	}

	tx := quote.Steps[0].Items[0].Data
	raw := map[string]interface{}{
		"to":    tx.To,
		"value": tx.Value,
		"data":  tx.Data,
		"gas":   tx.Gas,
	}

	return &bridge.QuoteResult{
		Bridge:           a.Name(),
		InputAmount:      req.Amount,
		EstimatedReceive: amount,
		DepositAddress:   relayDepositAddress(quote),
		RawQuote:         raw,
	}, nil
}

func (a *relayBridge) Build(_ context.Context, req bridge.BuildRequest) (*bridge.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("relay: invalid quote format")
	}
	to := stringFromMap(raw, "to")
	if to == "" {
		return nil, fmt.Errorf("relay: quote response missing executable calldata")
	}
	return buildBridgeResult("relay", to, stringFromMap(raw, "value"), stringFromMap(raw, "data"), parseGas(stringFromMap(raw, "gas")))
}

func (a *relayBridge) quoteBody(req bridge.QuoteRequest) (relayQuoteRequest, error) {
	fromChainID, err := resolveBridgeChainID(req.FromChain)
	if err != nil {
		return relayQuoteRequest{}, fmt.Errorf("relay: %w", err)
	}
	toChainID, err := resolveBridgeChainID(req.ToChain)
	if err != nil {
		return relayQuoteRequest{}, fmt.Errorf("relay: %w", err)
	}
	body := relayQuoteRequest{
		User:                 req.FromAddress,
		OriginChainID:        fromChainID,
		DestinationChainID:   toChainID,
		OriginCurrency:       normalizeNativeToken(req.FromToken, zeroNativeToken),
		DestinationCurrency:  resolveBridgeToToken(req, zeroNativeToken),
		Amount:               req.Amount,
		TradeType:            "EXACT_INPUT",
		UseExternalLiquidity: true,
		UseDepositAddress:    true,
	}
	if recv := receiveAddress(req); recv != "" {
		body.Recipient = recv
	}
	if req.FromAddress != "" {
		body.RefundTo = req.FromAddress
	}
	return body, nil
}

// relayOutputAmount returns the destination amount as a raw integer string.
// Prefers Details.CurrencyOut.Amount when present (already raw). Falls back
// to Details.CurrencyOut.AmountFormatted, but that field is a decimal string
// and must be scaled by the destination token's real decimals — resolved via
// the adapter's bridge.Resolver.
//
// Do NOT use Steps[].Items[].Data.Value as a fallback: that field is the
// source-side msg.value, not the destination amount, and would silently
// report the wrong number.
func (a *relayBridge) relayOutputAmount(ctx context.Context, req bridge.QuoteRequest, quote relayQuoteResponse, toToken string) (string, error) {
	if quote.Details.CurrencyOut.Amount != "" {
		return quote.Details.CurrencyOut.Amount, nil
	}
	if quote.Details.CurrencyOut.AmountFormatted == "" {
		return "", fmt.Errorf("relay: quote response missing CurrencyOut.Amount and AmountFormatted")
	}
	if a.resolver == nil {
		return "", fmt.Errorf("relay: AmountFormatted path requires a decimals resolver; pass one to NewRelayBridge")
	}
	decimals, err := a.resolver.Resolve(ctx, req.ToChain, toToken)
	if err != nil {
		return "", fmt.Errorf("relay: resolve destination decimals for %s on %s: %w", toToken, req.ToChain, err)
	}
	return decimalFormattedToComparable(quote.Details.CurrencyOut.AmountFormatted, int(decimals)), nil
}

func decimalFormattedToComparable(formatted string, decimals int) string {
	parts := strings.SplitN(formatted, ".", 2)
	whole := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > decimals {
		frac = frac[:decimals]
	}
	for len(frac) < decimals {
		frac += "0"
	}
	raw := strings.TrimLeft(whole+frac, "0")
	if raw == "" {
		return "0"
	}
	if _, ok := new(big.Int).SetString(raw, 10); !ok {
		return "0"
	}
	return raw
}

func relayDepositAddress(quote relayQuoteResponse) string {
	if len(quote.Steps) == 0 || len(quote.Steps[0].Items) == 0 {
		return ""
	}
	tx := quote.Steps[0].Items[0].Data
	if tx.Value != "" && tx.Value != "0" {
		return strings.ToLower(tx.To)
	}
	data := strings.TrimPrefix(tx.Data, "0x")
	if len(data) >= 72 && strings.HasPrefix(data, "a9059cbb") {
		return strings.ToLower("0x" + data[32:72])
	}
	return strings.ToLower(tx.To)
}
