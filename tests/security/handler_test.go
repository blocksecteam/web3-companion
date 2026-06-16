// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/security"
	securitychecks "github.com/blocksecteam/web3-companion/internal/security/checks"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

const (
	testOwner   = "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	testRouter  = "0x3fC91A3afd70395cd496c647D5a6CC9D4B2b7FAD"
	testToken   = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	testAccount = "0x1111111111111111111111111111111111111111"
)

type stubSimulator struct {
	result   *security.SimulationResult
	fundFlow []string
	err      error
}

func (s stubSimulator) Simulate(ctx context.Context, req security.CheckRequest) (*security.SimulationResult, []string, error) {
	return s.result, s.fundFlow, s.err
}

type stubLabelProvider struct {
	labels map[string]security.AddressLabel
	seen   *[]string
	err    error
}

type stubFunctionSignatureResolver map[string]string

func (r stubFunctionSignatureResolver) ResolveFunctions(ctx context.Context, selectors []string) (map[string]string, error) {
	out := make(map[string]string, len(selectors))
	for _, selector := range selectors {
		if signature, ok := r[strings.ToLower(selector)]; ok {
			out[strings.ToLower(selector)] = signature
		}
	}
	return out, nil
}

func (p stubLabelProvider) Labels(ctx context.Context, chainID int, addresses []string) ([]security.AddressLabel, error) {
	if p.err != nil {
		return nil, p.err
	}
	out := make([]security.AddressLabel, 0, len(addresses))
	for _, addr := range addresses {
		if p.seen != nil {
			*p.seen = append(*p.seen, strings.ToLower(addr))
		}
		if label, ok := p.labels[strings.ToLower(addr)]; ok {
			out = append(out, label)
		}
	}
	return out, nil
}

func setupSecurityRouter(t *testing.T, sim *security.SimulationResult) (*gin.Engine, string) {
	t.Helper()
	r, apiKey, _, _ := setupSecurityRouterWithDB(t, sim)
	return r, apiKey
}

func setupSecurityRouterWithDB(t *testing.T, sim *security.SimulationResult) (*gin.Engine, string, string, *gorm.DB) {
	t.Helper()
	return setupSecurityRouterWithDBAndConfig(t, sim, security.SecurityConfig{})
}

func setupSecurityRouterWithDBAndConfig(t *testing.T, sim *security.SimulationResult, cfg security.SecurityConfig) (*gin.Engine, string, string, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	passkeys := passkey.NewServer(db, testWebAuthnConfig())

	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, nil)
	// Default fundFlow includes testRouter so transfer_intent_match's
	// recipient sub-check can match the standard fixture. Tests with
	// stricter fundFlow expectations build their own handler.
	opts := []security.HandlerOption{
		security.WithSimulator(stubSimulator{result: sim, fundFlow: []string{testRouter}}),
		security.WithFunctionSignatureResolver(stubFunctionSignatureResolver{}),
		security.WithIntentStore(db),
		security.WithPasskeyServer(passkeys),
		security.WithCheckRegistry(reg),
	}
	if cfg.SecurityAgentReviewMode != "" || cfg.AllowReviewClearSet {
		opts = append(opts, security.WithSecurityConfig(cfg))
	}
	h := security.NewHandler(opts...)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey, userID, db
}

func testWebAuthnConfig() *config.WebAuthnConfig {
	return &config.WebAuthnConfig{
		RPID:                    "localhost",
		RPName:                  "Web3 Test",
		RPOrigin:                "http://localhost:8080",
		ChallengeTimeoutSeconds: 120,
	}
}

