// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/setup"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{ExternalURL: "http://localhost:8081"},
		DB:     config.DBConfig{Driver: "sqlite", DSN: ":memory:"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPName: "test", RPOrigin: "http://localhost:8081",
		},
		Auth:      config.AuthConfig{JWTSecret: "test-jwt-secret-long-enough"},
		Workdir:   config.WorkdirConfig{Path: "/tmp/web3-test"},
		SetupMode: true,
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, NativeSymbol: "ETH"},
			{Name: "base", ChainID: 8453, NativeSymbol: "ETH"},
			{Name: "polygon", ChainID: 137, NativeSymbol: "POL"},
			{Name: "bsc", ChainID: 56, NativeSymbol: "BNB"},
		},
	}
}

func setupTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	setup.ResetForTest()
	db := testutil.NewTestDB(t)
	cfg := testConfig()
	secrets := setup.GeneratedSecrets{JWTSecret: "test-jwt", SSMKey: "test-ssm"}

	r := gin.New()
	setup.RegisterRoutes(r, cfg, db, secrets)
	return r, db
}

func localRequest(r http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func parseBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse response: %v\nbody: %s", err, w.Body.String())
	}
	return out
}

func TestStatus_ReturnsSetupMode(t *testing.T) {
	r, _ := setupTestRouter(t)

	w := localRequest(r, "GET", "/setup/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	body := parseBody(t, w)
	if body["setup_mode"] != true {
		t.Errorf("setup_mode: got %v, want true", body["setup_mode"])
	}
	if body["complete"] != false {
		t.Errorf("complete: got %v, want false", body["complete"])
	}

	chains, ok := body["chains"].([]interface{})
	if !ok || len(chains) != 4 {
		t.Fatalf("chains: expected 4, got %v", body["chains"])
	}
}

func TestStatus_AccessibleFromRemote(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/setup/status", nil)
	req.RemoteAddr = "203.0.113.50:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/setup/status should be accessible from remote, got %d", w.Code)
	}
}

func TestGetSecrets_ReturnsGenerated(t *testing.T) {
	r, _ := setupTestRouter(t)

	w := localRequest(r, "GET", "/setup/secrets", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	body := parseBody(t, w)
	if body["jwt_secret"] != "test-jwt" {
		t.Errorf("jwt_secret: got %v, want test-jwt", body["jwt_secret"])
	}
	if body["ssm_key"] != "test-ssm" {
		t.Errorf("ssm_key: got %v, want test-ssm", body["ssm_key"])
	}
}

func TestGetSecrets_BlockedFromRemote(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/setup/secrets", nil)
	req.RemoteAddr = "203.0.113.50:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestSaveLLM_WritesSettings(t *testing.T) {
	r, db := setupTestRouter(t)

	body := map[string]string{
		"provider":   "deepseek",
		"base_url":   "https://api.deepseek.com/anthropic",
		"model_name": "deepseek-v4-pro",
		"api_key":    "sk-test-key",
	}
	w := localRequest(r, "POST", "/setup/llm", body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	resp := parseBody(t, w)
	if resp["api_key_configured"] != true {
		t.Error("expected api_key_configured=true")
	}

	svc := testutil.NewSettingsService(db)
	llm, _ := svc.ReadLLM(testutil.DefaultLLM())
	if llm.APIKey != "sk-test-key" {
		t.Errorf("expected API key in DB, got %q", llm.APIKey)
	}
}

func TestSaveRPC_WritesSettings(t *testing.T) {
	r, db := setupTestRouter(t)

	body := map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]string{"url": "https://eth.example.com"},
		},
	}
	w := localRequest(r, "POST", "/setup/rpc", body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	svc := testutil.NewSettingsService(db)
	rpc, _ := svc.ReadRPC()
	if rpc.Chains["ethereum"].URL != "https://eth.example.com" {
		t.Errorf("expected ethereum URL in DB, got %v", rpc.Chains)
	}
}

func TestSaveRPC_RejectsUnsupportedChain(t *testing.T) {
	r, _ := setupTestRouter(t)

	body := map[string]interface{}{
		"chains": map[string]interface{}{
			"solana": map[string]string{"url": "https://sol.example.com"},
		},
	}
	w := localRequest(r, "POST", "/setup/rpc", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestComplete_RequiresRPC(t *testing.T) {
	r, _ := setupTestRouter(t)

	w := localRequest(r, "POST", "/setup/complete", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestComplete_Success(t *testing.T) {
	r, _ := setupTestRouter(t)

	rpcBody := map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]string{"url": "https://eth.example.com"},
		},
	}
	localRequest(r, "POST", "/setup/rpc", rpcBody)

	w := localRequest(r, "POST", "/setup/complete", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	if !setup.IsRuntimeComplete() {
		t.Error("expected runtime complete flag to be true")
	}
}

func TestComplete_BlocksSecondCall(t *testing.T) {
	r, _ := setupTestRouter(t)

	rpcBody := map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]string{"url": "https://eth.example.com"},
		},
	}
	localRequest(r, "POST", "/setup/rpc", rpcBody)
	localRequest(r, "POST", "/setup/complete", nil)

	w2 := localRequest(r, "POST", "/setup/complete", nil)
	if w2.Code != http.StatusOK {
		t.Errorf("second complete: got %d, want 200 (idempotent)", w2.Code)
	}
}

