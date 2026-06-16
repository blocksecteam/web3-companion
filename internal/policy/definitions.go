// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (s *Service) EnsureUserDefaultPolicy(userID string) (*storage.PolicyDefinition, error) {
	var existing storage.PolicyDefinition
	err := s.db.Where("user_id = ? AND is_default = ? AND deleted_at IS NULL", userID, true).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return s.createPolicyDefinition(userID, DefinitionInput{Name: "default", Mode: "manual"}, true)
}

func (s *Service) CreatePolicyDefinition(userID string, input DefinitionInput) (*storage.PolicyDefinition, error) {
	return s.createPolicyDefinition(userID, input, false)
}

func (s *Service) createPolicyDefinition(userID string, input DefinitionInput, isDefault bool) (*storage.PolicyDefinition, error) {
	def := &storage.PolicyDefinition{
		PolicyDefinitionID: "pdef_" + randomID(16),
		UserID:             userID,
		Name:               definitionName(input.Name),
		IsDefault:          isDefault,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if isDefault {
			if err := tx.Model(&storage.PolicyDefinition{}).
				Where("user_id = ?", userID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(def).Error; err != nil {
			return err
		}
		version, err := createDefinitionVersion(tx, userID, def.PolicyDefinitionID, 1, input)
		if err != nil {
			return err
		}
		if err := tx.Model(def).Update("current_version_id", version.PolicyDefinitionVersionID).Error; err != nil {
			return err
		}
		def.CurrentVersionID = version.PolicyDefinitionVersionID
		return nil
	})
	return def, err
}

func (s *Service) ListPolicyDefinitions(userID string, limit, offset int) ([]storage.PolicyDefinition, error) {
	var defs []storage.PolicyDefinition
	q := s.db.Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("is_default DESC, updated_at DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	if err := q.Find(&defs).Error; err != nil {
		return nil, err
	}
	if defs == nil {
		defs = []storage.PolicyDefinition{}
	}
	return defs, nil
}

func (s *Service) ListPolicyDefinitionResponses(userID string, limit, offset int) ([]DefinitionResponse, error) {
	defs, err := s.ListPolicyDefinitions(userID, limit, offset)
	if err != nil {
		return nil, err
	}
	responses := make([]DefinitionResponse, 0, len(defs))
	for _, def := range defs {
		resp, err := s.ResponseForDefinition(def)
		if err != nil {
			return nil, err
		}
		responses = append(responses, resp)
	}
	return responses, nil
}

func (s *Service) ResponseForDefinition(def storage.PolicyDefinition) (DefinitionResponse, error) {
	var version storage.PolicyDefinitionVersion
	if err := s.db.First(&version, "policy_definition_version_id = ?", def.CurrentVersionID).Error; err != nil {
		return DefinitionResponse{}, err
	}
	return DefinitionResponse{
		PolicyDefinitionID:        def.PolicyDefinitionID,
		CurrentVersionID:          def.CurrentVersionID,
		Name:                      def.Name,
		IsDefault:                 def.IsDefault,
		Version:                   version.Version,
		Mode:                      version.Mode,
		MaxPerTransactionUSD:      version.MaxPerTransactionUSD,
		DailyTransferLimitUSD:     version.DailyTransferLimitUSD,
		DailyTradeLimitUSD:        version.DailyTradeLimitUSD,
		AutoSignEnabled:           version.AutoSignEnabled,
		AutoSignMaxPerTxUSD:       version.AutoSignMaxPerTxUSD,
		AutoSignDailyLimitUSD:     version.AutoSignDailyLimitUSD,
		AutoSignMaxCountDay:       version.AutoSignMaxCountDay,
		RecipientWhitelistEnabled: version.RecipientWhitelistEnabled,
		RecipientWhitelistAddrs:   DecodeAddressList(version.RecipientWhitelistAddrs),
		CreatedAt:                 def.CreatedAt,
		UpdatedAt:                 def.UpdatedAt,
	}, nil
}

func (s *Service) UpdatePolicyDefinition(userID, definitionID string, input DefinitionInput) (*storage.PolicyDefinition, error) {
	var out storage.PolicyDefinition
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var def storage.PolicyDefinition
		if err := tx.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", definitionID, userID).First(&def).Error; err != nil {
			return err
		}
		var latest storage.PolicyDefinitionVersion
		nextVersion := 1
		if err := tx.Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).
			Order("version DESC").
			First(&latest).Error; err == nil {
			nextVersion = latest.Version + 1
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		version, err := createDefinitionVersion(tx, userID, definitionID, nextVersion, input)
		if err != nil {
			return err
		}
		updates := map[string]interface{}{
			"name":               definitionName(input.Name),
			"current_version_id": version.PolicyDefinitionVersionID,
		}
		if err := tx.Model(&storage.PolicyDefinition{}).
			Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).
			Updates(updates).Error; err != nil {
			return err
		}
		resetDate := time.Now().UTC().Format("2006-01-02")
		var instances []storage.PolicyInstance
		if err := tx.Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).Find(&instances).Error; err != nil {
			return err
		}
		instanceIDs := make([]string, 0, len(instances))
		for _, instance := range instances {
			instanceIDs = append(instanceIDs, instance.PolicyInstanceID)
		}
		if len(instanceIDs) > 0 {
			now := time.Now().UTC()
			if err := tx.Model(&storage.PolicyUsageEvent{}).
				Where("user_id = ? AND policy_id IN ? AND status = ?", userID, instanceIDs, StatusSigned).
				Updates(map[string]interface{}{"status": StatusExpired, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&storage.PolicyInstance{}).
			Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).
			Updates(map[string]interface{}{
				"policy_definition_version_id": version.PolicyDefinitionVersionID,
				"daily_transfer_used_usd":      money.Amount{},
				"daily_trade_used_usd":         money.Amount{},
				"auto_sign_daily_used_usd":     money.Amount{},
				"auto_sign_count_today":        0,
				"last_reset_date":              resetDate,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).First(&out).Error; err != nil {
			return err
		}
		return nil
	})
	return &out, err
}

