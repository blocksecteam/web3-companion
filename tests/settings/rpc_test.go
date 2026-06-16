// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package settings_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/settings"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

type settingsUser struct {
	DB     *gorm.DB
	UserID string
}

func TestRPCSettingsSaveWritesToDBAndOverlaysConfig(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	service := settings.NewService(user.DB)
	r := settingsRouter(t, cfg, service, user)
	ethereumRPC := chainIDRPC(t, "0x1")
	baseRPC := chainIDRPC(t, "0x2105")

	w := testutil.DoRequest(r, http.MethodPut, "/api/v1/user/settings/rpc", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": ethereumRPC.URL + "/eth?key=secret"},
			"base":     map[string]interface{}{"url": baseRPC.URL + "/base"},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("save rpc = %d: %s", w.Code, w.Body.String())
	}
	if cfg.ChainByName("ethereum").RPCURL != ethereumRPC.URL+"/eth?key=secret" {
		t.Fatalf("config ethereum RPC = %q", cfg.ChainByName("ethereum").RPCURL)
	}

	stored, err := service.ReadRPC()
	if err != nil {
		t.Fatalf("read stored rpc: %v", err)
	}
	if stored.Chains["ethereum"].URL != ethereumRPC.URL+"/eth?key=secret" {
		t.Fatalf("stored ethereum RPC = %q", stored.Chains["ethereum"].URL)
	}

	var resp struct {
		Chains []struct {
			Name       string `json:"name"`
			RPCURLHint string `json:"rpc_url_hint"`
		} `json:"chains"`
	}
	testutil.ParseJSON(t, w, &resp)
	for _, chain := range resp.Chains {
		if chain.Name == "ethereum" && chain.RPCURLHint == ethereumRPC.URL+"/eth?key=secret" {
			t.Fatalf("response leaked full RPC URL: %+v", resp)
		}
	}
}

func TestRPCSettingsSaveMergesWithExistingSettings(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	service := settings.NewService(user.DB)
	r := settingsRouter(t, cfg, service, user)
	ethereumRPC := chainIDRPC(t, "0x1")
	baseRPC := chainIDRPC(t, "0x2105")
	nextEthereumRPC := chainIDRPC(t, "0x1")

	w := testutil.DoRequest(r, http.MethodPut, "/api/v1/user/settings/rpc", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": ethereumRPC.URL + "/eth"},
			"base":     map[string]interface{}{"url": baseRPC.URL + "/base"},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("initial save rpc = %d: %s", w.Code, w.Body.String())
	}

	w = testutil.DoRequest(r, http.MethodPut, "/api/v1/user/settings/rpc", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": nextEthereumRPC.URL + "/eth-next"},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("partial save rpc = %d: %s", w.Code, w.Body.String())
	}

	stored, err := service.ReadRPC()
	if err != nil {
		t.Fatalf("read stored rpc: %v", err)
	}
	if stored.Chains["ethereum"].URL != nextEthereumRPC.URL+"/eth-next" {
		t.Fatalf("stored ethereum RPC = %q", stored.Chains["ethereum"].URL)
	}
	if stored.Chains["base"].URL != baseRPC.URL+"/base" {
		t.Fatalf("stored base RPC was not preserved: %+v", stored.Chains)
	}
}

