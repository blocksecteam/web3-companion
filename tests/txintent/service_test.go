// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package txintent_test

import (
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestServiceCreateIntentSetsTTLAndPayloadHash(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }

	intent, err := svc.Create(t.Context(), txintent.CreateRequest{
		UserID:      "user_1",
		WalletID:    "wallet_1",
		WalletUID:   "wuid_wallet_1",
		Chain:       "ethereum",
		ChainID:     1,
		TxType:      "transfer",
		Owner:       "0x1111111111111111111111111111111111111111",
		To:          "0x2222222222222222222222222222222222222222",
		Value:       "100",
		Data:        "0x",
		GasLimit:    21000,
		Recipient:   "0x2222222222222222222222222222222222222222",
		SummaryJSON: `{"type":"transfer"}`,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if intent.TransactionID == "" {
		t.Fatal("TransactionID is empty")
	}
	if intent.Status != txintent.StatusBuilt {
		t.Fatalf("Status = %q, want %q", intent.Status, txintent.StatusBuilt)
	}
	if !intent.ExpiresAt.Equal(now.Add(txintent.DefaultTTL)) {
		t.Fatalf("ExpiresAt = %s, want %s", intent.ExpiresAt, now.Add(txintent.DefaultTTL))
	}
	if intent.PayloadHash == "" {
		t.Fatal("PayloadHash is empty")
	}
	if intent.PayloadHash != txintent.IntentPayloadHash(intent) {
		t.Fatalf("PayloadHash = %q, want recomputed %q", intent.PayloadHash, txintent.IntentPayloadHash(intent))
	}
}

func TestServiceSecurityTransitions(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	intent := createTestIntent(t, svc)

	passed, err := svc.RecordSecurityResult(t.Context(), intent.TransactionID, "user_1", txintent.SecurityResult{
		Verdict: "pass",
		JSON:    `{"verdict":"pass"}`,
	})
	if err != nil {
		t.Fatalf("RecordSecurityResult(pass) error = %v", err)
	}
	if passed.Status != txintent.StatusSecurityPassed {
		t.Fatalf("pass status = %q, want %q", passed.Status, txintent.StatusSecurityPassed)
	}
	if !txintent.SecurityAllowsSigning(passed) {
		t.Fatal("pass should allow signing")
	}

	warnIntent := createTestIntentWithID(t, svc, "wallet_warn")
	warned, err := svc.RecordSecurityResult(t.Context(), warnIntent.TransactionID, "user_1", txintent.SecurityResult{
		Verdict: "warn",
		JSON:    `{"verdict":"warn"}`,
	})
	if err != nil {
		t.Fatalf("RecordSecurityResult(warn) error = %v", err)
	}
	if warned.Status != txintent.StatusSecurityReviewRequired {
		t.Fatalf("warn status = %q, want %q", warned.Status, txintent.StatusSecurityReviewRequired)
	}
	if txintent.SecurityAllowsSigning(warned) {
		t.Fatal("warn should not allow signing")
	}
	cleared, err := svc.ClearSecurityReview(t.Context(), warnIntent.TransactionID, "user_1", txintent.SecurityClearance{
		Reviewer:   "web3_security_agent",
		Decision:   "safe",
		Confidence: "medium",
		Reason:     "warning reviewed by security agent",
	})
	if err != nil {
		t.Fatalf("ClearSecurityReview() error = %v", err)
	}
	if cleared.Status != txintent.StatusSecurityPassed || cleared.SecurityConfirmedAt == nil {
		t.Fatalf("cleared warning not signable: status=%q confirmed_at=%v", cleared.Status, cleared.SecurityConfirmedAt)
	}
	if !txintent.SecurityAllowsSigning(cleared) {
		t.Fatal("cleared warning should allow signing")
	}

	failIntent := createTestIntentWithID(t, svc, "wallet_fail")
	failed, err := svc.RecordSecurityResult(t.Context(), failIntent.TransactionID, "user_1", txintent.SecurityResult{
		Verdict: "fail",
		JSON:    `{"verdict":"fail"}`,
	})
	if err != nil {
		t.Fatalf("RecordSecurityResult(fail) error = %v", err)
	}
	if failed.Status != txintent.StatusSecurityReviewRequired {
		t.Fatalf("fail status = %q, want %q", failed.Status, txintent.StatusSecurityReviewRequired)
	}
	if txintent.SecurityAllowsSigning(failed) {
		t.Fatal("fail should not allow signing")
	}
	failCleared, err := svc.ClearSecurityReview(t.Context(), failIntent.TransactionID, "user_1", txintent.SecurityClearance{
		Reviewer:   "web3_security_agent",
		Decision:   "safe",
		Confidence: "high",
		Reason:     "agent accepted simulation failure after review",
	})
	if err != nil {
		t.Fatalf("ClearSecurityReview(fail) error = %v", err)
	}
	if !txintent.SecurityAllowsSigning(failCleared) {
		t.Fatal("agent-cleared fail should allow signing")
	}

	lowConfidenceIntent := createTestIntentWithID(t, svc, "wallet_low_confidence")
	if _, err := svc.RecordSecurityResult(t.Context(), lowConfidenceIntent.TransactionID, "user_1", txintent.SecurityResult{
		Verdict: "warn",
		JSON:    `{"verdict":"warn"}`,
	}); err != nil {
		t.Fatalf("RecordSecurityResult(low confidence warn) error = %v", err)
	}
	if _, err := svc.ClearSecurityReview(t.Context(), lowConfidenceIntent.TransactionID, "user_1", txintent.SecurityClearance{
		Reviewer:   "web3_security_agent",
		Decision:   "safe",
		Confidence: "low",
	}); err != txintent.ErrSecurityNotSatisfied {
		t.Fatalf("low confidence safe error = %v, want %v", err, txintent.ErrSecurityNotSatisfied)
	}
}

func TestServiceEncryptsAndDecryptsSignedTransaction(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	intent := createTestIntent(t, svc)
	intent.Status = txintent.StatusSecurityPassed
	intent.SecurityVerdict = "pass"
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save security status: %v", err)
	}

	signed, err := svc.StoreSignedTx(t.Context(), intent.TransactionID, "user_1", txintent.SignedTx{
		Hex:            "0x02f8signed",
		Hash:           "0xabc",
		PolicyID:       "pol_1",
		PolicyDecision: "auto",
	})
	if err != nil {
		t.Fatalf("StoreSignedTx() error = %v", err)
	}
	if signed.Status != txintent.StatusSigned {
		t.Fatalf("Status = %q, want %q", signed.Status, txintent.StatusSigned)
	}
	if signed.SignedTxEncrypted == nil || signed.SignedTxDataKey == nil {
		t.Fatal("signed tx encryption fields were not stored")
	}
	if string(signed.SignedTxEncrypted) == "0x02f8signed" {
		t.Fatal("signed tx was stored in plaintext")
	}

	hex, err := svc.DecryptSignedTx(t.Context(), signed)
	if err != nil {
		t.Fatalf("DecryptSignedTx() error = %v", err)
	}
	if hex != "0x02f8signed" {
		t.Fatalf("decrypted signed tx = %q", hex)
	}
}

func TestServiceRejectsSecurityResultAfterSigning(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	intent := createTestIntent(t, svc)
	intent.Status = txintent.StatusSecurityPassed
	intent.SecurityVerdict = "pass"
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save security status: %v", err)
	}
	signed, err := svc.StoreSignedTx(t.Context(), intent.TransactionID, "user_1", txintent.SignedTx{
		Hex:            "0x02f8signed",
		Hash:           "0xabc",
		PolicyID:       "pol_1",
		PolicyDecision: "auto",
	})
	if err != nil {
		t.Fatalf("StoreSignedTx() error = %v", err)
	}

	_, err = svc.RecordSecurityResult(t.Context(), intent.TransactionID, "user_1", txintent.SecurityResult{
		Verdict: "pass",
		JSON:    `{"verdict":"pass"}`,
	})
	if err != txintent.ErrInvalidTransition {
		t.Fatalf("RecordSecurityResult() error = %v, want %v", err, txintent.ErrInvalidTransition)
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", signed.TransactionID).Error; err != nil {
		t.Fatalf("reload intent: %v", err)
	}
	if stored.Status != txintent.StatusSigned {
		t.Fatalf("status after rejected security result = %q, want %q", stored.Status, txintent.StatusSigned)
	}
}

