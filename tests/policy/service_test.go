// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy_test

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestServiceCurrentPolicyRequiresBoundInstance(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "policyless_wallet", "0x1234567890123456789012345678901234567890")

	_, err := policy.NewService(db).CurrentPolicy(userID, "policyless_wallet", time.Now())
	if !errors.Is(err, policy.ErrNoPolicy) {
		t.Fatalf("CurrentPolicy error = %v, want missing instance error", err)
	}
}

func TestServiceEnsureDefaultPolicyBindsDefaultInstance(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "default_wallet", "0x1234567890123456789012345678901234567890")

	current, err := policy.NewService(db).EnsureDefaultPolicy(userID, "default_wallet")
	if err != nil {
		t.Fatalf("EnsureDefaultPolicy: %v", err)
	}
	if current.PolicyID == "" || current.WalletID != "default_wallet" || current.Name != "default" || current.Mode != "manual" || current.AutoSignEnabled {
		t.Fatalf("unexpected default policy: %+v", current)
	}

	var instances int64
	db.Model(&storage.PolicyInstance{}).Where("wallet_id = ? AND user_id = ?", "default_wallet", userID).Count(&instances)
	if instances != 1 {
		t.Fatalf("policy instance count = %d, want 1", instances)
	}
}

func TestServiceRecordSignedOperationDoesNotIncrementSubmittedUsage(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "usage_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "usage_wallet", policy.DefinitionInput{Name: "default", Mode: "manual"})

	err := policy.NewService(db).RecordSignedOperation(policy.SignedOperation{
		UserID: userID, WalletID: "usage_wallet", PolicyID: current.PolicyID,
		TxHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
		Chain:  "ethereum", TxType: "transfer", AmountUSD: usd("42.5"),
		Decision: policy.DecisionWebAuthn,
	})
	if err != nil {
		t.Fatalf("RecordSignedOperation: %v", err)
	}

	var updated storage.PolicyInstance
	if err := db.First(&updated, "policy_instance_id = ?", current.PolicyID).Error; err != nil {
		t.Fatalf("load policy instance: %v", err)
	}
	if updated.DailyTransferUsedUSD.String() != "0" || updated.AutoSignDailyUsedUSD.String() != "0" || updated.AutoSignCountToday != 0 {
		t.Fatalf("signed operation should not count as submitted usage yet: %+v", updated)
	}

	usage, err := policy.NewService(db).UsageForPolicy(current.PolicyID, time.Now())
	if err != nil {
		t.Fatalf("UsageForPolicy: %v", err)
	}
	if usage.PendingTransferUSD.String() != "42.5" || usage.SubmittedTransferUSD.String() != "0" {
		t.Fatalf("unexpected usage snapshot: %+v", usage)
	}
}

func TestServiceRecordSignedOperationRejectsConcurrentAutoLimitOverrun(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "auto_limit_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "auto_limit_wallet", policy.DefinitionInput{
		Name:                  "auto limited",
		Mode:                  "auto",
		AutoSignEnabled:       true,
		AutoSignMaxPerTxUSD:   usd("100"),
		AutoSignDailyLimitUSD: usd("15"),
		DailyTransferLimitUSD: usd("100"),
	})
	svc := policy.NewService(db)
	first := policy.SignedOperation{
		UserID: userID, WalletID: "auto_limit_wallet", PolicyID: current.PolicyID,
		TxHash: "0x6666666666666666666666666666666666666666666666666666666666666666",
		Chain:  "ethereum", TxType: "transfer", AmountUSD: usd("10"), Decision: policy.DecisionAuto,
	}
	if err := svc.RecordSignedOperation(first); err != nil {
		t.Fatalf("first RecordSignedOperation: %v", err)
	}
	second := first
	second.TxHash = "0x7777777777777777777777777777777777777777777777777777777777777777"
	err := svc.RecordSignedOperation(second)
	if !errors.Is(err, policy.ErrPolicyBlocked) {
		t.Fatalf("second RecordSignedOperation error = %v, want ErrPolicyBlocked", err)
	}

	var events int64
	if err := db.Model(&storage.PolicyUsageEvent{}).Where("policy_id = ?", current.PolicyID).Count(&events).Error; err != nil {
		t.Fatalf("count policy events: %v", err)
	}
	if events != 1 {
		t.Fatalf("policy events = %d, want only first reservation", events)
	}
}

