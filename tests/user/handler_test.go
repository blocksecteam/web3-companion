// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package user_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/portfolio"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	userhandler "github.com/blocksecteam/web3-companion/internal/user"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func toJSONMap(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON map: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal JSON map: %v", err)
	}
	return out
}

func setupUserRouter(t *testing.T) (*gin.Engine, string, string, *gorm.DB, *companion.Manager, *vault.Vault) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	cfg := testutil.NewTestConfig()
	auth.SetExternalURL(cfg.Server.ExternalURL)
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, cfg.SSMKeysDir(), nil, nil)
	pk := passkey.NewServer(db, &cfg.WebAuthn)
	manager := companion.NewManager(companion.Config{HomeRoot: t.TempDir()})

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	userhandler.RegisterRoutes(api, db, v, pk, manager, kms)
	return r, userID, apiKey, db, manager, v
}

func TestUserWalletDeleteRequiresTypedWalletIDBeforePasskey(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/user/wallets", apiKey, map[string]string{"wallet_id": "settings_wallet"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create wallet = %d, want 201: %s", w.Code, w.Body.String())
	}

	w = testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/settings_wallet", apiKey, map[string]string{
		"confirmation": "wrong_wallet",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete with wrong confirmation = %d, want 400: %s", w.Code, w.Body.String())
	}
	if err := db.Create(&storage.WalletAssetSnapshot{
		UserID:     userID,
		WalletID:   "settings_wallet",
		TotalUSD:   usd("0"),
		ObservedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create wallet asset snapshot: %v", err)
	}

	w = testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/settings_wallet", apiKey, map[string]string{
		"confirmation": "settings_wallet",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("delete with typed wallet id = %d, want 202 challenge: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ChallengeID string `json:"challenge_id"`
		SignURL     string `json:"sign_url"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.ChallengeID == "" || resp.SignURL == "" {
		t.Fatalf("delete challenge response missing fields: %+v", resp)
	}
}

func TestUserWalletDeleteRequiresFreshAssetSnapshot(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)
	testutil.CreateTestWallet(t, db, userID, "unknown_asset_wallet", "0x1234567890123456789012345678901234567890")

	w := testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/unknown_asset_wallet", apiKey, map[string]string{
		"confirmation": "unknown_asset_wallet",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("delete wallet without asset snapshot = %d, want 409: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !jsonContainsErrorCode(t, body, "wallet_asset_snapshot_required") {
		t.Fatalf("missing snapshot response = %s, want wallet_asset_snapshot_required", body)
	}

	if err := db.Create(&storage.WalletAssetSnapshot{
		UserID:     userID,
		WalletID:   "unknown_asset_wallet",
		TotalUSD:   usd("0"),
		ObservedAt: time.Now().Add(-2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create stale wallet asset snapshot: %v", err)
	}
	w = testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/unknown_asset_wallet", apiKey, map[string]string{
		"confirmation": "unknown_asset_wallet",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("delete wallet with stale snapshot = %d, want 409: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !jsonContainsErrorCode(t, body, "wallet_asset_snapshot_stale") {
		t.Fatalf("stale snapshot response = %s, want wallet_asset_snapshot_stale", body)
	}
}

func TestUserWalletDeleteRefreshesAggregateAssetsBeforePasskey(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)
	cfg := testutil.NewTestConfig()
	auth.SetExternalURL(cfg.Server.ExternalURL)
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, cfg.SSMKeysDir(), nil, nil)
	pk := passkey.NewServer(db, &cfg.WebAuthn)
	testutil.CreateTestWallet(t, db, userID, "live_asset_wallet", "0x1234567890123456789012345678901234567890")

	var queriedNetworks []string
	portals := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		network := r.URL.Query().Get("networks")
		queriedNetworks = append(queriedNetworks, network)
		if r.URL.Path != "/account" || r.URL.Query().Get("owner") == "" {
			t.Fatalf("unexpected Portals request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		switch network {
		case "ethereum":
			_, _ = w.Write([]byte(`{"balances":[{"address":"0xeeee","symbol":"ETH","name":"Ether","balance":1,"balanceUSD":0.07,"price":0.07,"decimals":18}]}`))
		case "base":
			_, _ = w.Write([]byte(`{"balances":[{"address":"0xbbbb","symbol":"USDC","name":"USD Coin","balance":1,"balanceUSD":0.06,"price":0.06,"decimals":6}]}`))
		default:
			t.Fatalf("unexpected network %q", network)
		}
	}))
	defer portals.Close()

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	assetInspector := portfolio.NewAssetInspector(
		[]string{"ethereum", "base"},
		portfolio.WithPortalsURL(portals.URL),
	)
	userhandler.RegisterRoutesWithOptions(api, db, v, pk, nil, kms, userhandler.WithWalletAssetInspector(assetInspector))

	w := testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/live_asset_wallet", apiKey, map[string]string{
		"confirmation": "live_asset_wallet",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("delete wallet with live assets = %d, want 409: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !jsonContainsErrorCode(t, body, "wallet_has_assets") {
		t.Fatalf("live asset response = %s, want wallet_has_assets", body)
	}
	if strings.Join(queriedNetworks, ",") != "ethereum,base" {
		t.Fatalf("queried networks = %#v, want ethereum and base", queriedNetworks)
	}
	var snapshot storage.WalletAssetSnapshot
	if err := db.Where("user_id = ? AND wallet_id = ?", userID, "live_asset_wallet").First(&snapshot).Error; err != nil {
		t.Fatalf("expected live asset snapshot to be persisted: %v", err)
	}
	if snapshot.TotalUSD.String() != "0.13" {
		t.Fatalf("snapshot total = %v, want 0.13", snapshot.TotalUSD)
	}
	var challengeCount int64
	db.Model(&storage.Challenge{}).Where("user_id = ? AND type = ?", userID, "delete_wallet").Count(&challengeCount)
	if challengeCount != 0 {
		t.Fatalf("asset-blocked live delete must not create challenge, count=%d", challengeCount)
	}
}

func TestUserWalletDeleteBlocksWalletWithAssets(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)
	testutil.CreateTestWallet(t, db, userID, "asset_wallet", "0x1234567890123456789012345678901234567890")
	if err := db.Create(&storage.WalletAssetSnapshot{
		UserID:     userID,
		WalletID:   "asset_wallet",
		TotalUSD:   usd("0.11"),
		ObservedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create wallet asset snapshot: %v", err)
	}

	w := testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/asset_wallet", apiKey, map[string]string{
		"confirmation": "asset_wallet",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("delete wallet with assets = %d, want 409: %s", w.Code, w.Body.String())
	}
	var challengeCount int64
	db.Model(&storage.Challenge{}).Where("user_id = ? AND type = ?", userID, "delete_wallet").Count(&challengeCount)
	if challengeCount != 0 {
		t.Fatalf("asset-blocked delete must not create challenge, count=%d", challengeCount)
	}
}

func TestUserWalletDeleteWithApprovedChallengeDeletesWallet(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)
	testutil.CreateTestWallet(t, db, userID, "delete_final_wallet", "0x1234567890123456789012345678901234567890")
	if err := db.Create(&storage.WalletAssetSnapshot{
		UserID:     userID,
		WalletID:   "delete_final_wallet",
		TotalUSD:   usd("0"),
		ObservedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create wallet asset snapshot: %v", err)
	}
	challengeID := approvedChallengeForPayload(t, db, userID, "delete_wallet", map[string]interface{}{
		"operation":    "delete_wallet",
		"wallet_id":    "delete_final_wallet",
		"confirmation": "delete_final_wallet",
	})

	w := testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/wallets/delete_final_wallet?challenge_id="+challengeID, apiKey, map[string]string{
		"confirmation": "delete_final_wallet",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("delete approved wallet = %d, want 200: %s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&storage.Wallet{}).Where("wallet_id = ? AND deleted_at IS NULL", "delete_final_wallet").Count(&count)
	if count != 0 {
		t.Fatalf("active deleted wallet record count = %d, want 0", count)
	}
	var retained storage.Wallet
	if err := db.Where("wallet_id = ? AND deleted_at IS NOT NULL", "delete_final_wallet").First(&retained).Error; err != nil {
		t.Fatalf("deleted wallet row should be retained: %v", err)
	}
	if retained.KeyStatus != "soft_deleted" {
		t.Fatalf("deleted wallet key status = %q, want soft_deleted", retained.KeyStatus)
	}

	w = testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/user/wallets", apiKey, map[string]string{"wallet_id": "delete_final_wallet"})
	if w.Code != http.StatusCreated {
		t.Fatalf("recreate deleted wallet id = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestUserPolicyDefinitionsDefaultCannotBeDeletedAndDeletedPoliciesAreHidden(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)

	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/user/policy-definitions", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list definitions = %d, want 200: %s", w.Code, w.Body.String())
	}
	var list struct {
		Definitions []struct {
			PolicyDefinitionID string `json:"policy_definition_id"`
			Name               string `json:"name"`
			IsDefault          bool   `json:"is_default"`
		} `json:"policy_definitions"`
	}
	testutil.ParseJSON(t, w, &list)
	if len(list.Definitions) != 1 || list.Definitions[0].Name != "default" || !list.Definitions[0].IsDefault {
		t.Fatalf("unexpected default definitions: %+v", list.Definitions)
	}

	deleteDefaultChallenge := approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation":            "delete_policy_definition",
		"policy_definition_id": list.Definitions[0].PolicyDefinitionID,
	})
	w = testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/policy-definitions/"+list.Definitions[0].PolicyDefinitionID+"?challenge_id="+deleteDefaultChallenge, apiKey, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete default = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !jsonContainsErrorCode(t, w.Body.String(), "default_policy_delete_blocked") {
		t.Fatalf("delete default response = %s, want default_policy_delete_blocked", w.Body.String())
	}

	createBody := map[string]interface{}{"name": "short leash", "mode": "manual"}
	createBody["challenge_id"] = approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation": "create_policy_definition",
		"request":   map[string]interface{}{"name": "short leash", "mode": "manual"},
	})
	w = testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/user/policy-definitions", apiKey, createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("create definition = %d, want 201: %s", w.Code, w.Body.String())
	}
	var created struct {
		PolicyDefinitionID string `json:"policy_definition_id"`
	}
	testutil.ParseJSON(t, w, &created)
	if created.PolicyDefinitionID == "" {
		t.Fatalf("missing created definition id: %+v", created)
	}

	deleteChallenge := approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation":            "delete_policy_definition",
		"policy_definition_id": created.PolicyDefinitionID,
	})
	w = testutil.DoAuthRequest(r, http.MethodDelete, "/api/v1/user/policy-definitions/"+created.PolicyDefinitionID+"?challenge_id="+deleteChallenge, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete custom = %d, want 200: %s", w.Code, w.Body.String())
	}

	w = testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/user/policy-definitions", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list after delete = %d, want 200: %s", w.Code, w.Body.String())
	}
	testutil.ParseJSON(t, w, &list)
	for _, def := range list.Definitions {
		if def.PolicyDefinitionID == created.PolicyDefinitionID {
			t.Fatalf("deleted definition should be hidden: %+v", list.Definitions)
		}
	}
}

func TestUserPolicyDefinitionUpdateCreatesVersionRefreshesUsageAndAudits(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)
	testutil.CreateTestWallet(t, db, userID, "definition_wallet", "0x1234567890123456789012345678901234567890")

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	createBody := map[string]interface{}{
		"name":                        "desk auto",
		"mode":                        "auto",
		"max_per_transaction_usd":     "100",
		"daily_transfer_limit_usd":    "200",
		"auto_sign_enabled":           true,
		"auto_sign_max_per_tx_usd":    "20",
		"auto_sign_daily_limit_usd":   "40",
		"auto_sign_max_count_day":     2,
		"recipient_whitelist_enabled": false,
		"expires_at":                  expiresAt.Format(time.RFC3339),
	}
	createBody["challenge_id"] = approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation": "create_policy_definition",
		"request":   toJSONMap(t, createBody),
	})
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/user/policy-definitions", apiKey, createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("create definition = %d, want 201: %s", w.Code, w.Body.String())
	}
	var created struct {
		PolicyDefinitionID string `json:"policy_definition_id"`
		CurrentVersionID   string `json:"current_version_id"`
	}
	testutil.ParseJSON(t, w, &created)

	instance := storage.PolicyInstance{
		PolicyInstanceID:          "pinst_user_api_refresh",
		UserID:                    userID,
		WalletID:                  "definition_wallet",
		PolicyDefinitionID:        created.PolicyDefinitionID,
		PolicyDefinitionVersionID: created.CurrentVersionID,
		DailyTransferUsedUSD:      usd("55"),
		AutoSignDailyUsedUSD:      usd("12"),
		AutoSignCountToday:        1,
		LastResetDate:             "2026-05-17",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}

	updateBody := map[string]interface{}{
		"name":                     "desk manual",
		"mode":                     "manual",
		"max_per_transaction_usd":  "50",
		"daily_transfer_limit_usd": "100",
	}
	updateBody["challenge_id"] = approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation":            "update_policy_definition",
		"policy_definition_id": created.PolicyDefinitionID,
		"request":              toJSONMap(t, updateBody),
	})
	w = testutil.DoAuthRequest(r, http.MethodPut, "/api/v1/user/policy-definitions/"+created.PolicyDefinitionID, apiKey, updateBody)
	if w.Code != http.StatusOK {
		t.Fatalf("update definition = %d, want 200: %s", w.Code, w.Body.String())
	}
	var updated struct {
		PolicyDefinitionID string `json:"policy_definition_id"`
		CurrentVersionID   string `json:"current_version_id"`
	}
	testutil.ParseJSON(t, w, &updated)
	if updated.CurrentVersionID == created.CurrentVersionID {
		t.Fatalf("current version did not change: %+v", updated)
	}

	var refreshed storage.PolicyInstance
	if err := db.First(&refreshed, "policy_instance_id = ?", instance.PolicyInstanceID).Error; err != nil {
		t.Fatalf("load refreshed instance: %v", err)
	}
	if refreshed.PolicyDefinitionVersionID != updated.CurrentVersionID || refreshed.DailyTransferUsedUSD.String() != "0" || refreshed.AutoSignDailyUsedUSD.String() != "0" || refreshed.AutoSignCountToday != 0 {
		t.Fatalf("instance should point at new version with refreshed usage: %+v", refreshed)
	}

	var audit storage.AuditLog
	if err := db.Where("user_id = ? AND event_type = ?", userID, "POLICY_DEFINITION_UPDATED").First(&audit).Error; err != nil {
		t.Fatalf("missing policy update audit: %v", err)
	}
	if !strings.Contains(audit.DataJSON, created.PolicyDefinitionID) || !strings.Contains(audit.DataJSON, updated.CurrentVersionID) {
		t.Fatalf("audit should include definition and new version ids: %s", audit.DataJSON)
	}
}

func TestUserWalletPolicyBindingCreatesInstanceAndAudits(t *testing.T) {
	r, userID, apiKey, db, _, _ := setupUserRouter(t)
	testutil.CreateTestWallet(t, db, userID, "bind_wallet", "0x1234567890123456789012345678901234567890")

	createBody := map[string]interface{}{"name": "desk manual", "mode": "manual", "max_per_transaction_usd": "25"}
	createBody["challenge_id"] = approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation": "create_policy_definition",
		"request":   map[string]interface{}{"name": "desk manual", "mode": "manual", "max_per_transaction_usd": "25"},
	})
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/user/policy-definitions", apiKey, createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("create definition = %d, want 201: %s", w.Code, w.Body.String())
	}
	var created struct {
		PolicyDefinitionID string `json:"policy_definition_id"`
		CurrentVersionID   string `json:"current_version_id"`
	}
	testutil.ParseJSON(t, w, &created)

	bindBody := map[string]interface{}{"policy_definition_id": created.PolicyDefinitionID}
	w = testutil.DoAuthRequest(r, http.MethodPut, "/api/v1/user/wallets/bind_wallet/policy-binding", apiKey, bindBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("request binding = %d, want 202: %s", w.Code, w.Body.String())
	}

	bindBody["challenge_id"] = approvedChallengeForPayload(t, db, userID, "policy_change", map[string]interface{}{
		"operation":            "bind_wallet_policy",
		"wallet_id":            "bind_wallet",
		"policy_definition_id": created.PolicyDefinitionID,
	})
	w = testutil.DoAuthRequest(r, http.MethodPut, "/api/v1/user/wallets/bind_wallet/policy-binding", apiKey, bindBody)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm binding = %d, want 200: %s", w.Code, w.Body.String())
	}
	var bound struct {
		PolicyInstanceID          string `json:"policy_instance_id"`
		WalletID                  string `json:"wallet_id"`
		PolicyDefinitionID        string `json:"policy_definition_id"`
		PolicyDefinitionVersionID string `json:"policy_definition_version_id"`
	}
	testutil.ParseJSON(t, w, &bound)
	if bound.WalletID != "bind_wallet" || bound.PolicyDefinitionID != created.PolicyDefinitionID || bound.PolicyDefinitionVersionID != created.CurrentVersionID {
		t.Fatalf("unexpected binding response: %+v", bound)
	}

	var instance storage.PolicyInstance
	if err := db.First(&instance, "policy_instance_id = ?", bound.PolicyInstanceID).Error; err != nil {
		t.Fatalf("load policy instance: %v", err)
	}
	current, err := policyservice.NewService(db).CurrentPolicy(userID, "bind_wallet", time.Now())
	if err != nil {
		t.Fatalf("CurrentPolicy: %v", err)
	}
	if current.PolicyID != instance.PolicyInstanceID || current.Name != "desk manual" || current.MaxPerTransactionUSD.String() != "25" {
		t.Fatalf("current policy should resolve bound definition: %+v", current)
	}

	var audit storage.AuditLog
	if err := db.Where("user_id = ? AND event_type = ?", userID, "POLICY_INSTANCE_BOUND").First(&audit).Error; err != nil {
		t.Fatalf("missing policy binding audit: %v", err)
	}
	if !strings.Contains(audit.DataJSON, created.PolicyDefinitionID) || !strings.Contains(audit.DataJSON, "bind_wallet") {
		t.Fatalf("audit should include wallet and definition ids: %s", audit.DataJSON)
	}
}

func usd(raw string) money.Amount {
	return money.MustParse(raw)
}

func approvedChallengeForPayload(t *testing.T, db *gorm.DB, userID, typ string, payload interface{}) string {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal challenge payload: %v", err)
	}
	sum := sha256.Sum256(payloadBytes)
	envelope, err := json.Marshal(map[string]interface{}{
		"payload":      payload,
		"payload_hash": hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatalf("marshal challenge envelope: %v", err)
	}
	ch := storage.Challenge{
		ChallengeID: "ch_" + typ + "_user_" + time.Now().Format("150405.000000000"),
		UserID:      userID,
		Type:        typ,
		Status:      "approved",
		PayloadJSON: string(envelope),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	return ch.ChallengeID
}

func jsonContainsKey(t *testing.T, body string, key string) bool {
	t.Helper()
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	_, ok := data[key]
	return ok
}

func jsonContainsErrorCode(t *testing.T, body string, code string) bool {
	t.Helper()
	var data struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatalf("parse error body: %v", err)
	}
	return data.Error.Code == code
}
