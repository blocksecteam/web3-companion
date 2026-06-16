// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm

import (
	"testing"
	"time"

	"gorm.io/gorm"

	basepolicy "github.com/blocksecteam/web3-companion/internal/policy"
	policyengine "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestEvaluate_ZeroAmountPriceKnown_AutoSigns(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "w1", "0xabc")
	bindAutoPolicy(t, db, userID, "w1")

	result, err := policyengine.NewEngine(db).Evaluate(policyengine.Request{
		UserID:       userID,
		WalletID:     "w1",
		TxType:       "trade",
		AmountUSD:    usd("0"),
		PriceUnknown: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != policyengine.DecisionAuto {
		t.Errorf("expected DecisionAuto for zero-value tx with known price, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEvaluate_PriceUnknownTrue_ForcesWebAuthn(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "w1", "0xabc")
	bindAutoPolicy(t, db, userID, "w1")

	result, err := policyengine.NewEngine(db).Evaluate(policyengine.Request{
		UserID:       userID,
		WalletID:     "w1",
		TxType:       "trade",
		AmountUSD:    usd("0"),
		PriceUnknown: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != policyengine.DecisionWebAuthn {
		t.Errorf("expected DecisionWebAuthn for unknown price, got %s", result.Decision)
	}
	if result.Reason != "token price unknown" {
		t.Errorf("expected reason 'token price unknown', got %q", result.Reason)
	}
}

func TestRecordUserSign_DoesNotUpdateAutoSignCounters(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "w1", "0xabc")
	pol := bindAutoPolicy(t, db, userID, "w1")
	today := time.Now().UTC().Format("2006-01-02")
	if err := db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", pol.PolicyID).Updates(map[string]interface{}{
		"auto_sign_daily_used_usd": usd("500"),
		"auto_sign_count_today":    3,
		"last_reset_date":          today,
	}).Error; err != nil {
		t.Fatalf("seed policy instance: %v", err)
	}
	pol.AutoSignDailyUsedUSD = usd("500")
	pol.AutoSignCountToday = 3
	pol.LastResetDate = today

	if err := policyengine.NewEngine(db).RecordUserSign(pol, "transfer", usd("100"), time.Now()); err != nil {
		t.Fatalf("RecordUserSign: %v", err)
	}

	var updated storage.PolicyInstance
	db.First(&updated, "policy_instance_id = ?", pol.PolicyID)
	if updated.AutoSignDailyUsedUSD.String() != "500" {
		t.Errorf("auto_sign_daily_used_usd should be unchanged (500), got %v", updated.AutoSignDailyUsedUSD)
	}
	if updated.AutoSignCountToday != 3 {
		t.Errorf("auto_sign_count_today should be unchanged (3), got %v", updated.AutoSignCountToday)
	}
	if updated.DailyTransferUsedUSD.String() != "100" {
		t.Errorf("daily_transfer_used_usd should be 100, got %v", updated.DailyTransferUsedUSD)
	}
}

func TestRecordAutoSign_UpdatesAllCounters(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "w2", "0xdef")
	pol := bindAutoPolicy(t, db, userID, "w2")
	today := time.Now().UTC().Format("2006-01-02")
	if err := db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", pol.PolicyID).Updates(map[string]interface{}{
		"auto_sign_daily_used_usd": usd("200"),
		"auto_sign_count_today":    2,
		"last_reset_date":          today,
	}).Error; err != nil {
		t.Fatalf("seed policy instance: %v", err)
	}
	pol.AutoSignDailyUsedUSD = usd("200")
	pol.AutoSignCountToday = 2
	pol.LastResetDate = today

	if err := policyengine.NewEngine(db).RecordAutoSign(pol, "trade", usd("50"), time.Now()); err != nil {
		t.Fatalf("RecordAutoSign: %v", err)
	}

	var updated storage.PolicyInstance
	db.First(&updated, "policy_instance_id = ?", pol.PolicyID)
	if updated.AutoSignDailyUsedUSD.String() != "250" {
		t.Errorf("auto_sign_daily_used_usd should be 250, got %v", updated.AutoSignDailyUsedUSD)
	}
	if updated.AutoSignCountToday != 3 {
		t.Errorf("auto_sign_count_today should be 3, got %v", updated.AutoSignCountToday)
	}
	if updated.DailyTradeUsedUSD.String() != "50" {
		t.Errorf("daily_trade_used_usd should be 50, got %v", updated.DailyTradeUsedUSD)
	}
}

func bindAutoPolicy(t *testing.T, db *gorm.DB, userID, walletID string) *basepolicy.Policy {
	t.Helper()
	svc := basepolicy.NewService(db)
	def, err := svc.CreatePolicyDefinition(userID, basepolicy.DefinitionInput{
		Name:                  "test",
		Mode:                  "auto",
		AutoSignEnabled:       true,
		AutoSignMaxPerTxUSD:   usd("500"),
		AutoSignDailyLimitUSD: usd("1000"),
	})
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