func okSimulation(balanceChanges []security.BalanceChange) *security.SimulationResult {
	return &security.SimulationResult{
		Code:    0,
		Message: "ok",
		Data: &security.SimulationData{
			Status:          true,
			Sender:          testOwner,
			Receiver:        testRouter,
			CallData:        "0xdeadbeef",
			GasLimit:        300000,
			GasPrice:        "1000000000",
			GasUsed:         100000,
			TransactionFee:  "100000000000000",
			Value:           "0",
			BalanceChanges:  balanceChanges,
			Trace:           &security.SimulationCall{Status: true, From: testOwner, To: testRouter, Calls: []security.SimulationCall{{Status: true, From: testOwner, To: testRouter, Func: "0xdeadbeef"}}},
			SimulationLabel: "rpc",
		},
		Raw: json.RawMessage(`{"provider":"rpc","status":true}`),
	}
}

func ownerSpendReceiveChanges(spendUSD, receiveUSD string) []security.BalanceChange {
	return []security.BalanceChange{{
		Account: testOwner,
		Assets: []security.BalanceChangeAsset{
			{Address: testToken, BaseUnits: "-1000000", Amount: "-1", Type: "erc20", Value: spendUSD},
			{Address: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", BaseUnits: "-100000000000000000", Amount: "-0.1", Type: "native", Value: "0.2"},
			{Address: testToken, BaseUnits: "990000", Amount: "0.99", Type: "erc20", Value: receiveUSD},
		},
	}}
}

func validCheckBody(data string) map[string]interface{} {
	return map[string]interface{}{
		"chain_id":  1,
		"chain":     "ethereum",
		"owner":     testOwner,
		"to":        testRouter,
		"data":      data,
		"value":     "0",
		"gas_limit": 300000,
		"gas_price": "1000000000",
	}
}

func TestSecurity_Check_Pass(t *testing.T) {
	// usd_value_conservation is the only check that consumes both spending
	// and receiving USD totals; it only applies to tx_type=swap (see
	// USDValueConservation.AppliesTo). Use tx_type=swap so this test exercises
	// the within-cap loss path.
	r, apiKey, userID, db := setupSecurityRouterWithDB(t, okSimulation(ownerSpendReceiveChanges("100", "99")))
	// asset_intent_match expects a tx_summary with source_asset / target_asset
	// matching the simulated effect's tokens; supply one so this test stays
	// pass-clean. The fixture uses testToken (USDC) on both sides.
	summary := fmt.Sprintf(`{"source_asset":%q,"target_asset":%q}`, testToken, testToken)
	txi := createSecurityIntentWithTxTypeAndSummary(t, db, userID, "0xa9059cbb", "swap", summary)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Verdict string `json:"verdict"`
		Effect  struct {
			NetUSD *string `json:"net_usd"`
		} `json:"effect"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Verdict != "pass" {
		t.Fatalf("verdict = %q, want pass\n%s", resp.Verdict, w.Body.String())
	}
	if resp.Effect.NetUSD == nil {
		t.Fatal("effect.net_usd is nil for same-chain priced simulation")
	}
	assertRuleStatus(t, w, "usd_value_conservation", "pass")
	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", txi.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if stored.Status != txintent.StatusSecurityReviewRequired || stored.SecurityVerdict != "pass" {
		t.Fatalf("unexpected intent security status: %#v", stored)
	}
}

// TestSecurity_Check_SkippedRuleAppearsInResponse guards the orchestrator's
// "list every rule, including skipped ones" contract. usd_value_conservation
// has AppliesTo=["swap"], so on a transfer it must still appear in checks[]
// with Status="skipped" — never silently dropped — so the All Checks table
// shows the rule exists and explains why it did not contribute.
func TestSecurity_Check_SkippedRuleAppearsInResponse(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDB(t, okSimulation(ownerSpendReceiveChanges("100", "99")))
	// transfer_intent_match expects a transfer summary matching the fixture;
	// supply one so this test stays focused on usd_value_conservation's
	// skipped behaviour rather than collateral warns from other rules.
	transferSummary := fmt.Sprintf(`{"token":%q,"amount":"1","to":%q}`, testToken, testRouter)
	txi := createSecurityIntentWithTxTypeAndSummary(t, db, userID, "0xa9059cbb", "transfer", transferSummary)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	assertRuleStatus(t, w, "usd_value_conservation", "skipped")

	var resp struct {
		Verdict string `json:"verdict"`
		Checks  []struct {
			Rule          string `json:"rule"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			Status        string `json:"status"`
			StatusMeaning string `json:"status_meaning"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Verdict != "pass" {
		t.Errorf("skipped rules must not affect verdict; got %q, want pass", resp.Verdict)
	}
	var found bool
	for _, c := range resp.Checks {
		if c.Rule != "usd_value_conservation" {
			continue
		}
		found = true
		if c.Name == "" || c.Description == "" || c.StatusMeaning == "" {
			t.Errorf("skipped rule must still carry Name/Description/StatusMeaning; got %#v", c)
		}
	}
	if !found {
		t.Fatal("usd_value_conservation missing from checks[] — skipped rules must still appear")
	}
}

func TestSecurity_Check_PassAllowsSigningWhenReviewModeWarn(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDBAndConfig(t, okSimulation(ownerSpendReceiveChanges("100", "99")), security.SecurityConfig{
		SecurityAgentReviewMode: "warn",
	})
	transferSummary := fmt.Sprintf(`{"token":%q,"amount":"1","to":%q}`, testToken, testRouter)
	txi := createSecurityIntentWithTxTypeAndSummary(t, db, userID, "0xa9059cbb", "transfer", transferSummary)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", txi.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if stored.Status != txintent.StatusSecurityPassed || stored.SecurityVerdict != "pass" {
		t.Fatalf("unexpected intent security status: %#v", stored)
	}
}

func TestSecurity_Check_RejectsRawTransactionFields(t *testing.T) {
	r, apiKey := setupSecurityRouter(t, okSimulation(nil))
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, validCheckBody("0x"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for raw fields", w.Code)
	}
}

func TestSecurity_Check_UnlimitedApprove_Blocked(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDB(t, okSimulation(ownerSpendReceiveChanges("100", "99")))
	data := "0x095ea7b3000000000000000000000000" + testRouter[2:] + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	txi := createSecurityIntent(t, db, userID, data, false)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	assertRuleStatus(t, w, "unlimited_approval", "fail")
}

func TestSecurity_Check_SetApprovalForAll_Blocked(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDB(t, okSimulation(ownerSpendReceiveChanges("100", "99")))
	data := "0xa22cb465000000000000000000000000" + testRouter[2:] + "0000000000000000000000000000000000000000000000000000000000000001"
	txi := createSecurityIntent(t, db, userID, data, false)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	assertRuleStatus(t, w, "nft_approval_for_all", "fail")
}

func TestSecurity_Check_AddressLabelRiskFailsForSanctionedRecipient(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	labelProvider := stubLabelProvider{labels: map[string]security.AddressLabel{
		strings.ToLower(testRouter): {
			Address:    testRouter,
			ChainID:    1,
			NameTag:    "Blocked recipient",
			Attributes: []security.LabelValue{{Code: 4015, Name: "SANCTIONED"}},
		},
	}}
	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, labelProvider)
	h := security.NewHandler(
		security.WithSimulator(stubSimulator{result: okSimulation(ownerSpendReceiveChanges("100", "99"))}),
		security.WithAddressLabelProvider(labelProvider),
		security.WithFunctionSignatureResolver(stubFunctionSignatureResolver{}),
		security.WithIntentStore(db),
		security.WithCheckRegistry(reg),
	)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", false)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	assertRuleStatus(t, w, "address_label_risk", "fail")
}

func TestSecurity_Check_AttachesAddressLabelsToSimulationTrace(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	traceTarget := "0xbeef000000000000000000000000000000000001"
	sim := okSimulation(ownerSpendReceiveChanges("100", "99"))
	sim.Data.Trace.Calls[0].To = traceTarget
	seen := []string{}
	labelProvider := stubLabelProvider{labels: map[string]security.AddressLabel{
		strings.ToLower(testRouter): {
			Address:    testRouter,
			ChainID:    1,
			NameTag:    "Uniswap Universal Router",
			MainEntity: "Uniswap",
			Categories: []security.LabelValue{{Code: 3008, Name: "DEX"}, {Code: 3002, Name: "CEX"}},
			Attributes: []security.LabelValue{{Code: 4009, Name: "Hot Wallet"}, {Code: 4003, Name: "KYC"}},
		},
		strings.ToLower(traceTarget): {
			Address:    traceTarget,
			ChainID:    1,
			NameTag:    "Trace Target",
			MainEntity: "Trace Entity",
			Categories: []security.LabelValue{{Code: 3008, Name: "DEX"}},
		},
	}, seen: &seen}
	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, labelProvider)
	h := security.NewHandler(
		security.WithSimulator(stubSimulator{result: sim, fundFlow: []string{traceTarget}}),
		security.WithAddressLabelProvider(labelProvider),
		security.WithFunctionSignatureResolver(stubFunctionSignatureResolver{}),
		security.WithIntentStore(db),
		security.WithCheckRegistry(reg),
	)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", false)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Simulation struct {
			Provider       string                   `json:"provider"`
			Status         *bool                    `json:"status"`
			Trace          security.SimulationCall  `json:"trace"`
			BalanceChanges []security.BalanceChange `json:"balanceChanges"`
		} `json:"simulation"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Simulation.Provider != "rpc" {
		t.Fatalf("simulation provider = %q, want rpc", resp.Simulation.Provider)
	}
	if resp.Simulation.Status != nil {
		t.Fatalf("simulation raw payload should not expose top-level status: %s", w.Body.String())
	}
	if resp.Simulation.Trace.ToLabel == nil || resp.Simulation.Trace.ToLabel.Label != "Uniswap Universal Router" {
		t.Fatalf("trace to_label not attached: %s", w.Body.String())
	}
	if resp.Simulation.Trace.ToLabel.Attributes != "CEX, DEX, KYC, Hot Wallet" {
		t.Fatalf("trace to_label attributes = %q, want stable merged attributes", resp.Simulation.Trace.ToLabel.Attributes)
	}
	if len(resp.Simulation.Trace.Calls) != 1 {
		t.Fatalf("trace calls = %d, want nested child call: %s", len(resp.Simulation.Trace.Calls), w.Body.String())
	}
	if resp.Simulation.Trace.Calls[0].Func != "0xdeadbeef" {
		t.Fatalf("child trace func = %q, want selector only", resp.Simulation.Trace.Calls[0].Func)
	}
	if resp.Simulation.Trace.Calls[0].ToLabel == nil || resp.Simulation.Trace.Calls[0].ToLabel.Label != "Trace Target" {
		t.Fatalf("child trace to_label not attached from trace-only address: %s", w.Body.String())
	}
	if !containsAddress(seen, traceTarget) {
		t.Fatalf("label provider did not receive trace-only address %s; got %v", traceTarget, seen)
	}
	if len(resp.Simulation.BalanceChanges) != 1 {
		t.Fatalf("balanceChanges = %d, want simplified balance changes", len(resp.Simulation.BalanceChanges))
	}
	body := w.Body.String()
	if strings.Contains(body, `"input"`) || strings.Contains(body, `"output"`) ||
		strings.Contains(body, "call_trace") || strings.Contains(body, "state_diff") ||
		strings.Contains(body, "name_tag") || strings.Contains(body, "main_entity") ||
		strings.Contains(body, "categories") || strings.Contains(body, "risk_reasons") {
		t.Fatalf("simulation response should not expose unsimplified trace fields: %s", body)
	}
}

func TestSecurity_Check_SimplifiesSimulationTrace(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	sim := okSimulation(nil)
	sim.Data.Trace = &security.SimulationCall{
		ID:        10,
		Status:    false,
		Error:     "execution reverted",
		Type:      "CALL",
		From:      testOwner,
		To:        testRouter,
		FromLabel: &security.TraceLabel{},
		ToLabel:   &security.TraceLabel{Label: "Router"},
		Value:     "1500000000000000000",
		Func:      "0xa9059cbb",
		GasLimit:  300000,
		GasUsed:   100000,
		Calls: []security.SimulationCall{{
			Status:   true,
			Type:     "CALL",
			From:     testRouter,
			To:       testToken,
			Value:    "0",
			Func:     "0x12345678",
			GasLimit: 42000,
			GasUsed:  21000,
		}},
	}
	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, nil)
	h := security.NewHandler(
		security.WithSimulator(stubSimulator{result: sim}),
		security.WithFunctionSignatureResolver(stubFunctionSignatureResolver{
			"0xa9059cbb": "transfer(address,uint256)",
		}),
		security.WithIntentStore(db),
		security.WithCheckRegistry(reg),
	)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	txi := createSecurityIntent(t, db, userID, "0xa9059cbb", false)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Simulation struct {
			Trace map[string]interface{} `json:"trace"`
		} `json:"simulation"`
	}
	testutil.ParseJSON(t, w, &resp)
	trace := resp.Simulation.Trace
	if trace["func"] != "transfer(address,uint256)" {
		t.Fatalf("trace func = %#v, want resolved signature\n%s", trace["func"], w.Body.String())
	}
	if trace["value"] != 1.5 {
		t.Fatalf("trace value = %#v, want 1.5\n%s", trace["value"], w.Body.String())
	}
	if trace["error"] != "execution reverted" {
		t.Fatalf("trace error = %#v, want execution reverted\n%s", trace["error"], w.Body.String())
	}
	if _, ok := trace["from_label"]; ok {
		t.Fatalf("empty from_label should be omitted: %s", w.Body.String())
	}
	if _, ok := trace["to_label"]; !ok {
		t.Fatalf("non-empty to_label should be present: %s", w.Body.String())
	}
	for _, key := range []string{"id", "status", "gasLimit", "gasUsed"} {
		if _, ok := trace[key]; ok {
			t.Fatalf("trace should not expose %s: %s", key, w.Body.String())
		}
	}
	calls, ok := trace["calls"].([]interface{})
	if !ok || len(calls) != 1 {
		t.Fatalf("trace calls = %#v, want one child\n%s", trace["calls"], w.Body.String())
	}
	child := calls[0].(map[string]interface{})
	if child["func"] != "0x12345678" {
		t.Fatalf("unresolved child func = %#v, want raw selector\n%s", child["func"], w.Body.String())
	}
	if _, ok := child["value"]; ok {
		t.Fatalf("zero child value should be omitted: %s", w.Body.String())
	}
	for _, key := range []string{"id", "status", "gasLimit", "gasUsed"} {
		if _, ok := child[key]; ok {
			t.Fatalf("child trace should not expose %s: %s", key, w.Body.String())
		}
	}
}

