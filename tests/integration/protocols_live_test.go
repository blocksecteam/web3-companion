// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/swap/adapters"
)

var usdcAmountIn = map[string]string{
	"ethereum": "1000000",
	"bsc":      "1000000000000000000",
	"base":     "1000000",
	"polygon":  "1000000",
}

func checkUSDCBalance(chain, rpcURL, token, owner string) *big.Int {
	bal, err := swap.FetchAllowance(context.Background(), rpcURL,
		token, owner, "0x0000000000000000000000000000000000000000")

	_ = bal
	_ = err

	ownerPadded := strings.ToLower(strings.TrimPrefix(owner, "0x"))
	ownerPadded = strings.Repeat("0", 64-len(ownerPadded)) + ownerPadded
	calldata := "0x70a08231" + ownerPadded
	body := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_call","params":[{"to":%q,"data":%q},"latest"],"id":1}`, token, calldata)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err2 := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, strings.NewReader(body))
	if err2 != nil {
		return big.NewInt(0)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err2 := http.DefaultClient.Do(req)
	if err2 != nil {
		return big.NewInt(0)
	}
	defer resp.Body.Close()
	var rpcResp struct {
		Result string `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	hex := strings.TrimPrefix(rpcResp.Result, "0x")
	if hex == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	n.SetString(hex, 16)
	return n
}

func simulateERC7821(t *testing.T, chainID int, blockNumber int64, sender string, calls []erc7821.HexCall, ethValue *big.Int) rpcSimulationResult {
	t.Helper()
	data, err := erc7821.EncodeExecuteHex(calls)
	if err != nil {
		t.Fatalf("encode ERC-7821 execute: %v", err)
	}
	hexValue := "0x0"
	if ethValue != nil && ethValue.Sign() > 0 {
		hexValue = "0x" + fmt.Sprintf("%x", ethValue)
	}

	return simulateRPCTrace(t, chainID, blockNumber, sender, sender, data, hexValue)
}

var chainRPCURLs = map[string]string{}

func initRPCURLs() {
	chainRPCURLs = configuredRPCURLs()
}

func currentBlockNumber(chain string) int64 {
	rpcURL, ok := chainRPCURLs[chain]
	if !ok || rpcURL == "" {
		return 0
	}
	body := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", rpcURL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var result struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Result == "" {
		return 0
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(result.Result, "0x"), 16)
	return n.Int64()
}

type liveChain struct {
	name     string
	chainID  int
	usdc     string
	amtWei   string
	amtHuman string
}

var liveChains = []liveChain{
	{"ethereum", 1, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "1000000000000000", "0.001"},
	{"bsc", 56, "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d", "100000000000000", "0.0001"},
	{"base", 8453, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", "100000000000000", "0.0001"},
	{"polygon", 137, "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", "100000000000000", "0.0001"},
}

func liveOwner() string {
	if v := os.Getenv("TEST_OWNER_ADDRESS"); v != "" {
		return v
	}
	return "0xa9C0378137f494FFCddcf862B7b52e01f0b01482"
}

func configuredOnchainRPCURLs(t *testing.T) map[string]string {
	t.Helper()
	rpc := configuredRPCURLs()
	if len(rpc) == 0 {
		return nil
	}
	return rpc
}

type liveAdapter struct {
	name    string
	adapter swap.ProtocolAdapter
}

func allLiveAdapters(t *testing.T) []liveAdapter {
	t.Helper()
	rpc := configuredOnchainRPCURLs(t)
	var resolver *swap.DecimalsResolver
	if rpc != nil {
		resolver = swap.NewDecimalsResolver(swap.NewRPCDecimalsSource(rpc, nil), nil)
	}
	result := []liveAdapter{
		{"odos", adapters.NewOdosAdapter()},
		{"kyber", adapters.NewKyberAdapter()},
		{"paraswap", adapters.NewParaSwapAdapter(resolver)},
		{"openocean", adapters.NewOpenOceanAdapter()},
	}
	if rpc != nil {
		result = append(result,
			liveAdapter{"uniswap", adapters.NewUniswapAdapter(rpc)},
			liveAdapter{"pancake", adapters.NewPancakeAdapter(rpc)},
		)
	} else {
		t.Log("WEB3_RPC_* URLs not set - skipping uniswap and pancake (on-chain RPC required)")
	}
	return result
}

type rpcSimulationResult struct {
	Success   bool
	GasUsed   int
	ErrorInfo string
}

func simulateRPCTrace(t *testing.T, chainID int, blockNumber int64, sender, to, data, value string) rpcSimulationResult {
	t.Helper()
	chainName := chainNameForID(chainID)
	rpcURL := chainRPCURLs[chainName]
	if rpcURL == "" {
		t.Skipf("RPC URL for chain_id=%d not set; configure the chain RPC URL with debug_traceCall support", chainID)
	}
	if value == "" {
		value = "0"
	}
	block := uint64(0)
	if blockNumber > 0 {
		block = uint64(blockNumber)
	}

	ctx, cancel := context.WithTimeout(integrationCtx(t), 30*time.Second)
	defer cancel()
	sim := security.NewRPCSimulator(map[int]string{chainID: rpcURL}, nil, nil)
	result, _, err := sim.Simulate(ctx, security.CheckRequest{
		ChainID:     chainID,
		Chain:       chainName,
		Owner:       sender,
		To:          to,
		Value:       value,
		Data:        data,
		GasLimit:    1_000_000,
		GasPrice:    "0",
		BlockNumber: block,
	})
	if err != nil {
		t.Fatalf("RPC simulation: %v", err)
	}
	if result == nil || result.Data == nil {
		return rpcSimulationResult{Success: false, ErrorInfo: "simulation result missing transaction data"}
	}
	return rpcSimulationResult{
		Success:   result.Data.Status,
		GasUsed:   int(result.Data.GasUsed),
		ErrorInfo: result.Data.ErrorInfo,
	}
}

func chainNameForID(chainID int) string {
	for _, chain := range liveChains {
		if chain.chainID == chainID {
			return chain.name
		}
	}
	return ""
}

func TestProtocols_Quote_NativeToUSDC(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	owner := liveOwner()
	for _, a := range allLiveAdapters(t) {
		a := a
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			for _, c := range liveChains {
				c := c
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					req := swap.QuoteRequest{
						Chain:         c.name,
						FromAddress:   owner,
						SourceAsset:   "eth",
						TargetAsset:   c.usdc,
						Amount:        c.amtWei,
						DisplayAmount: c.amtHuman,
						Slippage:      0.5,
					}
					result, err := a.adapter.Quote(integrationCtx(t), req)
					if err != nil {
						t.Skipf("%s/%s: quote skipped — %v", a.name, c.name, err)
						return
					}
					if result.OutputAmount == "" || result.OutputAmount == "0" {
						t.Errorf("%s/%s: expected non-zero output amount", a.name, c.name)
					}
					t.Logf("%s/%s: 0.0001 native → %s USDC",
						a.name, c.name, result.OutputAmount)
				})
			}
		})
	}
}

func TestProtocols_Simulate_NativeToUSDC(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	owner := liveOwner()
	for _, a := range allLiveAdapters(t) {
		a := a
		t.Run(a.name, func(t *testing.T) {

			for _, c := range liveChains {
				c := c
				t.Run(c.name, func(t *testing.T) {
					ctx := integrationCtx(t)
					label := a.name + "/" + c.name

					blockNumber := currentBlockNumber(c.name)

					req := swap.QuoteRequest{
						Chain:         c.name,
						FromAddress:   owner,
						SourceAsset:   "eth",
						TargetAsset:   c.usdc,
						Amount:        c.amtWei,
						DisplayAmount: c.amtHuman,
						Slippage:      0.5,
					}
					quote, err := a.adapter.Quote(ctx, req)
					if err != nil {
						t.Skipf("%s: quote skipped — %v", label, err)
						return
					}

					build, err := a.adapter.Build(ctx, swap.BuildRequest{
						Chain:       c.name,
						FromAddress: owner,
						Quote:       quote.RawQuote,
					})
					if err != nil {
						t.Fatalf("%s: build failed — %v", label, err)
					}

					swapValue := new(big.Int)
					if build.Value != "" && build.Value != "0x" && build.Value != "0x0" {
						swapValue.SetString(strings.TrimPrefix(build.Value, "0x"), 16)
					}
					allCalls := []erc7821.HexCall{{To: build.To, Value: swapValue, Data: build.Data}}
					sim := simulateERC7821(t, c.chainID, blockNumber, owner, allCalls, swapValue)
					if !sim.Success {
						t.Errorf("%s: RPC simulation failed - %s", label, sim.ErrorInfo)
					} else {
						t.Logf("%s: simulation ok - gasUsed=%d", label, sim.GasUsed)
					}
				})
			}
		})
	}
}

func TestProtocols_Simulate_USDCToNative(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	owner := liveOwner()
	for _, a := range allLiveAdapters(t) {
		a := a
		t.Run(a.name, func(t *testing.T) {

			for _, c := range liveChains {
				c := c
				t.Run(c.name, func(t *testing.T) {
					ctx := integrationCtx(t)
					label := a.name + "/" + c.name

					rpcURL, hasRPC := chainRPCURLs[c.name]
					if !hasRPC || rpcURL == "" {
						t.Skipf("%s: no RPC URL (set WEB3_RPC_*_URL)", label)
					}

					amtStr, ok := usdcAmountIn[c.name]
					if !ok {
						t.Skipf("%s: no USDC amount configured", label)
					}
					amount, _ := new(big.Int).SetString(amtStr, 10)

					bal := checkUSDCBalance(c.name, rpcURL, c.usdc, owner)
					if bal.Cmp(amount) < 0 {
						t.Skipf("%s: insufficient USDC (have %s, need %s)", label, bal, amount)
					}

					blockNumber := currentBlockNumber(c.name)

					req := swap.QuoteRequest{
						Chain:       c.name,
						FromAddress: owner,
						SourceAsset: c.usdc,
						TargetAsset: "eth",
						Amount:      amtStr,
						Slippage:    0.5,
					}
					quote, err := a.adapter.Quote(ctx, req)
					if err != nil {
						t.Skipf("%s: quote skipped — %v", label, err)
						return
					}

					build, err := a.adapter.Build(ctx, swap.BuildRequest{
						Chain:       c.name,
						FromAddress: owner,
						Quote:       quote.RawQuote,
					})
					if err != nil {
						t.Fatalf("%s: build failed — %v", label, err)
					}

					spender := quote.ApproveTarget
					if spender == "" {
						spender = build.To
					}
					current, err := swap.FetchAllowance(ctx, rpcURL, c.usdc, owner, spender)
					if err != nil {
						t.Fatalf("%s: fetch allowance: %v", label, err)
					}
					approveCalls := swap.GetApproveCalls("erc20", c.usdc, spender, build.To, amount, current)

					var allCalls []erc7821.HexCall
					for _, ac := range approveCalls {
						v := ac.Value
						if v == nil {
							v = big.NewInt(0)
						}
						allCalls = append(allCalls, erc7821.HexCall{To: ac.To, Value: v, Data: ac.Data})
					}
					swapValue := new(big.Int)
					if build.Value != "" && build.Value != "0x" && build.Value != "0x0" {
						swapValue.SetString(strings.TrimPrefix(build.Value, "0x"), 16)
					}
					allCalls = append(allCalls, erc7821.HexCall{
						To:    build.To,
						Value: swapValue,
						Data:  build.Data,
					})

					t.Logf("%s: %d approve call(s) + 1 swap call, spender=%s",
						label, len(approveCalls), spender)

					time.Sleep(300 * time.Millisecond)

					totalValue := new(big.Int)
					for _, call := range allCalls {
						if call.Value != nil {
							totalValue.Add(totalValue, call.Value)
						}
					}
					sim := simulateERC7821(t, c.chainID, blockNumber, owner, allCalls, totalValue)
					if !sim.Success {
						t.Errorf("%s: RPC simulation failed - %s", label, sim.ErrorInfo)
					} else {
						t.Logf("%s: simulation ok - gasUsed=%d", label, sim.GasUsed)
					}
				})
			}
		})
	}
}
