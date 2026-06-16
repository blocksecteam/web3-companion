// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	bridge "github.com/blocksecteam/web3-companion/internal/bridge"
)

type bungeeBridge struct {
	bridgeHTTPAdapter
}

type bungeeQuoteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Result  struct {
		AutoRoute *bungeeRoute `json:"autoRoute"`
		Deposit   *struct {
			DepositData *struct {
				Address string `json:"address"`
			} `json:"depositData"`
		} `json:"deposit"`
	} `json:"result"`
}

type bungeeRoute struct {
	Output struct {
		Amount string `json:"amount"`
	} `json:"output"`
	TxData *struct {
		To    string `json:"to"`
		Data  string `json:"data"`
		Value string `json:"value"`
	} `json:"txData"`
}

func NewBungeeBridge() bridge.Adapter {
	return &bungeeBridge{
		bridgeHTTPAdapter: newBridgeHTTPAdapter("bungee", bungeeDefaultURL, "BUNGEE_API_URL"),
	}
}

func (a *bungeeBridge) Name() string { return "bungee" }

func (a *bungeeBridge) SupportedPairs() []bridge.ChainPair {
	chains := []string{"ethereum", "base", "polygon", "bsc"}
	return bridgePairs(chains, chains)
}

// Capabilities: Bungee is a deposit-address bridge (enableDepositAddress=true),
// so the user sends tokens directly to a per-quote deposit address — no
// on-chain approve, no slippage parameter — the bridge service determines
// the destination amount at execution time. Accepts and emits both native
// and WETH on both sides; the API takes whatever input/output token address
// you pass in.
func (a *bungeeBridge) Capabilities() bridge.BridgeCapabilities {
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

func (a *bungeeBridge) Quote(ctx context.Context, req bridge.QuoteRequest) (*bridge.QuoteResult, error) {
	fromChainID, err := resolveBridgeChainID(req.FromChain)
	if err != nil {
		return nil, fmt.Errorf("bungee: %w", err)
	}
	toChainID, err := resolveBridgeChainID(req.ToChain)
	if err != nil {
		return nil, fmt.Errorf("bungee: %w", err)
	}

	params := url.Values{}
	params.Set("originChainId", fmt.Sprintf("%d", fromChainID))
	params.Set("destinationChainId", fmt.Sprintf("%d", toChainID))
	params.Set("inputToken", normalizeNativeToken(req.FromToken, eip155NativeToken))
	params.Set("outputToken", resolveBridgeToToken(req, eip155NativeToken))
	params.Set("inputAmount", req.Amount)
	params.Set("receiverAddress", receiveAddress(req))
	params.Set("userAddress", req.FromAddress)
	params.Set("refundAddress", req.FromAddress)
	params.Set("enableDepositAddress", "true")

	var quote bungeeQuoteResponse
	if err := a.getJSON(ctx, "/bungee/quote", params, &quote); err != nil {
		return nil, err
	}
	if quote.Message != "" && !quote.Success {
		return nil, fmt.Errorf("bungee: %s", quote.Message)
	}

	// Bungee's response can be auto-routed (single best route, executable) or
	// manual (multiple candidates the user must choose between). We only accept
	// auto-routed quotes because the handler has no choice-making logic — using
	// ManualRoutes here would produce a quote that ranks but cannot Build.
	autoRoute := quote.Result.AutoRoute
	if autoRoute == nil || autoRoute.TxData == nil || autoRoute.TxData.To == "" {
		return nil, fmt.Errorf("bungee: no executable autoRoute (manual route choice required)")
	}
	if amount := autoRoute.Output.Amount; amount == "" || amount == "0" {
		return nil, fmt.Errorf("bungee: autoRoute returned zero amount")
	}

	raw := map[string]interface{}{
		"to":    autoRoute.TxData.To,
		"value": autoRoute.TxData.Value,
		"data":  autoRoute.TxData.Data,
	}

	var depositAddress string
	if quote.Result.Deposit != nil && quote.Result.Deposit.DepositData != nil {
		depositAddress = strings.ToLower(quote.Result.Deposit.DepositData.Address)
	}

	return &bridge.QuoteResult{
		Bridge:           a.Name(),
		InputAmount:      req.Amount,
		EstimatedReceive: autoRoute.Output.Amount,
		DepositAddress:   depositAddress,
		RawQuote:         raw,
	}, nil
}

func (a *bungeeBridge) Build(_ context.Context, req bridge.BuildRequest) (*bridge.BuildResult, error) {
	raw := rawQuoteMap(req.Quote)
	if raw == nil {
		return nil, fmt.Errorf("bungee: invalid quote format")
	}
	to := stringFromMap(raw, "to")
	if to == "" {
		return nil, fmt.Errorf("bungee: quote response missing autoRoute.txData")
	}
	return buildBridgeResult("bungee", to, stringFromMap(raw, "value"), stringFromMap(raw, "data"), defaultBridgeGas)
}