func TestSaveRPC_InvalidJSON(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/setup/rpc", strings.NewReader("not json"))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSaveRPC_MultipleChains(t *testing.T) {
	r, db := setupTestRouter(t)

	body := map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]string{"url": "https://eth.example.com"},
			"base":     map[string]string{"url": "https://base.example.com"},
		},
	}
	w := localRequest(r, "POST", "/setup/rpc", body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	svc := testutil.NewSettingsService(db)
	rpc, _ := svc.ReadRPC()
	if len(rpc.Chains) != 2 {
		t.Errorf("expected 2 chains, got %d", len(rpc.Chains))
	}
}

func TestSaveLLM_InvalidJSON(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/setup/llm", strings.NewReader("not json"))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSaveLLM_ResponseFields(t *testing.T) {
	r, _ := setupTestRouter(t)

	body := map[string]string{
		"provider":   "custom",
		"base_url":   "https://custom.api.com/v1",
		"model_name": "custom-model",
		"api_key":    "sk-custom-key-12345678",
	}
	w := localRequest(r, "POST", "/setup/llm", body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	resp := parseBody(t, w)
	if resp["provider"] != "custom" {
		t.Errorf("provider: got %v, want custom", resp["provider"])
	}
	if resp["api_key_hint"] == nil || resp["api_key_hint"] == "" {
		t.Error("expected api_key_hint to be set")
	}
}

func TestVerifyRPC_InvalidJSON(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/setup/rpc/verify", strings.NewReader("not json"))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestVerifyRPC_UnsupportedChain(t *testing.T) {
	r, _ := setupTestRouter(t)

	body := map[string]interface{}{
		"chains": map[string]interface{}{
			"solana": map[string]string{"url": "https://sol.example.com"},
		},
	}
	w := localRequest(r, "POST", "/setup/rpc/verify", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestVerifyRPC_BadURL(t *testing.T) {
	r, _ := setupTestRouter(t)

	body := map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]string{"url": "http://127.0.0.1:1/nonexistent"},
		},
	}
	w := localRequest(r, "POST", "/setup/rpc/verify", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400 (SSRF blocked): %s", w.Code, w.Body.String())
	}
}

func TestVerifyLLM_InvalidJSON(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/setup/llm/verify", strings.NewReader("not json"))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestVerifyLLM_EmptyApiKey(t *testing.T) {
	r, _ := setupTestRouter(t)

	body := map[string]string{
		"provider":   "deepseek",
		"base_url":   "https://api.deepseek.com/anthropic",
		"model_name": "deepseek-v4-pro",
		"api_key":    "",
	}
	w := localRequest(r, "POST", "/setup/llm/verify", body)
	if w.Code != http.StatusBadGateway {
		t.Errorf("got %d, want 502: %s", w.Code, w.Body.String())
	}
}

func TestSaveRPC_AppliesConfigChains(t *testing.T) {
	setup.ResetForTest()
	db := testutil.NewTestDB(t)
	cfg := testConfig()
	secrets := setup.GeneratedSecrets{JWTSecret: "test-jwt", SSMKey: "test-ssm"}
	r := gin.New()
	setup.RegisterRoutes(r, cfg, db, secrets)

	body := map[string]interface{}{
		"chains": map[string]interface{}{
			"ethereum": map[string]string{"url": "https://eth-applied.example.com"},
		},
	}
	localRequest(r, "POST", "/setup/rpc", body)

	for _, ch := range cfg.Chains {
		if ch.Name == "ethereum" {
			if ch.RPCURL != "https://eth-applied.example.com" {
				t.Errorf("expected config chain RPCURL to be applied, got %q", ch.RPCURL)
			}
			return
		}
	}
	t.Error("ethereum chain not found in config")
}

func TestStatus_ReturnsCompleteWhenUserExists(t *testing.T) {
	setup.ResetForTest()
	db := testutil.NewTestDB(t)
	cfg := testConfig()
	secrets := setup.GeneratedSecrets{JWTSecret: "test-jwt", SSMKey: "test-ssm"}

	testutil.CreateTestUser(t, db)

	r := gin.New()
	setup.RegisterRoutes(r, cfg, db, secrets)

	w := localRequest(r, "GET", "/setup/status", nil)
	body := parseBody(t, w)
	if body["complete"] != true {
		t.Errorf("complete: got %v, want true (user exists)", body["complete"])
	}
}