func TestServiceRejectsStoreSignedTxFromInvalidStatus(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	intent := createTestIntent(t, svc)

	_, err := svc.StoreSignedTx(t.Context(), intent.TransactionID, "user_1", txintent.SignedTx{
		Hex:            "0x02f8signed",
		Hash:           "0xabc",
		PolicyID:       "pol_1",
		PolicyDecision: "auto",
	})
	if err != txintent.ErrInvalidTransition {
		t.Fatalf("StoreSignedTx() error = %v, want %v", err, txintent.ErrInvalidTransition)
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("reload intent: %v", err)
	}
	if stored.Status != txintent.StatusBuilt || stored.TxHash != "" || len(stored.SignedTxEncrypted) != 0 {
		t.Fatalf("invalid StoreSignedTx mutated intent: %+v", stored)
	}
}

func TestServiceRejectsMarkSubmittedFromInvalidStatus(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	intent := createTestIntent(t, svc)
	intent.Status = txintent.StatusSecurityPassed
	intent.SecurityVerdict = "pass"
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save intent: %v", err)
	}

	_, err := svc.MarkSubmitted(t.Context(), intent.TransactionID, "user_1")
	if err != txintent.ErrInvalidTransition {
		t.Fatalf("MarkSubmitted() error = %v, want %v", err, txintent.ErrInvalidTransition)
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("reload intent: %v", err)
	}
	if stored.Status != txintent.StatusSecurityPassed || stored.SubmittedAt != nil {
		t.Fatalf("invalid MarkSubmitted mutated intent: %+v", stored)
	}
}

