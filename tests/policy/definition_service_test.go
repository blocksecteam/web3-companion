// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy_test

import (
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestServiceEnsureUserDefaultPolicyCreatesManualDefaultDefinition(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)

	def, err := policy.NewService(db).EnsureUserDefaultPolicy(userID)
	if err != nil {
		t.Fatalf("EnsureUserDefaultPolicy: %v", err)
	}
	if def.UserID != userID || def.Name != "default" || !def.IsDefault || def.DeletedAt != nil {
		t.Fatalf("unexpected default definition: %+v", def)
	}

	var version storage.PolicyDefinitionVersion
	if err := db.First(&version, "policy_definition_version_id = ?", def.CurrentVersionID).Error; err != nil {
		t.Fatalf("load current version: %v", err)
	}
	if version.Mode != "manual" || version.AutoSignEnabled {
		t.Fatalf("default version should be manual with auto-sign disabled: %+v", version)
	}
}

func TestServiceBindDefaultPolicyCreatesWalletInstance(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_with_default", "0x1234567890123456789012345678901234567890")

	instance, err := policy.NewService(db).BindDefaultPolicy(userID, "wallet_with_default")
	if err != nil {
		t.Fatalf("BindDefaultPolicy: %v", err)
	}
	if instance.WalletID != "wallet_with_default" || instance.PolicyDefinitionID == "" || instance.PolicyDefinitionVersionID == "" {
		t.Fatalf("unexpected instance: %+v", instance)
	}

	current, err := policy.NewService(db).CurrentPolicy(userID, "wallet_with_default", time.Now())
	if err != nil {
		t.Fatalf("CurrentPolicy: %v", err)
	}
	if current.PolicyID != instance.PolicyInstanceID || current.Name != "default" || current.Mode != "manual" || current.AutoSignEnabled {
		t.Fatalf("CurrentPolicy should resolve bound default instance: %+v", current)
	}
}

func TestServiceDeleteDefinitionRejectsDefaultAndHidesDeletedDefinitions(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := policy.NewService(db)

	def, err := svc.EnsureUserDefaultPolicy(userID)
	if err != nil {
		t.Fatalf("EnsureUserDefaultPolicy: %v", err)
	}
	if err := svc.DeletePolicyDefinition(userID, def.PolicyDefinitionID); err != policy.ErrDefaultPolicyDeleteBlocked {
		t.Fatalf("delete default error = %v, want ErrDefaultPolicyDeleteBlocked", err)
	}

	custom, err := svc.CreatePolicyDefinition(userID, policy.DefinitionInput{Name: "short leash", Mode: "manual"})
	if err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}
	if err := svc.DeletePolicyDefinition(userID, custom.PolicyDefinitionID); err != nil {
		t.Fatalf("DeletePolicyDefinition: %v", err)
	}

	defs, err := svc.ListPolicyDefinitions(userID, 0, 0)
	if err != nil {
		t.Fatalf("ListPolicyDefinitions: %v", err)
	}
	for _, listed := range defs {
		if listed.PolicyDefinitionID == custom.PolicyDefinitionID {
			t.Fatalf("deleted definition should be hidden from lists: %+v", defs)
		}
	}
}

func TestServiceUpdateDefinitionCreatesVersionAndRefreshesBoundInstanceUsage(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	testutil.CreateTestWallet(t, db, userID, "wallet_refresh", "0x1234567890123456789012345678901234567890")
	svc := policy.NewService(db)

	def, err := svc.CreatePolicyDefinition(userID, policy.DefinitionInput{
		Name:                  "desk auto",
		Mode:                  "auto",
		MaxPerTransactionUSD:  usd("100"),
		DailyTransferLimitUSD: usd("200"),
		AutoSignEnabled:       true,
		AutoSignMaxPerTxUSD:   usd("20"),
		AutoSignDailyLimitUSD: usd("40"),
		AutoSignMaxCountDay:   2,
	})
	if err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}
	instance, err := svc.BindPolicyDefinition(userID, "wallet_refresh", def.PolicyDefinitionID)
	if err != nil {
		t.Fatalf("BindPolicyDefinition: %v", err)
	}
	if err := db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", instance.PolicyInstanceID).Updates(map[string]interface{}{
		"daily_transfer_used_usd":      usd("75"),
		"daily_trade_used_usd":         usd("30"),
		"auto_sign_daily_used_usd":     usd("15"),
		"auto_sign_count_today":        1,
		"last_reset_date":              "2026-05-17",
		"policy_definition_version_id": instance.PolicyDefinitionVersionID,
	}).Error; err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	pendingUsage := storage.PolicyUsageEvent{
		EventID:   "pue_definition_refresh",
		UserID:    userID,
		WalletID:  "wallet_refresh",
		PolicyID:  instance.PolicyInstanceID,
		Chain:     "ethereum",
		TxHash:    "0xabc",
		TxType:    "transfer",
		AmountUSD: usd("10"),
		Decision:  string(policy.DecisionAuto),
		Status:    policy.StatusSigned,
		UsageDate: time.Now().UTC().Format("2006-01-02"),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(&pendingUsage).Error; err != nil {
		t.Fatalf("seed pending usage event: %v", err)
	}

	updated, err := svc.UpdatePolicyDefinition(userID, def.PolicyDefinitionID, policy.DefinitionInput{
		Name:                 "desk manual",
		Mode:                 "manual",
		MaxPerTransactionUSD: usd("50"),
	})
	if err != nil {
		t.Fatalf("UpdatePolicyDefinition: %v", err)
	}
	if updated.CurrentVersionID == def.CurrentVersionID {
		t.Fatalf("update should create a new version: old=%s new=%s", def.CurrentVersionID, updated.CurrentVersionID)
	}

	var refreshed storage.PolicyInstance
	if err := db.First(&refreshed, "policy_instance_id = ?", instance.PolicyInstanceID).Error; err != nil {
		t.Fatalf("load instance: %v", err)
	}
	if refreshed.PolicyDefinitionVersionID != updated.CurrentVersionID {
		t.Fatalf("instance version = %s, want %s", refreshed.PolicyDefinitionVersionID, updated.CurrentVersionID)
	}
	if refreshed.DailyTransferUsedUSD.String() != "0" || refreshed.DailyTradeUsedUSD.String() != "0" || refreshed.AutoSignDailyUsedUSD.String() != "0" || refreshed.AutoSignCountToday != 0 {
		t.Fatalf("usage should be refreshed after definition update: %+v", refreshed)
	}

	var expired storage.PolicyUsageEvent
	if err := db.First(&expired, "event_id = ?", pendingUsage.EventID).Error; err != nil {
		t.Fatalf("load pending usage event: %v", err)
	}
	if expired.Status != policy.StatusExpired {
		t.Fatalf("pending usage event status = %s, want %s", expired.Status, policy.StatusExpired)
	}
}