func TestServiceEvaluateUsesDecimalMoneyForDailyLimits(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "decimal_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "decimal_wallet", policy.DefinitionInput{
		Name:                  "decimal limited",
		Mode:                  "manual",
		DailyTransferLimitUSD: usd("0.3"),
	})
	today := time.Now().UTC().Format("2006-01-02")
	if err := db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", current.PolicyID).Updates(map[string]interface{}{
		"daily_transfer_used_usd": usd("0.1"),
		"last_reset_date":         today,
	}).Error; err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	result, err := policy.NewService(db).Evaluate(policy.Request{
		UserID: userID, WalletID: "decimal_wallet", TxType: "transfer", AmountUSD: usd("0.2"),
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("Evaluate should not reject decimal-equivalent limit: %v", err)
	}
	if result.Decision != policy.DecisionWebAuthn {
		t.Fatalf("decision = %s, want webauthn for manual policy", result.Decision)
	}
}

func TestServiceRecordSignedOperationUsesDecimalMoneyForPendingLimits(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "decimal_pending_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "decimal_pending_wallet", policy.DefinitionInput{
		Name:                  "decimal auto",
		Mode:                  "auto",
		AutoSignEnabled:       true,
		AutoSignMaxPerTxUSD:   usd("1"),
		AutoSignDailyLimitUSD: usd("0.3"),
	})
	svc := policy.NewService(db)
	first := policy.SignedOperation{
		UserID: userID, WalletID: "decimal_pending_wallet", PolicyID: current.PolicyID,
		TxHash: "0x8888888888888888888888888888888888888888888888888888888888888888",
		Chain:  "ethereum", TxType: "transfer", AmountUSD: usd("0.1"), Decision: policy.DecisionAuto,
	}
	if err := svc.RecordSignedOperation(first); err != nil {
		t.Fatalf("first RecordSignedOperation: %v", err)
	}
	second := first
	second.TxHash = "0x9999999999999999999999999999999999999999999999999999999999999999"
	second.AmountUSD = usd("0.2")
	if err := svc.RecordSignedOperation(second); err != nil {
		t.Fatalf("second RecordSignedOperation should not reject decimal-equivalent limit: %v", err)
	}
}

func TestServiceMarkOperationSubmittedIncrementsSubmittedUsageOnce(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "submit_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "submit_wallet", policy.DefinitionInput{
		Name: "auto", Mode: "auto", AutoSignEnabled: true,
	})
	svc := policy.NewService(db)
	op := policy.SignedOperation{
		UserID: userID, WalletID: "submit_wallet", PolicyID: current.PolicyID,
		TxHash: "0x2222222222222222222222222222222222222222222222222222222222222222",
		Chain:  "ethereum", TxType: "swap", AmountUSD: usd("10"),
		Decision: policy.DecisionAuto,
	}
	if err := svc.RecordSignedOperation(op); err != nil {
		t.Fatalf("RecordSignedOperation: %v", err)
	}
	if _, err := svc.MarkOperationSubmitted(userID, op.TxHash, "ethereum"); err != nil {
		t.Fatalf("MarkOperationSubmitted: %v", err)
	}
	if _, err := svc.MarkOperationSubmitted(userID, op.TxHash, "ethereum"); err != nil {
		t.Fatalf("second MarkOperationSubmitted: %v", err)
	}

	usage, err := svc.UsageForPolicy(current.PolicyID, time.Now())
	if err != nil {
		t.Fatalf("UsageForPolicy: %v", err)
	}
	if usage.SubmittedTradeUSD.String() != "10" || usage.PendingTradeUSD.String() != "0" || usage.AutoSignSubmittedUSD.String() != "10" || usage.AutoSignSubmittedCount != 1 {
		t.Fatalf("unexpected submitted usage: %+v", usage)
	}
}

