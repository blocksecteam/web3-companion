// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (s *Service) UsageForPolicy(policyID string, now time.Time) (Usage, error) {
	if now.IsZero() {
		now = time.Now()
	}
	usageDate := now.UTC().Format("2006-01-02")
	return s.usageForInstance(policyID, usageDate, now)
}

func (s *Service) ResponseForPolicy(p Policy, now time.Time) (Response, error) {
	usage, err := s.UsageForPolicy(p.PolicyID, now)
	if err != nil {
		return Response{}, err
	}
	return Response{
		PolicyID:                  p.PolicyID,
		WalletID:                  walletDisplayID(storage.Wallet{WalletID: p.WalletID}),
		Name:                      p.Name,
		Enabled:                   p.Enabled,
		Mode:                      p.Mode,
		MaxPerTransactionUSD:      p.MaxPerTransactionUSD,
		DailyTransferLimitUSD:     p.DailyTransferLimitUSD,
		DailyTradeLimitUSD:        p.DailyTradeLimitUSD,
		AutoSignEnabled:           p.AutoSignEnabled,
		AutoSignMaxPerTxUSD:       p.AutoSignMaxPerTxUSD,
		AutoSignDailyLimitUSD:     p.AutoSignDailyLimitUSD,
		AutoSignMaxCountDay:       p.AutoSignMaxCountDay,
		RecipientWhitelistEnabled: p.RecipientWhitelistEnabled,
		RecipientWhitelistAddrs:   DecodeAddressList(p.RecipientWhitelistAddrs),
		Usage:                     usage,
		CreatedAt:                 p.CreatedAt,
		UpdatedAt:                 p.UpdatedAt,
	}, nil
}

func (s *Service) usageForInstance(policyID string, usageDate string, now time.Time) (Usage, error) {
	var instance storage.PolicyInstance
	if err := s.db.First(&instance, "policy_instance_id = ?", policyID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Usage{}, ErrNoPolicy
		}
		return Usage{}, err
	}
	if err := s.resetInstanceUsageIfNeeded(&instance, now); err != nil {
		return Usage{}, err
	}
	return usageForInstanceTx(s.db, instance, usageDate, now)
}

func usageForInstanceTx(tx *gorm.DB, instance storage.PolicyInstance, usageDate string, now time.Time) (Usage, error) {
	usage := Usage{
		DailyTransferUsedUSD:   instance.DailyTransferUsedUSD,
		DailyTradeUsedUSD:      instance.DailyTradeUsedUSD,
		AutoSignDailyUsedUSD:   instance.AutoSignDailyUsedUSD,
		AutoSignCountToday:     instance.AutoSignCountToday,
		LastResetDate:          instance.LastResetDate,
		SubmittedTransferUSD:   instance.DailyTransferUsedUSD,
		SubmittedTradeUSD:      instance.DailyTradeUsedUSD,
		AutoSignSubmittedUSD:   instance.AutoSignDailyUsedUSD,
		AutoSignSubmittedCount: instance.AutoSignCountToday,
	}
	var events []storage.PolicyUsageEvent
	if err := tx.Where("policy_id = ? AND usage_date = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", instance.PolicyInstanceID, usageDate, StatusSigned, now.UTC()).
		Find(&events).Error; err != nil {
		return Usage{}, err
	}
	for _, event := range events {
		if event.PriceUnknown {
			continue
		}
		if transferBucket(event.TxType) {
			usage.PendingTransferUSD = money.Sum(usage.PendingTransferUSD, event.AmountUSD)
		}
		if tradeBucket(event.TxType) {
			usage.PendingTradeUSD = money.Sum(usage.PendingTradeUSD, event.AmountUSD)
		}
		if event.Decision == string(DecisionAuto) {
			usage.AutoSignPendingUSD = money.Sum(usage.AutoSignPendingUSD, event.AmountUSD)
			usage.AutoSignPendingCount++
		}
	}
	return usage, nil
}

func (s *Service) resetInstanceUsageIfNeeded(instance *storage.PolicyInstance, now time.Time) error {
	return resetInstanceUsageIfNeededTx(s.db, instance, now)
}

func resetInstanceUsageIfNeededTx(tx *gorm.DB, instance *storage.PolicyInstance, now time.Time) error {
	resetDate := now.UTC().Format("2006-01-02")
	if instance.LastResetDate == resetDate {
		return nil
	}
	updates := map[string]interface{}{
		"last_reset_date": resetDate,
	}
	if instance.LastResetDate != "" {
		updates["daily_transfer_used_usd"] = money.Amount{}
		updates["daily_trade_used_usd"] = money.Amount{}
		updates["auto_sign_daily_used_usd"] = money.Amount{}
		updates["auto_sign_count_today"] = 0
		instance.DailyTransferUsedUSD = money.Amount{}
		instance.DailyTradeUsedUSD = money.Amount{}
		instance.AutoSignDailyUsedUSD = money.Amount{}
		instance.AutoSignCountToday = 0
	}
	if err := tx.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", instance.PolicyInstanceID).Updates(updates).Error; err != nil {
		return err
	}
	instance.LastResetDate = resetDate
	return nil
}

func (s *Service) RecordAutoSign(p *Policy, txType string, amountUSD money.Amount, now time.Time) error {
	if p == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	return incrementSubmittedUsage(s.db, storage.PolicyUsageEvent{
		PolicyID: p.PolicyID, TxType: txType, AmountUSD: amountUSD, Decision: string(DecisionAuto),
	}, now)
}

func (s *Service) RecordUserSign(p *Policy, txType string, amountUSD money.Amount, now time.Time) error {
	if p == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	return incrementSubmittedUsage(s.db, storage.PolicyUsageEvent{
		PolicyID: p.PolicyID, TxType: txType, AmountUSD: amountUSD, Decision: string(DecisionWebAuthn),
	}, now)
}

func incrementSubmittedUsage(db *gorm.DB, event storage.PolicyUsageEvent, now time.Time) error {
	var instance storage.PolicyInstance
	if err := db.First(&instance, "policy_instance_id = ?", event.PolicyID).Error; err != nil {
		return err
	}
	if err := resetInstanceUsageIfNeededTx(db, &instance, now); err != nil {
		return err
	}
	updates := map[string]interface{}{}
	if transferBucket(event.TxType) {
		instance.DailyTransferUsedUSD = money.Sum(instance.DailyTransferUsedUSD, event.AmountUSD)
		updates["daily_transfer_used_usd"] = instance.DailyTransferUsedUSD
	}
	if tradeBucket(event.TxType) {
		instance.DailyTradeUsedUSD = money.Sum(instance.DailyTradeUsedUSD, event.AmountUSD)
		updates["daily_trade_used_usd"] = instance.DailyTradeUsedUSD
	}
	if event.Decision == string(DecisionAuto) {
		instance.AutoSignDailyUsedUSD = money.Sum(instance.AutoSignDailyUsedUSD, event.AmountUSD)
		updates["auto_sign_daily_used_usd"] = instance.AutoSignDailyUsedUSD
		updates["auto_sign_count_today"] = gorm.Expr("auto_sign_count_today + 1")
	}
	if len(updates) == 0 {
		return nil
	}
	return db.Model(&storage.PolicyInstance{}).Where("policy_instance_id = ?", event.PolicyID).Updates(updates).Error
}