func containsAddress(addresses []string, want string) bool {
	for _, addr := range addresses {
		if strings.EqualFold(addr, want) {
			return true
		}
	}
	return false
}

func TestSecurity_CheckWarnVerdictRequiresReview(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, nil)
	h := security.NewHandler(
		security.WithSimulator(stubSimulator{result: okSimulation([]security.BalanceChange{{
			Account: testOwner,
			Assets:  []security.BalanceChangeAsset{{Address: testToken, BaseUnits: "-1000000", Amount: "-1", Type: "erc20", Value: "100"}},
		}})}),
		security.WithFunctionSignatureResolver(stubFunctionSignatureResolver{}),
		security.WithIntentStore(db),
		security.WithCheckRegistry(reg),
	)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", true)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", txi.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if stored.Status != txintent.StatusSecurityReviewRequired {
		t.Fatalf("status = %q, want %q", stored.Status, txintent.StatusSecurityReviewRequired)
	}
	if stored.SecurityConfirmedAt != nil {
		t.Fatalf("security_confirmed_at = %v, want nil for blocked warning", stored.SecurityConfirmedAt)
	}
}

func TestSecurity_ClearReviewAllowsAgentToOverrideFailedVerdict(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDBAndConfig(t, okSimulation(ownerSpendReceiveChanges("100", "0")), security.SecurityConfig{
		AllowReviewClear:    true,
		AllowReviewClearSet: true,
	})
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", false)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("check got %d, want 200: %s", w.Code, w.Body.String())
	}
	clear := testutil.DoAuthRequest(r, "POST", "/api/v1/security/clear-review", apiKey, map[string]interface{}{
		"transaction_id": txi.TransactionID,
		"reviewer":       "web3_security_agent",
		"decision":       "safe",
		"confidence":     "high",
		"reason":         "agent accepted this despite static failure",
	})
	if clear.Code != http.StatusOK {
		t.Fatalf("clear got %d, want 200: %s", clear.Code, clear.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	testutil.ParseJSON(t, clear, &resp)
	if resp.Status != txintent.StatusSecurityPassed {
		t.Fatalf("status = %q, want %q", resp.Status, txintent.StatusSecurityPassed)
	}
}

func TestSecurity_ClearReviewRejectsAgentClearanceWhenDisabled(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDBAndConfig(t, okSimulation(ownerSpendReceiveChanges("100", "0")), security.SecurityConfig{
		AllowReviewClear:    false,
		AllowReviewClearSet: true,
	})
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", false)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("check got %d, want 200: %s", w.Code, w.Body.String())
	}

	clear := testutil.DoAuthRequest(r, "POST", "/api/v1/security/clear-review", apiKey, map[string]interface{}{
		"transaction_id": txi.TransactionID,
		"reviewer":       "web3_security_agent",
		"decision":       "safe",
		"confidence":     "high",
		"reason":         "agent accepted this despite static failure",
	})
	if clear.Code != http.StatusForbidden {
		t.Fatalf("clear got %d, want 403: %s", clear.Code, clear.Body.String())
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", txi.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if stored.Status != txintent.StatusSecurityReviewRequired {
		t.Fatalf("status = %q, want %q", stored.Status, txintent.StatusSecurityReviewRequired)
	}
}

