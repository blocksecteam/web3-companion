// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	ssmhandler "github.com/blocksecteam/web3-companion/internal/ssm/handler"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupWebAuthnRouter(t *testing.T) (*gin.Engine, string) {
	r, apiKey, _ := setupWebAuthnRouterWithDB(t)
	return r, apiKey
}

func setupWebAuthnRouterWithDB(t *testing.T) (*gin.Engine, string, *gorm.DB) {
	r, _, apiKey, db := setupWebAuthnRouterWithUserDB(t)
	return r, apiKey, db
}

func setupWebAuthnRouterWithUserDB(t *testing.T) (*gin.Engine, string, string, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)

	cfg := testutil.NewTestConfig()
	pk := passkey.NewServer(db, &cfg.WebAuthn)

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	ssmhandler.RegisterWebAuthnCredentialRoutes(api, pk)
	ssmhandler.RegisterWebAuthnBrowserRoutes(api, pk)
	ssmhandler.RegisterWebAuthnConfirmRoutes(r, pk)

	return r, userID, apiKey, db
}

func TestWebAuthn_ListCredentials(t *testing.T) {
	r, apiKey := setupWebAuthnRouter(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/webauthn/credentials", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestWebAuthn_DeleteCredential_NotFound(t *testing.T) {
	r, apiKey := setupWebAuthnRouter(t)
	w := testutil.DoAuthRequest(r, "DELETE", "/api/v1/ssm/webauthn/credentials/nonexistent", apiKey, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestWebAuthn_GetChallengeStatus(t *testing.T) {
	r, userID, apiKey, db := setupWebAuthnRouterWithUserDB(t)
	challengeID := "ch_status"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "policy_change",
		Status:      "pending",
		PayloadJSON: `{"operation":"update_policy"}`,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/webauthn/challenge/"+challengeID, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestWebAuthn_GetChallengeStatusDoesNotCreateAuthChallenge(t *testing.T) {
	r, apiKey, db := setupWebAuthnRouterWithDB(t)
	userID := "test_user_TestWebAuthn_GetChallengeStatusDoesNotCreateAuthChallenge"
	challengeID := "ch_status_readonly"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "policy_change",
		Status:      "pending",
		PayloadJSON: `{"operation":"update_policy"}`,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/webauthn/challenge/"+challengeID, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	if _, ok := resp["public_key"]; ok {
		t.Fatalf("status endpoint should not return public_key options: %#v", resp)
	}

	var ch storage.Challenge
	if err := db.Where("challenge_id = ?", challengeID).First(&ch).Error; err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(ch.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["auth_challenge"]; ok {
		t.Fatalf("status endpoint created auth_challenge side effect: %#v", payload)
	}
}

func TestWebAuthn_GetChallengeStatus_NotFound(t *testing.T) {
	r, apiKey := setupWebAuthnRouter(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/webauthn/challenge/nonexistent", apiKey, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestWebAuthn_ConfirmPage(t *testing.T) {

	r, userID, _, db := setupWebAuthnRouterWithUserDB(t)
	challengeID := "ch_confirm_post"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "policy_change",
		Status:      "pending",
		PayloadJSON: `{"operation":"update_policy"}`,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/confirm", map[string]string{
		"challenge_id":  challengeID,
		"credential_id": "test-cred-id",
		"public_key":    "test-public-key",
	}, nil)
	t.Logf("POST /auth/confirm response: %d", w.Code)
}

func TestWebAuthn_PublicChallengeOptions(t *testing.T) {
	r, userID, _, db := setupWebAuthnRouterWithUserDB(t)
	challengeID := "ch_public_options"
	payload := map[string]interface{}{
		"challenge": "public-options-seed",
		"payload": map[string]interface{}{
			"operation": "export_wallet",
			"wallet_id": "public_challenge_wallet",
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "export_wallet",
		Status:      "pending",
		PayloadJSON: string(payloadBytes),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoRequest(r, "GET", "/auth/challenge/"+challengeID, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("public challenge status = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	if resp["public_key"] == nil {
		t.Fatal("missing public_key")
	}
}

func TestWebAuthn_PublicChallengeOptionsIncludesSummary(t *testing.T) {
	r, userID, _, db := setupWebAuthnRouterWithUserDB(t)
	challengeID := "ch_public_summary"
	payload := map[string]interface{}{
		"challenge": "summary-seed",
		"payload": map[string]interface{}{
			"operation":    "delete_wallet",
			"wallet_id":    "wallet_summary",
			"confirmation": "wallet_summary",
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "delete_wallet",
		Status:      "pending",
		PayloadJSON: string(payloadBytes),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoRequest(r, "GET", "/auth/challenge/"+challengeID, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("public challenge status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Summary struct {
			Operation string `json:"operation"`
			Title     string `json:"title"`
			WalletID  string `json:"wallet_id"`
			Details   []struct {
				Label string `json:"label"`
				Value string `json:"value"`
			} `json:"details"`
		} `json:"summary"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Summary.Operation != "delete_wallet" || resp.Summary.WalletID != "wallet_summary" || resp.Summary.Title == "" {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
	if len(resp.Summary.Details) == 0 {
		t.Fatalf("summary should include display details: %+v", resp.Summary)
	}
}

func TestWebAuthn_PublicChallengeOptionsExpiresExpiredPendingChallenge(t *testing.T) {
	r, userID, _, db := setupWebAuthnRouterWithUserDB(t)
	challengeID := "ch_public_expired"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "export_wallet",
		Status:      "pending",
		PayloadJSON: `{"challenge":"expired-seed","payload":{"operation":"export_wallet","wallet_id":"expired_wallet"}}`,
		CreatedAt:   time.Now().Add(-2 * time.Minute),
		ExpiresAt:   time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoRequest(r, "GET", "/auth/challenge/"+challengeID, nil, nil)
	if w.Code != http.StatusGone {
		t.Fatalf("public expired challenge = %d, want 410: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Error.Code != "challenge_expired" {
		t.Fatalf("error code = %q, want challenge_expired", resp.Error.Code)
	}

	var ch storage.Challenge
	if err := db.Where("challenge_id = ?", challengeID).First(&ch).Error; err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	if ch.Status != "timeout" {
		t.Fatalf("expired challenge status = %q, want timeout", ch.Status)
	}
}

func TestWebAuthn_CancelChallengeUpdatesPendingChallenge(t *testing.T) {
	r, userID, apiKey, db := setupWebAuthnRouterWithUserDB(t)
	challengeID := "ch_cancel_pending"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "delete_wallet",
		Status:      "pending",
		PayloadJSON: `{"challenge":"cancel-seed","payload":{"operation":"delete_wallet","wallet_id":"cancel_wallet"}}`,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/webauthn/challenge/"+challengeID+"/cancel", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel challenge = %d, want 200: %s", w.Code, w.Body.String())
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	testutil.ParseJSON(t, w, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancel response status = %q, want cancelled", cancelled.Status)
	}

	w = testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/ssm/webauthn/challenge/"+challengeID, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status after cancel = %d, want 200: %s", w.Code, w.Body.String())
	}
	var status struct {
		Status string `json:"status"`
	}
	testutil.ParseJSON(t, w, &status)
	if status.Status != "cancelled" {
		t.Fatalf("challenge status = %q, want cancelled", status.Status)
	}
}

func TestWebAuthn_CancelChallengeRejectsOtherUserAndApprovedChallenge(t *testing.T) {
	r, _, apiKey, db := setupWebAuthnRouterWithUserDB(t)
	if err := db.Create(&storage.Challenge{
		ChallengeID: "ch_cancel_foreign",
		UserID:      "other_user",
		Type:        "delete_wallet",
		Status:      "pending",
		PayloadJSON: "{}",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create foreign challenge: %v", err)
	}
	if err := db.Create(&storage.Challenge{
		ChallengeID: "ch_cancel_approved",
		UserID:      "test_user_" + strings.ReplaceAll(t.Name(), "/", "_"),
		Type:        "delete_wallet",
		Status:      "approved",
		PayloadJSON: "{}",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create approved challenge: %v", err)
	}

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/webauthn/challenge/ch_cancel_foreign/cancel", apiKey, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cancel foreign challenge = %d, want 404: %s", w.Code, w.Body.String())
	}
	w = testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/webauthn/challenge/ch_cancel_approved/cancel", apiKey, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("cancel approved challenge = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestWebAuthn_AuthenticatedChallengeStatusRejectsOtherUser(t *testing.T) {
	r, apiKey, db := setupWebAuthnRouterWithDB(t)
	if err := db.Create(&storage.Challenge{
		ChallengeID: "foreign_challenge",
		UserID:      "other_user",
		Type:        "sign",
		Status:      "pending",
		PayloadJSON: "{}",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/webauthn/challenge/foreign_challenge", apiKey, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestWebAuthn_ConfirmPage_MissingChallenge(t *testing.T) {
	r, _ := setupWebAuthnRouter(t)
	w := testutil.DoRequest(r, "POST", "/auth/confirm", nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400, body: %s", w.Code, w.Body.String())
	}
}
