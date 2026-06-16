// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/server"
	"github.com/blocksecteam/web3-companion/internal/settings"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestServerHealthUsesInjectedBuildInfo(t *testing.T) {
	server.SetBuildInfo("test-build")
	t.Cleanup(func() { server.SetBuildInfo("") })

	r := gin.New()
	server.RegisterHealthRoute(r)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /health got %d: %s", w.Code, w.Body.String())
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got["status"] != "ok" || got["version"] != "test-build" {
		t.Fatalf("health response = %#v, want status ok and injected version", got)
	}
}

func TestServerVerifyConfiguredRPCsRejectsWrongChainIDWithoutLeakingURL(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": "0x2105"})
	}))
	defer rpc.Close()
	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{
		{Name: "bsc", ChainID: 56, NativeSymbol: "BNB", RPCURL: rpc.URL + "/secret-path"},
	}

	err := server.VerifyConfiguredRPCs(t.Context(), cfg)
	if err == nil {
		t.Fatal("VerifyConfiguredRPCs error = nil, want wrong-chain error")
	}
	if !strings.Contains(err.Error(), "bsc") || !strings.Contains(err.Error(), "56") {
		t.Fatalf("VerifyConfiguredRPCs error = %v, want chain name and expected id", err)
	}
	if strings.Contains(err.Error(), rpc.URL) || strings.Contains(err.Error(), "secret-path") {
		t.Fatalf("VerifyConfiguredRPCs leaked RPC URL: %v", err)
	}
}

func TestServerCORSAllowsConfiguredFrontendOrigin(t *testing.T) {
	cfg := testutil.NewTestConfig()
	cfg.CORS.AllowedOrigins = []string{"http://localhost:5173"}

	r := gin.New()
	r.Use(server.BrowserCORSForTest(cfg))
	server.RegisterHealthRoute(r)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/user/settings/llm", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS with configured origin = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q", got)
	}
}

func TestServerRoutesSupportedChainsAndProtocolsArePublic(t *testing.T) {
	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH"},
	}
	db := testutil.NewTestDB(t)
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)

	r := gin.New()
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{}))

	for _, path := range []string{
		"/api/v1/chain/supported",
		"/api/v1/swap/protocols",
		"/api/v1/bridge/protocols",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s should be public, got %d: %s", path, w.Code, w.Body.String())
		}
	}
}

func TestServerRoutesAuthenticatedChainGasRequiresJWT(t *testing.T) {
	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH"},
	}
	db := testutil.NewTestDB(t)
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)

	r := gin.New()
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	server.RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), tokenService, cfg.Auth.JWTSecret, llmService, nil)
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), llmService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chain/gas/ethereum", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/chain/gas/ethereum without JWT got %d, want 401", w.Code)
	}
}

func TestServerRoutesBrowserScopeCannotAccessCompanionWriteAPIs(t *testing.T) {
	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH"},
	}
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)
	browserToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientBrowser,
		Scope:      auth.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue browser token: %v", err)
	}

	r := gin.New()
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	server.RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), tokenService, cfg.Auth.JWTSecret, llmService, nil)
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), llmService)

	for _, tt := range []struct {
		method string
		path   string
		body   map[string]interface{}
	}{
		{method: http.MethodPost, path: "/api/v1/ssm/sign", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/ssm/delegate", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/chain/broadcast", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/swap/build", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/bridge/build", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/security/check", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/security/clear-review", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/security/override", body: map[string]interface{}{}},
	} {
		w := testutil.DoRequest(r, tt.method, tt.path, tt.body, map[string]string{
			"Authorization": "Bearer " + browserToken.AccessToken,
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s with browser token = %d, want 403: %s", tt.method, tt.path, w.Code, w.Body.String())
		}
	}

}

func TestServerRoutesBrowserScopeCanReadAndCancelPasskeyChallenge(t *testing.T) {
	cfg := testutil.NewTestConfig()
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)
	browserToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientBrowser,
		Scope:      auth.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue browser token: %v", err)
	}
	if err := db.Create(&storage.Challenge{
		ChallengeID: "ch_browser_modal",
		UserID:      userID,
		Type:        "delete_wallet",
		Status:      "pending",
		PayloadJSON: `{"challenge":"seed","payload":{"operation":"delete_wallet","wallet_id":"browser_wallet"}}`,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	r := gin.New()
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	server.RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), tokenService, cfg.Auth.JWTSecret, llmService, nil)
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), llmService)

	w := testutil.DoRequest(r, http.MethodGet, "/api/v1/ssm/webauthn/challenge/ch_browser_modal", nil, map[string]string{
		"Authorization": "Bearer " + browserToken.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("browser challenge status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var status struct {
		Status  string `json:"status"`
		Summary struct {
			WalletID string `json:"wallet_id"`
		} `json:"summary"`
	}
	testutil.ParseJSON(t, w, &status)
	if status.Status != "pending" || status.Summary.WalletID != "browser_wallet" {
		t.Fatalf("unexpected browser challenge status: %+v", status)
	}

	w = testutil.DoRequest(r, http.MethodPost, "/api/v1/ssm/webauthn/challenge/ch_browser_modal/cancel", nil, map[string]string{
		"Authorization": "Bearer " + browserToken.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("browser challenge cancel = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestServerRoutesContainerScopeCanReachCompanionWriteAPIs(t *testing.T) {
	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH"},
	}
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)
	containerToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientContainer,
		Scope:      auth.ScopeCompanion,
	})
	if err != nil {
		t.Fatalf("issue container token: %v", err)
	}

	r := gin.New()
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	server.RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), tokenService, cfg.Auth.JWTSecret, llmService, nil)
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), llmService)

	for _, tt := range []struct {
		method string
		path   string
		body   map[string]interface{}
	}{
		{method: http.MethodPost, path: "/api/v1/ssm/sign", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/ssm/delegate", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/chain/broadcast", body: map[string]interface{}{}},
		{method: http.MethodPost, path: "/api/v1/swap/build", body: map[string]interface{}{}},
	} {
		w := testutil.DoRequest(r, tt.method, tt.path, tt.body, map[string]string{
			"Authorization": "Bearer " + containerToken.AccessToken,
		})
		if w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized || w.Code == http.StatusNotFound {
			t.Fatalf("%s %s with container token got auth/routing failure %d: %s", tt.method, tt.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "invalid_request") &&
			!strings.Contains(w.Body.String(), "unsupported_chain") &&
			!strings.Contains(w.Body.String(), "wallet_not_found") {
			t.Fatalf("%s %s should reach handler validation, got %d: %s", tt.method, tt.path, w.Code, w.Body.String())
		}
	}
}

