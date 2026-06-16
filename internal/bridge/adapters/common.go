// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	bridge "github.com/blocksecteam/web3-companion/internal/bridge"
)

const (
	bungeeDefaultURL = "https://public-backend.bungee.exchange/api/v1"
	relayDefaultURL  = "https://api.relay.link"

	eip155NativeToken = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"
	zeroNativeToken   = "0x0000000000000000000000000000000000000000"

	defaultBridgeGas = 300000
)

var bridgeChainIDs = map[string]int{
	"ethereum":  1,
	"arbitrum":  42161,
	"optimism":  10,
	"base":      8453,
	"polygon":   137,
	"bsc":       56,
	"avalanche": 43114,
	"zora":      7777777,
	"blast":     81457,
}

var canonicalBridgeTokens = map[string]map[string]string{
	"usdc": {
		"ethereum": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"arbitrum": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
		"optimism": "0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85",
		"base":     "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"polygon":  "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359",
		"bsc":      "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d",
	},
}

type bridgeHTTPAdapter struct {
	name    string
	baseURL string
	client  *http.Client
}

func newBridgeHTTPAdapter(name, defaultURL, envKey string) bridgeHTTPAdapter {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv(envKey)), "/")
	if baseURL == "" {
		baseURL = defaultURL
	}
	return bridgeHTTPAdapter{
		name:    name,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// NewHTTPBridgeAdapterForTest constructs a single bridge adapter pointed at
// `baseURL`. The resolver is forwarded to adapters that need it (relay) and
// may be nil; if nil, code paths that require decimals resolution return an
// error rather than guessing.
func NewHTTPBridgeAdapterForTest(name, baseURL string, client *http.Client, resolver bridge.Resolver) bridge.Adapter {
	httpAdapter := bridgeHTTPAdapter{name: name, baseURL: strings.TrimRight(baseURL, "/"), client: client}
	if httpAdapter.client == nil {
		httpAdapter.client = &http.Client{Timeout: 30 * time.Second}
	}
	switch name {
	case "bungee":
		return &bungeeBridge{bridgeHTTPAdapter: httpAdapter}
	case "relay":
		return &relayBridge{bridgeHTTPAdapter: httpAdapter, resolver: resolver}
	default:
		return nil
	}
}

func (a bridgeHTTPAdapter) getJSON(ctx context.Context, path string, params url.Values, out interface{}) error {
	reqURL := a.baseURL + path
	if encoded := params.Encode(); encoded != "" {
		reqURL += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("%s: create request: %w", a.name, err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request: %w", a.name, err)
	}
	defer resp.Body.Close()
	return decodeProviderResponse(a.name, reqURL, resp, out)
}

func (a bridgeHTTPAdapter) postJSON(ctx context.Context, path string, body interface{}, out interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: encode request: %w", a.name, err)
	}
	reqURL := a.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%s: create request: %w", a.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request: %w", a.name, err)
	}
	defer resp.Body.Close()
	return decodeProviderResponse(a.name, reqURL, resp, out)
}

func decodeProviderResponse(name, reqURL string, resp *http.Response, out interface{}) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s: read response: %w", name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s API error (HTTP %d) for %s: %s", name, resp.StatusCode, reqURL, trimForError(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: parse response: %w", name, err)
	}
	return nil
}