func TestSecurity_OverrideRequiresPasskeyThenAllowsUserToAcceptReviewRequiredIntent(t *testing.T) {
	r, apiKey, userID, db := setupSecurityRouterWithDB(t, okSimulation(ownerSpendReceiveChanges("100", "0")))
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", false)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("check got %d, want 200: %s", w.Code, w.Body.String())
	}
	override := testutil.DoAuthRequest(r, "POST", "/api/v1/security/override", apiKey, map[string]interface{}{
		"transaction_id": txi.TransactionID,
		"reason":         "user explicitly accepts the risk",
	})
	if override.Code != http.StatusAccepted {
		t.Fatalf("override got %d, want 202 pending user auth: %s", override.Code, override.Body.String())
	}
	var resp struct {
		Status      string `json:"status"`
		ChallengeID string `json:"challenge_id"`
		SignURL     string `json:"sign_url"`
	}
	testutil.ParseJSON(t, override, &resp)
	if resp.Status != txintent.StatusPendingUserAuth || resp.ChallengeID == "" || resp.SignURL == "" {
		t.Fatalf("unexpected pending override response: %#v", resp)
	}
	var pending storage.TransactionIntent
	if err := db.First(&pending, "transaction_id = ?", txi.TransactionID).Error; err != nil {
		t.Fatalf("load pending intent: %v", err)
	}
	if pending.Status != txintent.StatusSecurityReviewRequired {
		t.Fatalf("intent status changed before passkey approval: %q", pending.Status)
	}
	var challenge storage.Challenge
	if err := db.First(&challenge, "challenge_id = ?", resp.ChallengeID).Error; err != nil {
		t.Fatalf("load override challenge: %v", err)
	}
	if challenge.Type != "security_override" {
		t.Fatalf("challenge type = %q, want security_override", challenge.Type)
	}
	if !strings.Contains(challenge.PayloadJSON, `"security"`) || !strings.Contains(challenge.PayloadJSON, `"payload_hash"`) {
		t.Fatalf("override challenge payload missing security binding: %s", challenge.PayloadJSON)
	}
	if err := db.Model(&challenge).Update("status", "approved").Error; err != nil {
		t.Fatalf("approve challenge: %v", err)
	}

	approved := testutil.DoAuthRequest(r, "POST", "/api/v1/security/override", apiKey, map[string]interface{}{
		"transaction_id": txi.TransactionID,
		"reason":         "user explicitly accepts the risk",
		"challenge_id":   resp.ChallengeID,
	})
	if approved.Code != http.StatusOK {
		t.Fatalf("approved override got %d, want 200: %s", approved.Code, approved.Body.String())
	}
	var approvedResp struct {
		Status string `json:"status"`
	}
	testutil.ParseJSON(t, approved, &approvedResp)
	if approvedResp.Status != txintent.StatusSecurityOverridden {
		t.Fatalf("status = %q, want %q", approvedResp.Status, txintent.StatusSecurityOverridden)
	}
}