func TestServiceRejectsExpiredIntentForSigning(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := txintent.NewService(db, vault.NewTestKMSClient())
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	intent := createTestIntent(t, svc)
	intent.ExpiresAt = now.Add(-time.Second)
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save expired intent: %v", err)
	}

	_, err := svc.LoadForSigning(t.Context(), intent.TransactionID, "user_1")
	if err == nil {
		t.Fatal("LoadForSigning() expected expired error")
	}
	if err != txintent.ErrIntentExpired {
		t.Fatalf("LoadForSigning() error = %v, want %v", err, txintent.ErrIntentExpired)
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("reload intent: %v", err)
	}
	if stored.Status != txintent.StatusExpired {
		t.Fatalf("expired status = %q, want %q", stored.Status, txintent.StatusExpired)
	}
}

// Intents already in broadcasting/submitted must not be flipped to expired
// by a late call to LoadForBroadcast (or any other ensureNotExpired path).
// The on-chain tx is what's authoritative; rewriting status to expired
// would also defeat MarkSubmittedByHash drift recovery, which filters on
// status="broadcasting".
func TestServicePreservesBroadcastingAndSubmittedAcrossExpiry(t *testing.T) {
	for _, status := range []string{txintent.StatusBroadcasting, txintent.StatusSubmitted} {
		t.Run(status, func(t *testing.T) {
			db := testutil.NewTestDB(t)
			svc := txintent.NewService(db, vault.NewTestKMSClient())
			now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
			svc.Now = func() time.Time { return now }
			intent := createTestIntent(t, svc)
			intent.Status = status
			intent.ExpiresAt = now.Add(-time.Second)
			if err := db.Save(&intent).Error; err != nil {
				t.Fatalf("save %s expired intent: %v", status, err)
			}

			_, err := svc.LoadForBroadcast(t.Context(), intent.TransactionID, "user_1")
			if err != txintent.ErrIntentExpired {
				t.Fatalf("LoadForBroadcast() error = %v, want ErrIntentExpired", err)
			}

			var stored storage.TransactionIntent
			if err := db.First(&stored, "transaction_id = ?", intent.TransactionID).Error; err != nil {
				t.Fatalf("reload intent: %v", err)
			}
			if stored.Status != status {
				t.Fatalf("status was clobbered: got %q, want %q (ensureNotExpired must not rewrite on-chain states)", stored.Status, status)
			}
		})
	}
}

func createTestIntent(t *testing.T, svc *txintent.Service) storage.TransactionIntent {
	t.Helper()
	return createTestIntentWithID(t, svc, "wallet_1")
}

func createTestIntentWithID(t *testing.T, svc *txintent.Service, walletID string) storage.TransactionIntent {
	t.Helper()
	intent, err := svc.Create(t.Context(), txintent.CreateRequest{
		UserID:    "user_1",
		WalletID:  walletID,
		WalletUID: "wuid_" + walletID,
		Chain:     "ethereum",
		ChainID:   1,
		TxType:    "transfer",
		Owner:     "0x1111111111111111111111111111111111111111",
		To:        "0x2222222222222222222222222222222222222222",
		Value:     "0",
		Data:      "0x",
		GasLimit:  21000,
		Recipient: "0x2222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return intent
}