func trimForError(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func resolveBridgeChainID(chain string) (int, error) {
	if id, ok := bridgeChainIDs[strings.ToLower(chain)]; ok {
		return id, nil
	}
	id, err := strconv.Atoi(chain)
	if err == nil && id > 0 {
		return id, nil
	}
	return 0, fmt.Errorf("unknown chain: %q", chain)
}

func normalizeNativeToken(tok, nativeAddress string) string {
	t := strings.ToLower(strings.TrimSpace(tok))
	switch t {
	case "", "native", "0x0", strings.ToLower(eip155NativeToken), zeroNativeToken:
		return nativeAddress
	default:
		return tok
	}
}

func NormalizeNativeTokenForTest(tok, nativeAddress string) string {
	return normalizeNativeToken(tok, nativeAddress)
}

func resolveBridgeToToken(req bridge.QuoteRequest, nativeAddress string) string {
	if strings.TrimSpace(req.ToToken) != "" {
		return normalizeNativeToken(req.ToToken, nativeAddress)
	}
	fromToken := strings.ToLower(req.FromToken)
	for _, byChain := range canonicalBridgeTokens {
		if strings.EqualFold(byChain[strings.ToLower(req.FromChain)], fromToken) {
			if target := byChain[strings.ToLower(req.ToChain)]; target != "" {
				return target
			}
		}
	}
	return normalizeNativeToken(req.FromToken, nativeAddress)
}

func receiveAddress(req bridge.QuoteRequest) string {
	return req.FromAddress
}

func parseGas(raw string) uint64 {
	if raw == "" {
		return defaultBridgeGas
	}
	base := 10
	value := raw
	if strings.HasPrefix(raw, "0x") {
		base = 16
		value = strings.TrimPrefix(raw, "0x")
	}
	gas, err := strconv.ParseUint(value, base, 64)
	if err != nil || gas == 0 {
		return defaultBridgeGas
	}
	return gas
}

func decimalFromHexOrString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "0"
	}
	if strings.HasPrefix(raw, "0x") {
		value, ok := new(big.Int).SetString(strings.TrimPrefix(raw, "0x"), 16)
		if !ok {
			return "0"
		}
		return value.String()
	}
	return raw
}

func bridgePairs(fromChains, toChains []string) []bridge.ChainPair {
	pairs := make([]bridge.ChainPair, 0, len(fromChains)*len(toChains))
	for _, from := range fromChains {
		for _, to := range toChains {
			if from == to {
				continue
			}
			pairs = append(pairs, bridge.ChainPair{FromChain: from, ToChain: to})
		}
	}
	return pairs
}

// rawQuoteMap normalizes a BuildRequest.Quote (which can be a map produced by
// the adapter's Quote(), or the same map after JSON round-trip) into map[string]interface{}.
// Returns nil if the value isn't map-shaped.
func rawQuoteMap(v interface{}) map[string]interface{} {
	switch q := v.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return q
	case map[string]string:
		out := make(map[string]interface{}, len(q))
		for k, s := range q {
			out[k] = s
		}
		return out
	default:
		data, err := json.Marshal(q)
		if err != nil {
			return nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	switch v := m[key].(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

// buildBridgeResult assembles a BuildResult from raw fields. The name parameter
// is used only for error messages (it identifies which adapter failed); the result
// itself no longer carries the bridge identifier — that is recovered at the handler
// layer via adapter.Name().
func buildBridgeResult(name, to, value, data string, gas uint64) (*bridge.BuildResult, error) {
	if to == "" || data == "" || !strings.HasPrefix(data, "0x") {
		return nil, fmt.Errorf("%s: build response missing executable calldata", name)
	}
	return &bridge.BuildResult{
		To:       to,
		Value:    decimalFromHexOrString(value),
		Data:     data,
		GasLimit: gas,
	}, nil
}

// RegisterAll registers all built-in bridge adapters.
// To add a new protocol, simply call reg.Register(NewXxxBridge(...)) in this function.
//
// Approve-mode bridges have their allowance/approve calls computed by the handler
// via bridge.ComputeApproveSequence; adapters do not touch RPC themselves.
//
// The resolver is forwarded to adapters that need destination-token decimals at
// Quote time (currently relay). Pass nil if no resolver is available — affected
// adapters will fall back to an error rather than guess.
func RegisterAll(reg *bridge.Registry, resolver bridge.Resolver) {
	reg.Register(NewBungeeBridge())
	reg.Register(NewRelayBridge(resolver))
	reg.Register(NewLiFiBridge())
}