func TestRuntimeRPCApplyUpdatesSnapshotsAndConfig(t *testing.T) {
	cfg := testConfig(t)
	db := testutil.NewTestDB(t)
	service := settings.NewService(db)
	runtime, err := settings.NewRuntimeRPC(cfg, service)
	if err != nil {
		t.Fatalf("new runtime rpc: %v", err)
	}
	initialByName := runtime.ChainRPCMap()
	if initialByName["ethereum"] != "https://initial.example/eth" {
		t.Fatalf("initial ethereum rpc = %q", initialByName["ethereum"])
	}
	initialByID := runtime.ChainIDRPCMap()
	if initialByID[8453] != "https://initial.example/base" {
		t.Fatalf("initial base rpc by chain id = %q", initialByID[8453])
	}

	next := settings.RPCSettings{Chains: map[string]settings.RPCChainSettings{
		"ethereum": {URL: "https://runtime.example/eth"},
		"base":     {URL: "https://runtime.example/base"},
	}}
	runtime.Apply(next)

	byName := runtime.ChainRPCMap()
	if byName["ethereum"] != "https://runtime.example/eth" {
		t.Fatalf("runtime ethereum rpc = %q", byName["ethereum"])
	}
	byName["ethereum"] = "https://mutated.example"
	if runtime.ChainRPCMap()["ethereum"] != "https://runtime.example/eth" {
		t.Fatal("runtime ChainRPCMap returned mutable internal state")
	}
	byID := runtime.ChainIDRPCMap()
	if byID[1] != "https://runtime.example/eth" || byID[8453] != "https://runtime.example/base" {
		t.Fatalf("runtime chain id map = %+v", byID)
	}
	if cfg.ChainByName("base").RPCURL != "https://runtime.example/base" {
		t.Fatalf("config base RPC = %q", cfg.ChainByName("base").RPCURL)
	}
}

func TestRPCSettingsSaveAppliesRuntimeProvider(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	service := settings.NewService(user.DB)
	runtime, err := settings.NewRuntimeRPC(cfg, service)
	if err != nil {
		t.Fatalf("new runtime rpc: %v", err)
	}
	r := settingsRouterWithOptions(t, cfg, service, user, settings.WithRPCSettingsApplier(runtime))
	ethereumRPC := chainIDRPC(t, "0x1")

	w := testutil.DoRequest(r, http.MethodPut, "/api/v1/user/settings/rpc", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": ethereumRPC.URL + "/eth"},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("save rpc = %d: %s", w.Code, w.Body.String())
	}
	if runtime.ChainRPCMap()["ethereum"] != ethereumRPC.URL+"/eth" {
		t.Fatalf("runtime ethereum RPC was not updated: %+v", runtime.ChainRPCMap())
	}
	if runtime.ChainIDRPCMap()[1] != ethereumRPC.URL+"/eth" {
		t.Fatalf("runtime ethereum RPC by chain id was not updated: %+v", runtime.ChainIDRPCMap())
	}
}

func TestRPCSettingsSaveRejectsWrongChainID(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	rpc := chainIDRPC(t, "0x2105")
	r := settingsRouter(t, cfg, settings.NewService(user.DB), user)

	w := testutil.DoRequest(r, http.MethodPut, "/api/v1/user/settings/rpc", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": rpc.URL},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("save rpc wrong chain = %d, want 502: %s", w.Code, w.Body.String())
	}
	if cfg.ChainByName("ethereum").RPCURL == rpc.URL {
		t.Fatal("wrong-chain RPC URL was applied to runtime config")
	}
}

func TestRPCSettingsRejectsUnsupportedChainAndInvalidURL(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	r := settingsRouter(t, cfg, settings.NewService(user.DB), user)

	for _, body := range []map[string]interface{}{
		{"chains": map[string]interface{}{"arbitrum": map[string]interface{}{"url": "https://rpc.example"}}},
		{"chains": map[string]interface{}{"ethereum": map[string]interface{}{"url": "file:///tmp/rpc"}}},
	} {
		w := testutil.DoRequest(r, http.MethodPut, "/api/v1/user/settings/rpc", body, map[string]string{"Authorization": "Bearer " + token})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("save rpc invalid body = %d, want 400: %s", w.Code, w.Body.String())
		}
	}
}

func TestRPCSettingsVerifyCallsEthChainIDAndStoresVerifiedAt(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	service := settings.NewService(user.DB)
	rpc := chainIDRPC(t, "0x1")
	r := settingsRouter(t, cfg, service, user)

	w := testutil.DoRequest(r, http.MethodPost, "/api/v1/user/settings/rpc/verify", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": rpc.URL},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("verify rpc = %d: %s", w.Code, w.Body.String())
	}
	stored, err := service.ReadRPC()
	if err != nil {
		t.Fatalf("read stored rpc: %v", err)
	}
	if stored.Chains["ethereum"].VerifiedAt == nil {
		t.Fatalf("verified_at was not stored: %+v", stored)
	}
	if cfg.ChainByName("ethereum").RPCURL != rpc.URL {
		t.Fatalf("config ethereum RPC = %q, want verified URL", cfg.ChainByName("ethereum").RPCURL)
	}
}