func (s *Service) DeletePolicyDefinition(userID, definitionID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var def storage.PolicyDefinition
		if err := tx.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", definitionID, userID).First(&def).Error; err != nil {
			return err
		}
		if def.IsDefault {
			return ErrDefaultPolicyDeleteBlocked
		}
		var bound int64
		if err := tx.Model(&storage.PolicyInstance{}).
			Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).
			Count(&bound).Error; err != nil {
			return err
		}
		if bound > 0 {
			return ErrPolicyDefinitionInUse
		}
		now := time.Now().UTC()
		return tx.Model(&storage.PolicyDefinition{}).
			Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).
			Update("deleted_at", &now).Error
	})
}

func (s *Service) SetDefaultPolicyDefinition(userID, definitionID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var def storage.PolicyDefinition
		if err := tx.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", definitionID, userID).First(&def).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.PolicyDefinition{}).
			Where("user_id = ?", userID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&storage.PolicyDefinition{}).
			Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).
			Update("is_default", true).Error
	})
}

func createDefinitionVersion(tx *gorm.DB, userID, definitionID string, versionNumber int, input DefinitionInput) (*storage.PolicyDefinitionVersion, error) {
	whitelistJSON, _ := json.Marshal(NormalizeAddressList(input.RecipientWhitelistAddrs))
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "manual"
	}
	v := &storage.PolicyDefinitionVersion{
		PolicyDefinitionVersionID: "pver_" + randomID(16),
		PolicyDefinitionID:        definitionID,
		UserID:                    userID,
		Version:                   versionNumber,
		Mode:                      mode,
		MaxPerTransactionUSD:      input.MaxPerTransactionUSD,
		DailyTransferLimitUSD:     input.DailyTransferLimitUSD,
		DailyTradeLimitUSD:        input.DailyTradeLimitUSD,
		AutoSignEnabled:           input.AutoSignEnabled,
		AutoSignMaxPerTxUSD:       input.AutoSignMaxPerTxUSD,
		AutoSignDailyLimitUSD:     input.AutoSignDailyLimitUSD,
		AutoSignMaxCountDay:       input.AutoSignMaxCountDay,
		RecipientWhitelistEnabled: input.RecipientWhitelistEnabled,
		RecipientWhitelistAddrs:   string(whitelistJSON),
	}
	if v.Mode != "auto" {
		v.AutoSignEnabled = false
		v.AutoSignMaxPerTxUSD = money.Amount{}
		v.AutoSignDailyLimitUSD = money.Amount{}
		v.AutoSignMaxCountDay = 0
	}
	if err := tx.Create(v).Error; err != nil {
		return nil, err
	}
	return v, nil
}

func definitionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "default"
	}
	return name
}

func (s *Service) DefinitionSummary(userID string) (DefinitionSummaryResult, error) {
	type modeRow struct {
		Mode            string
		AutoSignEnabled bool
		Count           int64
	}
	var rows []modeRow
	if err := s.db.Table("policy_definition_versions pdv").
		Select("pdv.mode, pdv.auto_sign_enabled, COUNT(*) as count").
		Joins("JOIN policy_definitions pd ON pd.current_version_id = pdv.policy_definition_version_id").
		Where("pd.user_id = ? AND pd.deleted_at IS NULL", userID).
		Group("pdv.mode, pdv.auto_sign_enabled").
		Scan(&rows).Error; err != nil {
		return DefinitionSummaryResult{}, err
	}

	var result DefinitionSummaryResult
	for _, r := range rows {
		if r.Mode == "auto" && r.AutoSignEnabled {
			result.Auto += r.Count
		} else {
			result.Manual += r.Count
		}
	}

	var def storage.PolicyDefinition
	if err := s.db.Select("name").
		Where("user_id = ? AND is_default = ? AND deleted_at IS NULL", userID, true).
		First(&def).Error; err == nil {
		result.DefaultName = def.Name
	}

	return result, nil
}