func TestServiceMarkOperationSubmittedUpdatesDecimalUsage(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "submit_decimal_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "submit_decimal_wallet", policy.DefinitionInput{
		Name:                  "auto decimal",
		Mode:                  "auto",
		AutoSignEnabled:       true,
		AutoSignMaxPerTxUSD:   usd("1"),
		AutoSignDailyLimitUSD: usd("1"),
	})
	svc := policy.NewService(db)
	op := policy.SignedOperation{
		UserID: userID, WalletID: "submit_decimal_wallet", PolicyID: current.PolicyID,
		TxHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Chain:  "ethereum", TxType: "transfer", AmountUSD: usd("0.3"),
		Decision: policy.DecisionAuto,
	}
	if err := svc.RecordSignedOperation(op); err != nil {
		t.Fatalf("RecordSignedOperation: %v", err)
	}
	if _, err := svc.MarkOperationSubmitted(userID, op.TxHash, "ethereum"); err != nil {
		t.Fatalf("MarkOperationSubmitted: %v", err)
	}
	var instance storage.PolicyInstance
	if err := db.First(&instance, "policy_instance_id = ?", current.PolicyID).Error; err != nil {
		t.Fatalf("load policy instance: %v", err)
	}
	if instance.DailyTransferUsedUSD.String() != "0.3" || instance.AutoSignDailyUsedUSD.String() != "0.3" {
		t.Fatalf("usage money = %+v", instance)
	}
}

func TestServiceMarkOperationSubmittedResetsStaleUsageBeforeIncrement(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "stale_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "stale_wallet", policy.DefinitionInput{Name: "default", Mode: "manual"})
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if err := db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", current.PolicyID).Updates(map[string]interface{}{
		"daily_transfer_used_usd":  usd("100"),
		"daily_trade_used_usd":     usd("75"),
		"auto_sign_daily_used_usd": usd("50"),
		"auto_sign_count_today":    3,
		"last_reset_date":          yesterday,
	}).Error; err != nil {
		t.Fatalf("seed stale usage: %v", err)
	}
	svc := policy.NewService(db)
	op := policy.SignedOperation{
		UserID: userID, WalletID: "stale_wallet", PolicyID: current.PolicyID,
		TxHash: "0x3333333333333333333333333333333333333333333333333333333333333333",
		Chain:  "ethereum", TxType: "transfer", AmountUSD: usd("10"),
		Decision: policy.DecisionWebAuthn,
	}
	if err := svc.RecordSignedOperation(op); err != nil {
		t.Fatalf("RecordSignedOperation: %v", err)
	}
	if _, err := svc.MarkOperationSubmitted(userID, op.TxHash, "ethereum"); err != nil {
		t.Fatalf("MarkOperationSubmitted: %v", err)
	}

	var updated storage.PolicyInstance
	if err := db.First(&updated, "policy_instance_id = ?", current.PolicyID).Error; err != nil {
		t.Fatalf("load policy instance: %v", err)
	}
	if updated.DailyTransferUsedUSD.String() != "10" || updated.DailyTradeUsedUSD.String() != "0" || updated.AutoSignDailyUsedUSD.String() != "0" || updated.AutoSignCountToday != 0 {
		t.Fatalf("stale usage was not reset before increment: %+v", updated)
	}
}

func TestServiceUsageForPolicyIgnoresExpiredSignedOperations(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "expired_usage_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "expired_usage_wallet", policy.DefinitionInput{Name: "default", Mode: "manual"})
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Minute)
	if err := db.Create(&storage.PolicyUsageEvent{
		EventID: "pue_expired_usage", UserID: userID, WalletID: "expired_usage_wallet", PolicyID: current.PolicyID,
		Chain: "ethereum", TxHash: "0x4444444444444444444444444444444444444444444444444444444444444444",
		TxType: "transfer", AmountUSD: usd("25"), Decision: string(policy.DecisionWebAuthn),
		Status: policy.StatusSigned, UsageDate: now.Format("2006-01-02"), ExpiresAt: &expiredAt,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("create policy usage event: %v", err)
	}

	usage, err := policy.NewService(db).UsageForPolicy(current.PolicyID, now)
	if err != nil {
		t.Fatalf("UsageForPolicy: %v", err)
	}
	if usage.PendingTransferUSD.String() != "0" {
		t.Fatalf("expired signed operation should not count as pending usage: %+v", usage)
	}
}