func TestRPCSettingsVerifyEmptyBodyUsesCurrentSettings(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	service := settings.NewService(user.DB)
	ethereumRPC := chainIDRPC(t, "0x1")
	baseRPC := chainIDRPC(t, "0x2105")
	if err := service.WriteRPC(settings.RPCSettings{Chains: map[string]settings.RPCChainSettings{
		"ethereum": {URL: ethereumRPC.URL + "/eth"},
		"base":     {URL: baseRPC.URL + "/base"},
	}}); err != nil {
		t.Fatalf("write rpc settings: %v", err)
	}
	r := settingsRouter(t, cfg, service, user)

	w := testutil.DoRequest(r, http.MethodPost, "/api/v1/user/settings/rpc/verify", map[string]interface{}{
		"chains": map[string]interface{}{},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("verify current rpc = %d: %s", w.Code, w.Body.String())
	}
	stored, err := service.ReadRPC()
	if err != nil {
		t.Fatalf("read stored rpc: %v", err)
	}
	if stored.Chains["ethereum"].URL != ethereumRPC.URL+"/eth" || stored.Chains["base"].URL != baseRPC.URL+"/base" {
		t.Fatalf("verify with empty body rewrote current RPC settings: %+v", stored.Chains)
	}
	if stored.Chains["ethereum"].VerifiedAt == nil || stored.Chains["base"].VerifiedAt == nil {
		t.Fatalf("verify current settings did not mark chains verified: %+v", stored.Chains)
	}
}

func TestRPCSettingsVerifyRejectsWrongChainID(t *testing.T) {
	cfg := testConfig(t)
	user, token := setupSettingsUser(t, cfg)
	rpc := chainIDRPC(t, "0x2105")
	r := settingsRouter(t, cfg, settings.NewService(user.DB), user)

	w := testutil.DoRequest(r, http.MethodPost, "/api/v1/user/settings/rpc/verify", map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]interface{}{"url": rpc.URL},
		},
	}, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("verify rpc wrong chain = %d, want 502: %s", w.Code, w.Body.String())
	}
}

func chainIDRPC(t *testing.T, result string) *httptest.Server {
	t.Helper()
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		if body["method"] != "eth_chainId" {
			t.Fatalf("rpc method = %#v, want eth_chainId", body["method"])
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": body["id"], "result": result})
	}))
	t.Cleanup(rpc.Close)
	return rpc
}

func setupSettingsUser(t *testing.T, cfg *config.Config) (*settingsUser, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	tokenPair, err := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret).Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientBrowser,
		Scope:      auth.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue settings token: %v", err)
	}
	return &settingsUser{DB: db, UserID: userID}, tokenPair.AccessToken
}

func settingsRouter(t *testing.T, cfg *config.Config, service *settings.Service, user *settingsUser) *gin.Engine {
	t.Helper()
	return settingsRouterWithOptions(t, cfg, service, user)
}

func settingsRouterWithOptions(t *testing.T, cfg *config.Config, service *settings.Service, user *settingsUser, opts ...settings.RouteOption) *gin.Engine {
	t.Helper()
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.Middleware(user.DB, auth.WithJWTSecretString(cfg.Auth.JWTSecret)))
	browser := api.Group("")
	browser.Use(auth.RequireClient(auth.ClientBrowser), auth.RequireScope(auth.ScopeBrowser))
	settings.RegisterRoutes(browser, cfg, service, nil, nil, opts...)
	return r
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := testutil.NewTestConfig()
	cfg.Workdir.Path = t.TempDir()
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH", RPCURL: "https://initial.example/eth"},
		{Name: "base", ChainID: 8453, NativeSymbol: "ETH", RPCURL: "https://initial.example/base"},
	}
	return cfg
}
