// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (s *Service) BindDefaultPolicy(userID, walletID string) (*storage.PolicyInstance, error) {
	def, err := s.EnsureUserDefaultPolicy(userID)
	if err != nil {
		return nil, err
	}
	return s.BindPolicyDefinition(userID, walletID, def.PolicyDefinitionID)
}

func (s *Service) BindPolicyDefinition(userID, walletID, definitionID string) (*storage.PolicyInstance, error) {
	var out storage.PolicyInstance
	err := s.db.Transaction(func(tx *gorm.DB) error {
		wallet, err := activeWalletForPolicy(tx, userID, walletID)
		if err != nil {
			return err
		}
		var def storage.PolicyDefinition
		if err := tx.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", definitionID, userID).First(&def).Error; err != nil {
			return err
		}
		if def.CurrentVersionID == "" {
			return ErrNoPolicy
		}
		instance := storage.PolicyInstance{
			PolicyInstanceID:          "pinst_" + randomID(16),
			UserID:                    userID,
			WalletID:                  wallet.WalletID,
			DisplayWalletID:           walletDisplayID(wallet),
			WalletUID:                 wallet.WalletUID,
			WalletGeneration:          wallet.Generation,
			PolicyDefinitionID:        def.PolicyDefinitionID,
			PolicyDefinitionVersionID: def.CurrentVersionID,
			LastResetDate:             time.Now().UTC().Format("2006-01-02"),
		}
		if err := tx.Where("user_id = ? AND wallet_id = ?", userID, wallet.WalletID).
			Delete(&storage.PolicyInstance{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&instance).Error; err != nil {
			return err
		}
		out = instance
		return nil
	})
	return &out, err
}

func (s *Service) EnsureDefaultPolicy(userID, walletID string) (*Policy, error) {
	instance, err := s.BindDefaultPolicy(userID, walletID)
	if err != nil {
		return nil, err
	}
	return s.policyFromInstanceID(userID, instance.PolicyInstanceID, time.Now())
}

func (s *Service) CurrentPolicy(userID, walletID string, now time.Time) (*Policy, error) {
	if now.IsZero() {
		now = time.Now()
	}
	return s.currentInstancePolicy(userID, walletID, now)
}

func (s *Service) currentInstancePolicy(userID, walletID string, now time.Time) (*Policy, error) {
	wallet, err := activeWalletForPolicy(s.db, userID, walletID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoPolicy
		}
		return nil, err
	}
	var instance storage.PolicyInstance
	if err := s.db.Where("user_id = ? AND wallet_id = ?", userID, wallet.WalletID).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoPolicy
		}
		return nil, err
	}
	return s.policyFromInstance(userID, instance, now)
}

func (s *Service) policyFromInstanceID(userID, policyInstanceID string, now time.Time) (*Policy, error) {
	var instance storage.PolicyInstance
	if err := s.db.Where("user_id = ? AND policy_instance_id = ?", userID, policyInstanceID).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoPolicy
		}
		return nil, err
	}
	return s.policyFromInstance(userID, instance, now)
}

func activeWalletForPolicy(db *gorm.DB, userID, walletID string) (storage.Wallet, error) {
	var wallet storage.Wallet
	err := db.Where("user_id = ? AND display_wallet_id = ? AND deleted_at IS NULL", userID, strings.TrimSpace(walletID)).First(&wallet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = db.Where("user_id = ? AND wallet_id = ? AND deleted_at IS NULL", userID, strings.TrimSpace(walletID)).First(&wallet).Error
	}
	if wallet.DisplayWalletID == "" {
		wallet.DisplayWalletID = wallet.WalletID
	}
	return wallet, err
}

func walletDisplayID(wallet storage.Wallet) string {
	if strings.TrimSpace(wallet.DisplayWalletID) != "" {
		return strings.TrimSpace(wallet.DisplayWalletID)
	}
	return strings.TrimSpace(wallet.WalletID)
}

func (s *Service) policyFromInstance(userID string, instance storage.PolicyInstance, now time.Time) (*Policy, error) {
	if err := s.resetInstanceUsageIfNeeded(&instance, now); err != nil {
		return nil, err
	}
	var def storage.PolicyDefinition
	if err := s.db.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", instance.PolicyDefinitionID, userID).First(&def).Error; err != nil {
		return nil, err
	}
	var version storage.PolicyDefinitionVersion
	if err := s.db.Where("policy_definition_version_id = ? AND user_id = ?", instance.PolicyDefinitionVersionID, userID).First(&version).Error; err != nil {
		return nil, err
	}
	return policyFromInstance(def, version, instance), nil
}

func policyFromInstance(def storage.PolicyDefinition, version storage.PolicyDefinitionVersion, instance storage.PolicyInstance) *Policy {
	return &Policy{
		PolicyID:                  instance.PolicyInstanceID,
		WalletID:                  walletDisplayID(storage.Wallet{WalletID: instance.WalletID, DisplayWalletID: instance.DisplayWalletID}),
		WalletUID:                 instance.WalletUID,
		UserID:                    instance.UserID,
		Name:                      def.Name,
		Enabled:                   true,
		Mode:                      version.Mode,
		MaxPerTransactionUSD:      version.MaxPerTransactionUSD,
		DailyTransferLimitUSD:     version.DailyTransferLimitUSD,
		DailyTradeLimitUSD:        version.DailyTradeLimitUSD,
		AutoSignEnabled:           version.AutoSignEnabled,
		AutoSignMaxPerTxUSD:       version.AutoSignMaxPerTxUSD,
		AutoSignDailyLimitUSD:     version.AutoSignDailyLimitUSD,
		AutoSignMaxCountDay:       version.AutoSignMaxCountDay,
		RecipientWhitelistEnabled: version.RecipientWhitelistEnabled,
		RecipientWhitelistAddrs:   version.RecipientWhitelistAddrs,
		DailyTransferUsedUSD:      instance.DailyTransferUsedUSD,
		DailyTradeUsedUSD:         instance.DailyTradeUsedUSD,
		AutoSignDailyUsedUSD:      instance.AutoSignDailyUsedUSD,
		AutoSignCountToday:        instance.AutoSignCountToday,
		LastResetDate:             instance.LastResetDate,
		CreatedAt:                 instance.CreatedAt,
		UpdatedAt:                 instance.UpdatedAt,
	}
}