func TestServiceSignedOperationForBroadcastExpiresOldSignedOperation(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "expired_broadcast_wallet", "0x1234567890123456789012345678901234567890")
	current := bindPolicyInstance(t, db, userID, "expired_broadcast_wallet", policy.DefinitionInput{Name: "default", Mode: "manual"})
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Minute)
	txHash := "0x5555555555555555555555555555555555555555555555555555555555555555"
	if err := db.Create(&storage.PolicyUsageEvent{
		EventID: "pue_expired_broadcast", UserID: userID, WalletID: "expired_broadcast_wallet", PolicyID: current.PolicyID,
		Chain: "ethereum", TxHash: txHash, TxType: "transfer", AmountUSD: usd("25"),
		Decision: string(policy.DecisionWebAuthn), Status: policy.StatusSigned,
		UsageDate: now.Format("2006-01-02"), ExpiresAt: &expiredAt,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("create policy usage event: %v", err)
	}

	_, err := policy.NewService(db).SignedOperationForBroadcast(userID, txHash, "ethereum")
	if !errors.Is(err, policy.ErrSignedOperationNotFound) {
		t.Fatalf("SignedOperationForBroadcast error = %v, want ErrSignedOperationNotFound", err)
	}
	var event storage.PolicyUsageEvent
	if err := db.First(&event, "event_id = ?", "pue_expired_broadcast").Error; err != nil {
		t.Fatalf("load event: %v", err)
	}
	if event.Status != policy.StatusExpired {
		t.Fatalf("expired event status = %s, want %s", event.Status, policy.StatusExpired)
	}
}

func usd(raw string) money.Amount {
	return money.MustParse(raw)
}

func TestServiceEvaluateRecipientWhitelistOnlyAppliesToTransfer(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wl_wallet", "0x1234567890123456789012345678901234567890")
	bindPolicyInstance(t, db, userID, "wl_wallet", policy.DefinitionInput{
		Name:                      "wl",
		Mode:                      "auto",
		AutoSignEnabled:           true,
		AutoSignMaxPerTxUSD:       usd("100"),
		AutoSignDailyLimitUSD:     usd("1000"),
		RecipientWhitelistEnabled: true,
		RecipientWhitelistAddrs:   []string{"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	})

	svc := policy.NewService(db)
	strangerRecipient := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// transfer: recipient NOT in whitelist → must fall back to WebAuthn with the
	// "recipient not whitelisted" reason. This is the gate the policy is for.
	result, err := svc.Evaluate(policy.Request{
		UserID: userID, WalletID: "wl_wallet", TxType: "transfer",
		AmountUSD: usd("1"), Recipient: strangerRecipient, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("transfer Evaluate err = %v", err)
	}
	if result.Decision != policy.DecisionWebAuthn || result.Reason != "recipient not whitelisted" {
		t.Fatalf("transfer with non-whitelisted recipient: got decision=%s reason=%q, want webauthn / recipient not whitelisted",
			result.Decision, result.Reason)
	}

	// swap: same non-whitelisted recipient must NOT trigger the whitelist rule.
	// The recipient field for swap is the DEX router; whitelist semantics don't
	// apply. Should reach DecisionAuto (all other auto-sign checks pass).
	result, err = svc.Evaluate(policy.Request{
		UserID: userID, WalletID: "wl_wallet", TxType: "swap",
		AmountUSD: usd("1"), Recipient: strangerRecipient, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("swap Evaluate err = %v", err)
	}
	if result.Decision != policy.DecisionAuto {
		t.Fatalf("swap should auto-sign (whitelist not applicable): got decision=%s reason=%q",
			result.Decision, result.Reason)
	}

	// bridge: same — funds always land at the source wallet on the destination
	// chain, so the whitelist rule does not apply.
	result, err = svc.Evaluate(policy.Request{
		UserID: userID, WalletID: "wl_wallet", TxType: "bridge",
		AmountUSD: usd("1"), Recipient: strangerRecipient, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("bridge Evaluate err = %v", err)
	}
	if result.Decision != policy.DecisionAuto {
		t.Fatalf("bridge should auto-sign (whitelist not applicable): got decision=%s reason=%q",
			result.Decision, result.Reason)
	}
}

func bindPolicyInstance(t *testing.T, db *gorm.DB, userID, walletID string, input policy.DefinitionInput) *policy.Policy {
	t.Helper()
	svc := policy.NewService(db)
	def, err := svc.CreatePolicyDefinition(userID, input)
	if err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}
	if _, err := svc.BindPolicyDefinition(userID, walletID, def.PolicyDefinitionID); err != nil {
		t.Fatalf("BindPolicyDefinition: %v", err)
	}
	current, err := svc.CurrentPolicy(userID, walletID, time.Now())
	if err != nil {
		t.Fatalf("CurrentPolicy: %v", err)
	}
	return current
}
