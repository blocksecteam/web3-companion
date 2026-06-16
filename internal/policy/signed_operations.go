// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (s *Service) RecordSignedOperation(op SignedOperation) error {
	if op.Now.IsZero() {
		op.Now = time.Now()
	}
	if op.ExpiresAt == nil {
		expiresAt := op.Now.Add(30 * time.Minute)
		op.ExpiresAt = &expiresAt
	}
	event := storage.PolicyUsageEvent{
		EventID:      "pue_" + randomID(16),
		UserID:       op.UserID,
		WalletID:     op.WalletID,
		WalletUID:    op.WalletUID,
		PolicyID:     op.PolicyID,
		Chain:        strings.ToLower(strings.TrimSpace(op.Chain)),
		TxHash:       strings.ToLower(strings.TrimSpace(op.TxHash)),
		TxType:       strings.TrimSpace(op.TxType),
		AmountUSD:    op.AmountUSD,
		PriceUnknown: op.PriceUnknown,
		Decision:     string(op.Decision),
		Status:       StatusSigned,
		UsageDate:    op.Now.UTC().Format("2006-01-02"),
		ExpiresAt:    op.ExpiresAt,
		CreatedAt:    op.Now,
		UpdatedAt:    op.Now,
	}
	if event.UserID == "" || event.WalletID == "" || event.PolicyID == "" || event.Chain == "" || event.TxHash == "" || event.TxType == "" {
		return fmt.Errorf("incomplete signed operation")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if event.WalletUID == "" {
			var instance storage.PolicyInstance
			if err := tx.First(&instance, "policy_instance_id = ?", event.PolicyID).Error; err != nil {
				return err
			}
			event.WalletUID = instance.WalletUID
			if event.WalletID == "" {
				event.WalletID = instance.WalletID
			}
		}
		if event.WalletUID == "" {
			return fmt.Errorf("incomplete signed operation")
		}
		if err := validateSignedReservation(tx, event); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
	})
}

func validateSignedReservation(tx *gorm.DB, event storage.PolicyUsageEvent) error {
	if event.Decision != string(DecisionAuto) || event.PriceUnknown {
		return nil
	}
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var instance storage.PolicyInstance
	if err := tx.First(&instance, "policy_instance_id = ?", event.PolicyID).Error; err != nil {
		return err
	}
	if err := resetInstanceUsageIfNeededTx(tx, &instance, now); err != nil {
		return err
	}
	var version storage.PolicyDefinitionVersion
	if err := tx.First(&version, "policy_definition_version_id = ?", instance.PolicyDefinitionVersionID).Error; err != nil {
		return err
	}
	if version.Mode != "auto" || !version.AutoSignEnabled {
		return nil
	}
	usage, err := usageForInstanceTx(tx, instance, event.UsageDate, now)
	if err != nil {
		return err
	}
	if limitExceeded(event.AmountUSD, version.AutoSignMaxPerTxUSD) {
		return ErrPolicyBlocked
	}
	if sumLimitExceeded(version.AutoSignDailyLimitUSD, usage.AutoSignDailyUsedUSD, usage.AutoSignPendingUSD, event.AmountUSD) {
		return ErrPolicyBlocked
	}
	if version.AutoSignMaxCountDay > 0 && usage.AutoSignCountToday+usage.AutoSignPendingCount >= version.AutoSignMaxCountDay {
		return ErrPolicyBlocked
	}
	return nil
}

func (s *Service) SignedOperationForBroadcast(userID, txHash, chain string) (*storage.PolicyUsageEvent, error) {
	var event storage.PolicyUsageEvent
	now := time.Now().UTC()
	err := s.db.Where("user_id = ? AND tx_hash = ? AND chain = ? AND status = ?", userID, strings.ToLower(txHash), normalizeChain(chain), StatusSigned).
		First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSignedOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	if event.ExpiresAt != nil && !event.ExpiresAt.After(now) {
		_ = s.db.Model(&event).Updates(map[string]interface{}{"status": StatusExpired, "updated_at": now}).Error
		return nil, ErrSignedOperationNotFound
	}
	return &event, nil
}

func (s *Service) MarkOperationSubmitted(userID, txHash, chain string) (*storage.PolicyUsageEvent, error) {
	var out storage.PolicyUsageEvent
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var event storage.PolicyUsageEvent
		err := tx.Where("user_id = ? AND tx_hash = ? AND chain = ?", userID, strings.ToLower(txHash), normalizeChain(chain)).
			First(&event).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSignedOperationNotFound
		}
		if err != nil {
			return err
		}
		if event.Status == StatusSubmitted || event.Status == StatusConfirmed {
			out = event
			return nil
		}
		if event.Status != StatusSigned {
			return fmt.Errorf("signed operation is %s", event.Status)
		}
		now := time.Now().UTC()
		if err := tx.Model(&event).Updates(map[string]interface{}{
			"status":       StatusSubmitted,
			"submitted_at": &now,
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}
		if !event.PriceUnknown {
			if err := incrementSubmittedUsage(tx, event, now); err != nil {
				return err
			}
		}
		event.Status = StatusSubmitted
		event.SubmittedAt = &now
		out = event
		return nil
	})
	return &out, err
}
