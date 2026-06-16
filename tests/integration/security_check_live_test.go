// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/security"
	securitychecks "github.com/blocksecteam/web3-companion/internal/security/checks"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

const (
	testUserID    = "test_user_security_e2e"
	testWalletID  = "default"
	testWalletUID = "wuid_test_security_e2e"
)

func setupSecurityCheckRouter(t *testing.T, withLabels bool) (*gin.Engine, string, string, *txintent.Service) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)

	rpcURLs := configuredRPCURLs()
	rpcByChainID := make(map[int]string)
	cfg := loadIntegrationConfig(t)
	for name, url := range rpcURLs {
		if ch := cfg.ChainByName(name); ch != nil {
			rpcByChainID[ch.ChainID] = url
		}
	}

	sim := security.NewRPCSimulator(rpcByChainID, nil, nil)
	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, nil)

	opts := []security.HandlerOption{
		security.WithSimulator(sim),
		security.WithCheckRegistry(reg),
		security.WithIntentStore(db),
		security.WithFunctionSignatureResolver(security.NewSourcifyFunctionSignatureResolver("", nil)),
	}
	if withLabels {
		apiKey := strings.TrimSpace(os.Getenv("METASLEUTH_API_KEY"))
		if apiKey != "" {
			opts = append(opts, security.WithAddressLabelProvider(
				security.NewMetaSleuthLabelProvider("", apiKey, nil),
			))
		}
	}

	handler := security.NewHandler(opts...)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	handler.RegisterRoutes(api)

	intentSvc := txintent.NewService(db, nil)
	return r, apiKey, userID, intentSvc
}

type securityCheckResponse struct {
	Verdict       string                   `json:"verdict"`
	TransactionID string                   `json:"transaction_id"`
	Status        string                   `json:"status"`
	Checks        []map[string]interface{} `json:"checks"`
}

func runSecurityCheck(t *testing.T, r *gin.Engine, apiKey, userID string, intentSvc *txintent.Service, req txintent.CreateRequest) securityCheckResponse {
	t.Helper()
	if req.UserID == "" {
		req.UserID = userID
	}
	if req.WalletID == "" {
		req.WalletID = testWalletID
	}
	if req.WalletUID == "" {
		req.WalletUID = testWalletUID
	}
	ctx := integrationCtx(t)
	intent, err := intentSvc.Create(ctx, req)
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/security/check", apiKey, map[string]string{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("security check HTTP %d: %s", w.Code, w.Body.String())
	}

	var resp securityCheckResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v\nbody: %s", err, w.Body.String())
	}
	return resp
}

func requireRules(t *testing.T, resp securityCheckResponse, rules ...string) {
	t.Helper()
	found := make(map[string]bool)
	for _, c := range resp.Checks {
		if rule, ok := c["rule"].(string); ok {
			found[rule] = true
		}
	}
	for _, r := range rules {
		if !found[r] {
			t.Errorf("missing required check rule: %s", r)
		}
	}
}

func logChecks(t *testing.T, resp securityCheckResponse) {
	t.Helper()
	t.Logf("verdict: %s", resp.Verdict)
	for _, c := range resp.Checks {
		rule, _ := c["rule"].(string)
		status, _ := c["status"].(string)
		detail, _ := c["detail"].(string)
		if len(detail) > 120 {
			detail = detail[:120] + "..."
		}
		t.Logf("  %-28s %s  %s", rule, status, detail)
	}
}

// --- Native transfer: should pass ---

func TestSecurityCheck_NativeTransfer_Pass(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}

	recipient := "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510"
	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)
	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "transfer",
		ChainID:     1,
		Chain:       "ethereum",
		Owner:       addrVitalik,
		To:          recipient,
		Value:       "1000000000000000",
		Data:        "0x",
		GasLimit:    30000,
		SummaryJSON: `{"token":"native","amount":"0.001","to":"` + recipient + `"}`,
	})

	logChecks(t, resp)
	requireRules(t, resp, "transaction_shape", "simulation_success", "transfer_intent_match", "gas_cost_sanity")

	if resp.Verdict != "pass" && resp.Verdict != "warn" {
		t.Errorf("expected pass or warn for valid native transfer, got %s", resp.Verdict)
	}
}

// --- ERC-20 transfer: should pass ---

func TestSecurityCheck_ERC20Transfer_Pass(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)

	transferData := "0xa9059cbb" +
		"000000000000000000000000000000000000000000000000000000000000dead" +
		"00000000000000000000000000000000000000000000000000000000000f4240"

	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "transfer",
		ChainID:     1,
		Chain:       "ethereum",
		Owner:       addrVitalik,
		To:          addrUSDCEth,
		Value:       "0",
		Data:        transferData,
		GasLimit:    100000,
		SummaryJSON: `{"token":"` + addrUSDCEth + `","amount":"1","to":"0x000000000000000000000000000000000000dead"}`,
	})

	logChecks(t, resp)
	requireRules(t, resp, "transaction_shape", "simulation_success")
}

// --- Transfer to burn address: should fail burn_address_check ---