func TestServerRoutesCompanionScopeCanReachUserContext(t *testing.T) {
	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{
		{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH", RPCURL: "https://eth.example.test"},
		{Name: "base", ChainID: 8453, NativeSymbol: "ETH", RPCURL: "https://base.example.test"},
		{Name: "polygon", ChainID: 137, NativeSymbol: "POL", RPCURL: "https://polygon.example.test"},
		{Name: "bsc", ChainID: 56, NativeSymbol: "BNB", RPCURL: "https://bsc.example.test"},
	}
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateUndelegatedTestWallet(t, db, userID, "main", "0x34239406901B30733212f418AFfbE3c38a8cFb6A")
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)
	containerToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientContainer,
		Scope:      auth.ScopeCompanion,
	})
	if err != nil {
		t.Fatalf("issue container token: %v", err)
	}

	r := gin.New()
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	server.RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), tokenService, cfg.Auth.JWTSecret, llmService, nil)
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), llmService)

	w := testutil.DoRequest(r, http.MethodGet, "/api/v1/users/context", nil, map[string]string{
		"Authorization": "Bearer " + containerToken.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/users/context = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Auth      string `json:"auth"`
		Readiness string `json:"readiness"`
		Wallets   []struct {
			WalletID string `json:"wallet_id"`
			Chains   []struct {
				Chain            string `json:"chain"`
				DelegationStatus string `json:"delegation_status"`
			} `json:"chains"`
		} `json:"wallets"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Auth != "container" || resp.Readiness != "ready" || len(resp.Wallets) != 1 {
		t.Fatalf("unexpected user context: %+v", resp)
	}
	if resp.Wallets[0].WalletID != "main" {
		t.Fatalf("unexpected wallet status: %+v", resp.Wallets[0])
	}
	wantChains := []string{"ethereum", "base", "polygon", "bsc"}
	if len(resp.Wallets[0].Chains) != len(wantChains) {
		t.Fatalf("wallet chains = %+v, want %v not_delegated chains", resp.Wallets[0].Chains, wantChains)
	}
	for i, wantChain := range wantChains {
		got := resp.Wallets[0].Chains[i]
		if got.Chain != wantChain || got.DelegationStatus != "not_delegated" {
			t.Fatalf("chain[%d] = %+v, want %s not_delegated", i, got, wantChain)
		}
	}
}

func TestServerRoutesCompanionScopeCannotBeginPasskeyRegistration(t *testing.T) {
	cfg := testutil.NewTestConfig()
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)
	containerToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientContainer,
		Scope:      auth.ScopeCompanion,
	})
	if err != nil {
		t.Fatalf("issue container token: %v", err)
	}

	r := gin.New()
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	server.RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), tokenService, cfg.Auth.JWTSecret, llmService, nil)
	server.RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), llmService)

	for _, tt := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/ssm/webauthn/register"},
		{method: http.MethodPost, path: "/api/v1/ssm/webauthn/verify"},
	} {
		w := testutil.DoRequest(r, tt.method, tt.path, map[string]string{}, map[string]string{
			"Authorization": "Bearer " + containerToken.AccessToken,
		})
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404: %s", tt.method, tt.path, w.Code, w.Body.String())
		}
	}
}
