// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package passkey_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func TestBeginAuthenticationIncludesAllowedCredentialIDs(t *testing.T) {
	server, db := newAuthenticationTestServer(t)
	userID := "user_allow_credentials"
	ch, err := server.CreateChallenge(userID, "policy_change", `{"operation":"policy_create"}`)
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	now := time.Now()
	for _, credID := range []string{"cred_allowed_1", "cred_allowed_2"} {
		if err := db.Create(&storage.WebAuthnCredential{
			CredentialID: credID,
			UserID:       userID,
			PublicKey:    []byte("test-public-key"),
			CreatedAt:    now,
			LastUsedAt:   now,
		}).Error; err != nil {
			t.Fatalf("create credential %s: %v", credID, err)
		}
	}
	if err := db.Create(&storage.WebAuthnCredential{
		CredentialID: "cred_other_user",
		UserID:       "other_user",
		PublicKey:    []byte("test-public-key"),
		CreatedAt:    now,
		LastUsedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create other user credential: %v", err)
	}

	opts, err := server.BeginAuthentication(userID, ch.ChallengeID)
	if err != nil {
		t.Fatalf("begin authentication: %v", err)
	}

	if len(opts.AllowCredentials) != 2 {
		t.Fatalf("allowCredentials length = %d, want 2: %#v", len(opts.AllowCredentials), opts.AllowCredentials)
	}
	want := map[string]bool{"cred_allowed_1": true, "cred_allowed_2": true}
	for _, credID := range opts.AllowCredentials {
		if !want[credID] {
			t.Fatalf("unexpected credential in allowCredentials: %q from %#v", credID, opts.AllowCredentials)
		}
		delete(want, credID)
	}
	if len(want) != 0 {
		t.Fatalf("missing credentials from allowCredentials: %#v", want)
	}
}

func TestBeginAuthenticationReusesExistingAuthChallenge(t *testing.T) {
	server, db := newAuthenticationTestServer(t)
	userID := "user_reuse_auth_challenge"
	ch, err := server.CreateChallenge(userID, "policy_change", `{"operation":"policy_create"}`)
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	if err := db.Create(&storage.WebAuthnCredential{
		CredentialID: "cred_reuse",
		UserID:       userID,
		PublicKey:    []byte("test-public-key"),
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatalf("create credential: %v", err)
	}

	first, err := server.BeginAuthentication(userID, ch.ChallengeID)
	if err != nil {
		t.Fatalf("first begin authentication: %v", err)
	}
	second, err := server.BeginAuthentication(userID, ch.ChallengeID)
	if err != nil {
		t.Fatalf("second begin authentication: %v", err)
	}

	if first.Challenge == "" {
		t.Fatal("first auth challenge is empty")
	}
	if second.Challenge != first.Challenge {
		t.Fatalf("auth challenge changed between calls: first=%q second=%q", first.Challenge, second.Challenge)
	}
}

func TestAuthenticationOptionsSerializesEmptyAllowCredentials(t *testing.T) {
	opts := passkey.AuthenticationOptions{
		Challenge:        "challenge",
		AllowCredentials: []string{},
		Timeout:          120000,
		UserVerification: "required",
		RPID:             "localhost",
	}
	data, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("marshal options: %v", err)
	}
	if !jsonContainsStringArray(data, "allowCredentials") {
		t.Fatalf("allowCredentials should serialize even when empty: %s", data)
	}
}

func TestBeginAuthenticationExpiresExpiredPendingChallenge(t *testing.T) {
	server, db := newAuthenticationTestServer(t)
	userID := "user_expired_auth_challenge"
	challengeID := "ch_expired_auth"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "policy_change",
		Status:      "pending",
		PayloadJSON: `{"challenge":"stale-challenge"}`,
		CreatedAt:   time.Now().Add(-2 * time.Minute),
		ExpiresAt:   time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	_, err := server.BeginAuthentication(userID, challengeID)
	if !errors.Is(err, passkey.ErrChallengeExpired) {
		t.Fatalf("BeginAuthentication error = %v, want ErrChallengeExpired", err)
	}

	var ch storage.Challenge
	if err := db.Where("challenge_id = ?", challengeID).First(&ch).Error; err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	if ch.Status != "timeout" {
		t.Fatalf("expired challenge status = %q, want timeout", ch.Status)
	}
}

func TestSummaryForChallengeIncludesSignDetails(t *testing.T) {
	payload, err := json.Marshal(map[string]interface{}{
		"payload": map[string]interface{}{
			"wallet_id":  "manual_wallet",
			"chain":      "ethereum",
			"to":         "0x1234567890123456789012345678901234567890",
			"amount_usd": 42.5,
			"tx_type":    "transfer",
			"reason":     "manual confirmation required",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	summary := passkey.SummaryForChallenge(storage.Challenge{
		ChallengeID: "ch_sign_summary",
		Type:        "sign",
		PayloadJSON: string(payload),
	})

	if summary.Operation != "sign" || summary.Title != "Approve signature" || summary.WalletID != "manual_wallet" {
		t.Fatalf("unexpected sign summary header: %+v", summary)
	}
	details := map[string]string{}
	for _, detail := range summary.Details {
		details[detail.Label] = detail.Value
	}
	for label, want := range map[string]string{
		"Wallet": "manual_wallet",
		"Chain":  "ethereum",
		"To":     "0x1234567890123456789012345678901234567890",
		"Type":   "transfer",
		"Reason": "manual confirmation required",
	} {
		if details[label] != want {
			t.Fatalf("summary detail %q = %q, want %q in %+v", label, details[label], want, summary.Details)
		}
	}
	if details["Amount USD"] != "42.50" {
		t.Fatalf("Amount USD = %q, want 42.50 in %+v", details["Amount USD"], summary.Details)
	}
}

func TestSummaryForChallengeIncludesSecurityWarnings(t *testing.T) {
	payload, err := json.Marshal(map[string]interface{}{
		"payload": map[string]interface{}{
			"wallet_id": "manual_wallet",
			"security": map[string]interface{}{
				"verdict": "warn",
				"checks": []map[string]interface{}{
					{
						"rule":   "usd_value_conservation",
						"status": "warn",
						"detail": "destination-chain receipt is not simulated",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	summary := passkey.SummaryForChallenge(storage.Challenge{
		ChallengeID: "ch_security_summary",
		Type:        "sign",
		PayloadJSON: string(payload),
	})

	details := map[string]string{}
	for _, detail := range summary.Details {
		details[detail.Label] = detail.Value
	}
	if details["Security verdict"] != "warn" {
		t.Fatalf("Security verdict = %q, want warn in %+v", details["Security verdict"], summary.Details)
	}
	if details["Security warning"] != "usd_value_conservation: destination-chain receipt is not simulated" {
		t.Fatalf("Security warning = %q in %+v", details["Security warning"], summary.Details)
	}
}

func newAuthenticationTestServer(t *testing.T) (*passkey.Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.AutoMigrate(&storage.Challenge{}, &storage.WebAuthnCredential{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	cfg := &config.WebAuthnConfig{
		RPID:                    "localhost",
		RPName:                  "test",
		RPOrigin:                "http://localhost:8080",
		ChallengeTimeoutSeconds: 120,
	}
	return passkey.NewServer(db, cfg), db
}

func jsonContainsStringArray(data []byte, key string) bool {
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	_, ok := payload[key].([]interface{})
	return ok
}
