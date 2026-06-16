// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/token"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	zeroAddress       = "0x0000000000000000000000000000000000000000"
	eip155NativeToken = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"

	defaultSlippagePercent = 0.5
	defaultSlippageBPS     = 50
	defaultGasLimit        = 300000
)

var chainIDs = map[string]int{
	"ethereum":  1,
	"base":      8453,
	"polygon":   137,
	"arbitrum":  42161,
	"optimism":  10,
	"bsc":       56,
	"avalanche": 43114,
}

// wrappedNativeByChain looks up the wrapped-native contract for a chain via the shared
// internal/token registry. Kept as a thin local helper so existing call sites can use
// the same map[chain]address access pattern.
func wrappedNativeForChain(chain string) string {
	addr, _ := token.WrappedNativeAddress(chain)
	return addr
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); v != "" {
		return v
	}
	return fallback
}

func ConfigureHTTPForTest(adapter interface{}, client *http.Client, baseURL string) {
	switch a := adapter.(type) {
	case *OdosAdapter:
		a.client = client
		a.baseURL = baseURL
	case *KyberAdapter:
		a.client = client
		a.baseURL = baseURL
	case *OpenOceanAdapter:
		a.client = client
		a.baseURL = baseURL
	case *ParaSwapAdapter:
		a.client = client
		a.baseURL = baseURL
	}
}

func normalizeNativeToken(tokenAddr, nativeAddress string) string {
	if token.IsNativeToken(tokenAddr) {
		return nativeAddress
	}
	return tokenAddr
}

func chainID(chain string) (int, bool) {
	id, ok := chainIDs[strings.ToLower(chain)]
	return id, ok
}

func slippagePercent(v float64) float64 {
	if v > 0 {
		return v
	}
	return defaultSlippagePercent
}

func doJSON(ctx context.Context, client *http.Client, method, requestURL string, body interface{}, headers map[string]string, out interface{}) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d for %s: %s", resp.StatusCode, requestURL, trimForError(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	return nil
}

func getJSON(ctx context.Context, client *http.Client, baseURL, path string, params url.Values, out interface{}, headers map[string]string) error {
	requestURL := strings.TrimRight(baseURL, "/") + path
	if encoded := params.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	return doJSON(ctx, client, http.MethodGet, requestURL, nil, headers, out)
}

func postJSON(ctx context.Context, client *http.Client, baseURL, path string, body interface{}, out interface{}, headers map[string]string) error {
	requestURL := strings.TrimRight(baseURL, "/") + path
	return doJSON(ctx, client, http.MethodPost, requestURL, body, headers, out)
}

func trimForError(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func rawQuoteMap(v interface{}) map[string]interface{} {
	switch q := v.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return q
	case map[string]string:
		out := make(map[string]interface{}, len(q))
		for k, v := range q {
			out[k] = v
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

func stringValue(m map[string]interface{}, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return ""
	}
}

func boolValue(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func intValue(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := strconv.Atoi(v.String())
		return i
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

func bigString(raw string) *big.Int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		if _, ok := n.SetString(raw[2:], 16); ok {
			return n
		}
		return big.NewInt(0)
	}
	if _, ok := n.SetString(raw, 10); ok {
		return n
	}
	return big.NewInt(0)
}

func hexFromString(raw string) string {
	n := bigString(raw)
	if n.Sign() == 0 {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

func parseGas(raw string) uint64 {
	n := bigString(raw)
	if n.Sign() <= 0 {
		return defaultGasLimit
	}
	return n.Uint64()
}

func deadlineSeconds() int64 {
	return time.Now().Add(5 * time.Minute).Unix()
}

func abiType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}

func packArgs(types []string, values ...interface{}) []byte {
	args := make(abi.Arguments, len(types))
	for i, typ := range types {
		args[i] = abi.Argument{Type: abiType(typ)}
	}
	data, err := args.Pack(values...)
	if err != nil {
		panic(err)
	}
	return data
}

func selector(hexSelector string, packed []byte) string {
	return "0x" + hexSelector + common.Bytes2Hex(packed)
}
func RegisterAll(reg *swap.Registry, resolver *swap.DecimalsResolver, rpcURLs ...map[string]string) {
	onchainRPCURLs := map[string]string(nil)
	if len(rpcURLs) > 0 {
		onchainRPCURLs = rpcURLs[0]
	}
	RegisterAllWithRPCProvider(reg, resolver, func() map[string]string {
		return onchainRPCURLs
	})
}

func RegisterAllWithRPCProvider(reg *swap.Registry, resolver *swap.DecimalsResolver, provider func() map[string]string) {
	reg.Register(NewOdosAdapter())
	reg.Register(NewKyberAdapter())
	reg.Register(NewParaSwapAdapter(resolver))
	reg.Register(NewOpenOceanAdapter())
	reg.Register(NewUniswapAdapterWithRPCProvider(provider))
	reg.Register(NewPancakeAdapterWithRPCProvider(provider))
	reg.Register(NewLiFiAdapter())
}
