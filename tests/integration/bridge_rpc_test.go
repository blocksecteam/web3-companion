// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func setupBridgeRPCRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "default", liveOwner())
	resolver := swap.NewDecimalsResolver(
		swap.NewRPCDecimalsSource(chainRPCURLs, nil),
		nil,
	)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	liveCfg := liveConfig()
	bridgeReg := bridge.NewRegistry()
	bridgeadapters.RegisterAll(bridgeReg, nil)
	bridge.NewHandler(bridgeReg, bridge.WithConfig(liveCfg), bridge.WithDB(db), bridge.WithResolver(resolver)).RegisterRoutes(api)
	return r, apiKey
}

func runBridgeRPC(t *testing.T, r *gin.Engine, apiKey string, chainID int, req map[string]interface{}) {
	t.Helper()

	owner, _ := req["from_address"].(string)
	fromChain, _ := req["from_chain"].(string)
	bridgeName, _ := req["bridge"].(string)

	blockNumber := currentBlockNumber(fromChain)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/build", apiKey, req)
	if w.Code != http.StatusOK {
		t.Skipf("%s build unavailable (HTTP %d): %s", bridgeName, w.Code, w.Body.String())
	}

	var result bridge.BuildResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode build result: %v", err)
	}
	if result.To == "" || result.Data == "" {
		t.Skipf("%s build returned empty to/data (external API issue)", bridgeName)
	}
	t.Logf("%s build: to=%s value=%s", bridgeName, result.To, result.Value)

	hexValue := decimalToHex(result.Value)
	sim := simulateRPCTrace(t, chainID, blockNumber, owner, result.To, result.Data, hexValue)
	if !sim.Success {
		t.Errorf("source tx failed - %s", sim.ErrorInfo)
	} else {
		t.Logf("source tx ok - gasUsed=%d", sim.GasUsed)
	}
}

func decimalToHex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return "0x0"
	}
	n := new(big.Int)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n.SetString(s[2:], 16)
	} else {
		n.SetString(s, 10)
	}
	if n.Sign() <= 0 {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

func TestBridge_RPC_Bungee_Native_ETHtoBase(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	r, apiKey := setupBridgeRPCRouter(t)
	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "bungee", "from_chain": "ethereum", "to_chain": "base",
		"from_token": "native", "to_token": "native",
		"amount": "0.001", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func TestBridge_RPC_Bungee_USDC_ETHtoPolygon(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	requireUSDCBalance(t, "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", 1_000_000)
	r, apiKey := setupBridgeRPCRouter(t)
	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "bungee", "from_chain": "ethereum", "to_chain": "polygon",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to_token":   "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359",
		"amount":     "1", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func TestBridge_RPC_Relay_Native_ETHtoBSC(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	r, apiKey := setupBridgeRPCRouter(t)

	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "relay", "from_chain": "ethereum", "to_chain": "bsc",
		"from_token": "native", "to_token": "native",
		"amount": "0.001", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func TestBridge_RPC_Relay_Native_ETHtoPolygon(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	r, apiKey := setupBridgeRPCRouter(t)

	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "relay", "from_chain": "ethereum", "to_chain": "polygon",
		"from_token": "native", "to_token": "native",
		"amount": "0.001", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func TestBridge_RPC_Relay_USDC_ETHtoBase(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	requireUSDCBalance(t, "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", 1_000_000)
	r, apiKey := setupBridgeRPCRouter(t)
	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "relay", "from_chain": "ethereum", "to_chain": "base",
		"from_token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"to_token":   "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"amount":     "1", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func TestBridge_RPC_Relay_NativeToUSDC_ETHtoBase(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	r, apiKey := setupBridgeRPCRouter(t)
	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "relay", "from_chain": "ethereum", "to_chain": "base",
		"from_token": "native",
		"to_token":   "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"amount":     "0.001", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func TestBridge_RPC_Bungee_NativeToUSDC_ETHtoBase(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	r, apiKey := setupBridgeRPCRouter(t)
	runBridgeRPC(t, r, apiKey, 1, map[string]interface{}{
		"wallet_id": "default", "bridge": "bungee", "from_chain": "ethereum", "to_chain": "base",
		"from_token": "native",
		"to_token":   "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		"amount":     "0.001", "from_address": liveOwner(), "slippage": 1.0,
	})
}

func requireUSDCBalance(t *testing.T, chain, token string, minRaw int64) {
	t.Helper()
	rpcURL, ok := chainRPCURLs[chain]
	if !ok || rpcURL == "" {
		t.Skip("RPC URL not set")
	}
	bal := checkUSDCBalance(chain, rpcURL, token, liveOwner())
	if bal.Cmp(big.NewInt(minRaw)) < 0 {
		t.Skipf("insufficient balance on %s: have %s, need %d", chain, bal, minRaw)
	}
}