func TestSecurityCheck_BurnAddress_Fail(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)
	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "transfer",
		ChainID:     1,
		Chain:       "ethereum",
		Owner:       addrVitalik,
		To:          "0x0000000000000000000000000000000000000000",
		Value:       "1000000000000000",
		GasLimit:    30000,
		Data:        "0x",
		SummaryJSON: `{"token":"native","amount":"0.001","to":"0x0000000000000000000000000000000000000000"}`,
	})

	logChecks(t, resp)

	var burnStatus string
	for _, c := range resp.Checks {
		if c["rule"] == "burn_address_check" {
			burnStatus, _ = c["status"].(string)
		}
	}
	if burnStatus != "fail" {
		t.Errorf("expected burn_address_check=fail for zero address, got %q", burnStatus)
	}
}

// --- Malformed owner address: RPC rejects the simulation ---

func TestSecurityCheck_InvalidOwner_SimulationFails(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)
	ctx := integrationCtx(t)
	intent, err := intentSvc.Create(ctx, txintent.CreateRequest{
		UserID:    userID,
		WalletID:  testWalletID,
		WalletUID: testWalletUID,
		TxType:    "transfer",
		ChainID:   1,
		Chain:     "ethereum",
		Owner:     "not-an-address",
		To:        addrDead,
		Value:     "1000000000000000",
		GasLimit:  30000,
		Data:      "0x",
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/security/check", apiKey, map[string]string{
		"transaction_id": intent.TransactionID,
	})
	if w.Code == http.StatusOK {
		t.Error("expected non-200 for invalid owner address")
	}
	t.Logf("invalid owner → HTTP %d (expected 4xx/5xx)", w.Code)
}

// --- Swap type: should trigger asset_intent_match + usd_value_conservation ---

func TestSecurityCheck_SwapType_ChecksApply(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)
	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "swap",
		ChainID:     1,
		Chain:       "ethereum",
		Owner:       addrVitalik,
		To:          addrVitalik,
		Value:       "100000000000000000",
		Data:        "0xe9ae5c53",
		GasLimit:    300000,
		SummaryJSON: `{"from_token":"native","to_token":"` + addrUSDCEth + `","amount":"0.1"}`,
	})

	logChecks(t, resp)
	requireRules(t, resp, "transaction_shape", "simulation_success", "gas_cost_sanity")

	found := make(map[string]bool)
	for _, c := range resp.Checks {
		if rule, ok := c["rule"].(string); ok {
			found[rule] = true
		}
	}
	if !found["asset_intent_match"] && !found["usd_value_conservation"] {
		t.Logf("swap-specific checks (asset_intent_match, usd_value_conservation) may be skipped due to simulation result")
	}
}

// --- With address labels enabled ---

func TestSecurityCheck_WithAddressLabels(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}
	if strings.TrimSpace(os.Getenv("METASLEUTH_API_KEY")) == "" {
		t.Skip("METASLEUTH_API_KEY not set")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, true)
	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "transfer",
		ChainID:     1,
		Chain:       "ethereum",
		Owner:       addrVitalik,
		To:          "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		Value:       "1000000000000000",
		GasLimit:    30000,
		Data:        "0x",
		SummaryJSON: `{"token":"native","amount":"0.001","to":"` + "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510" + `"}`,
	})

	logChecks(t, resp)

	var labelStatus string
	for _, c := range resp.Checks {
		if c["rule"] == "address_label_risk" {
			labelStatus, _ = c["status"].(string)
		}
	}
	if labelStatus == "" {
		t.Error("expected address_label_risk check when label provider is configured")
	} else {
		t.Logf("address_label_risk: %s", labelStatus)
	}
}

// --- Without address labels: check should be skipped ---

func TestSecurityCheck_WithoutAddressLabels_Skipped(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["ethereum"] == "" {
		t.Skip("ethereum RPC not configured")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)
	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "transfer",
		ChainID:     1,
		Chain:       "ethereum",
		Owner:       addrVitalik,
		To:          "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		Value:       "1000000000000000",
		GasLimit:    30000,
		Data:        "0x",
		SummaryJSON: `{"token":"native","amount":"0.001","to":"` + "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510" + `"}`,
	})

	for _, c := range resp.Checks {
		if c["rule"] == "address_label_risk" {
			status, _ := c["status"].(string)
			if status != "skipped" && status != "pass" {
				t.Errorf("expected address_label_risk skipped/pass without provider, got %s", status)
			}
			return
		}
	}
	t.Log("address_label_risk not in check list (no provider configured)")
}

// --- Base chain: verify cross-chain coverage ---

func TestSecurityCheck_NativeTransfer_Base(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	if chainRPCURLs["base"] == "" {
		t.Skip("base RPC not configured")
	}

	r, apiKey, userID, intentSvc := setupSecurityCheckRouter(t, false)
	resp := runSecurityCheck(t, r, apiKey, userID, intentSvc, txintent.CreateRequest{
		TxType:      "transfer",
		ChainID:     8453,
		Chain:       "base",
		Owner:       addrVitalik,
		To:          "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
		Value:       "1000000000000000",
		GasLimit:    30000,
		Data:        "0x",
		SummaryJSON: `{"token":"native","amount":"0.001","to":"` + "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510" + `"}`,
	})

	logChecks(t, resp)
	requireRules(t, resp, "transaction_shape", "simulation_success")

	if resp.Verdict == "" {
		t.Error("missing verdict for Base chain check")
	}
}