func TestSecurity_Check_SimulationFailure_Fails(t *testing.T) {
	sim := &security.SimulationResult{
		Code:    0,
		Message: "ok",
		Data:    &security.SimulationData{Status: false, ErrorInfo: "execution reverted"},
		Raw:     json.RawMessage(`{"provider":"rpc","status":false}`),
	}
	r, apiKey, userID, db := setupSecurityRouterWithDB(t, sim)
	txi := createSecurityIntent(t, db, userID, "0xdeadbeef", false)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/security/check", apiKey, map[string]interface{}{"transaction_id": txi.TransactionID})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	assertRuleStatus(t, w, "simulation_success", "fail")
}

func TestSecurity_Check_Unauthorized(t *testing.T) {
	r, _ := setupSecurityRouter(t, okSimulation(nil))
	w := testutil.DoRequest(r, "POST", "/api/v1/security/check", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func assertRuleStatus(t *testing.T, w *httptest.ResponseRecorder, rule, status string) {
	t.Helper()
	var resp struct {
		Verdict string `json:"verdict"`
		Checks  []struct {
			Rule   string `json:"rule"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v\n%s", err, w.Body.String())
	}
	for _, check := range resp.Checks {
		if check.Rule == rule {
			if check.Status != status {
				t.Fatalf("%s status = %q, want %q\n%s", rule, check.Status, status, w.Body.String())
			}
			return
		}
	}
	t.Fatalf("rule %q not found\n%s", rule, w.Body.String())
}

func createSecurityIntent(t *testing.T, db *gorm.DB, userID, data string, crossChain bool) storage.TransactionIntent {
	t.Helper()
	return createSecurityIntentWithTxType(t, db, userID, data, crossChain, "transfer")
}

func createSecurityIntentWithTxType(t *testing.T, db *gorm.DB, userID, data string, crossChain bool, txType string) storage.TransactionIntent {
	t.Helper()
	return createSecurityIntentWithTxTypeAndSummary(t, db, userID, data, txType, "")
}

func createSecurityIntentWithTxTypeAndSummary(t *testing.T, db *gorm.DB, userID, data, txType, summary string) storage.TransactionIntent {
	t.Helper()
	intent, err := txintent.NewService(db, vault.NewTestKMSClient()).Create(t.Context(), txintent.CreateRequest{
		UserID:      userID,
		WalletID:    "wallet_security",
		WalletUID:   "wuid_wallet_security",
		Chain:       "ethereum",
		ChainID:     1,
		TxType:      txType,
		Owner:       testOwner,
		To:          testRouter,
		Value:       "0",
		Data:        data,
		GasLimit:    300000,
		Recipient:   testRouter,
		SummaryJSON: summary,
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}
	return intent
}
