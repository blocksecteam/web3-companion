// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"net/http"
	"testing"

	bridgeapi "github.com/blocksecteam/web3-companion/internal/bridge"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

type rankedBridgeAdapter struct {
	name    string
	receive string
}

func (a rankedBridgeAdapter) Name() string { return a.name }

func (a rankedBridgeAdapter) Capabilities() bridgeapi.BridgeCapabilities {
	return bridgeapi.BridgeCapabilities{
		Native:           bridgeapi.NativeCapability{AcceptsNativeIn: true, AcceptsWETHIn: true, OutputsNative: true, OutputsWETH: true},
		Approve:          bridgeapi.ApproveCapability{Mode: bridgeapi.ApproveModeDeposit},
		SupportsSlippage: false,
	}
}

func (a rankedBridgeAdapter) SupportedPairs() []bridgeapi.ChainPair {
	return []bridgeapi.ChainPair{{FromChain: "ethereum", ToChain: "base"}}
}

func (a rankedBridgeAdapter) Quote(_ context.Context, req bridgeapi.QuoteRequest) (*bridgeapi.QuoteResult, error) {
	return &bridgeapi.QuoteResult{
		Bridge:           a.name,
		InputAmount:      req.Amount,
		EstimatedReceive: a.receive,
	}, nil
}

func (a rankedBridgeAdapter) Build(_ context.Context, _ bridgeapi.BuildRequest) (*bridgeapi.BuildResult, error) {
	return nil, nil
}

func TestBridgeQuoteReturnsValidRoutesSortedByReceiveAmount(t *testing.T) {
	r, apiKey := setupBridgeWithOptions(
		t,
		[]bridgeapi.HandlerOption{bridgeapi.WithResolver(&fakeResolver{decimals: 18})},
		rankedBridgeAdapter{name: "small", receive: "9"},
		rankedBridgeAdapter{name: "large", receive: "100"},
		rankedBridgeAdapter{name: "invalid", receive: "not-a-number"},
	)

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/bridge/quote", apiKey, map[string]interface{}{
		"from_chain":   "ethereum",
		"to_chain":     "base",
		"from_token":   "native",
		"to_token":     "native",
		"amount":       "1",
		"from_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("quote = %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Bridges []struct {
			Bridge    string `json:"bridge"`
			OutAmount string `json:"out_amount"`
		} `json:"bridges"`
	}
	testutil.ParseJSON(t, w, &resp)
	if len(resp.Bridges) != 2 {
		t.Fatalf("bridges = %#v, want only two parseable positive routes", resp.Bridges)
	}
	if resp.Bridges[0].Bridge != "large" || resp.Bridges[0].OutAmount != "100" {
		t.Fatalf("first bridge = %#v, want highest numeric receive amount", resp.Bridges[0])
	}
	if resp.Bridges[1].Bridge != "small" || resp.Bridges[1].OutAmount != "9" {
		t.Fatalf("second bridge = %#v, want lower numeric receive amount", resp.Bridges[1])
	}
}
