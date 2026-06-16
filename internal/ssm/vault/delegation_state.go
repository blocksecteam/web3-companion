// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (v *Vault) upsertDelegationState(ctx context.Context, wallet storage.Wallet, chainName string, values map[string]interface{}) error {
	now := time.Now().UTC()
	walletID := walletDisplayID(wallet)
	state := storage.WalletDelegation{
		WalletUID: wallet.WalletUID,
		Chain:     strings.TrimSpace(chainName),
	}
	err := v.db.WithContext(ctx).Where("wallet_uid = ? AND chain = ?", wallet.WalletUID, chainName).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state = storage.WalletDelegation{
			WalletUID:     wallet.WalletUID,
			Chain:         strings.TrimSpace(chainName),
			UserID:        wallet.UserID,
			WalletID:      walletID,
			Status:        "not_delegated",
			LastCheckedAt: &now,
		}
		for key, value := range values {
			applyDelegationField(&state, key, value)
		}
		return v.db.WithContext(ctx).Create(&state).Error
	}
	if err != nil {
		return err
	}
	values["user_id"] = wallet.UserID
	values["wallet_id"] = walletID
	values["last_checked_at"] = now
	return v.db.WithContext(ctx).Model(&storage.WalletDelegation{}).
		Where("wallet_uid = ? AND chain = ?", wallet.WalletUID, chainName).
		Updates(values).Error
}

func applyDelegationField(state *storage.WalletDelegation, key string, value interface{}) {
	switch key {
	case "user_id":
		if v, ok := value.(string); ok {
			state.UserID = v
		}
	case "wallet_id":
		if v, ok := value.(string); ok {
			state.WalletID = v
		}
	case "delegated":
		if v, ok := value.(bool); ok {
			state.Delegated = v
		}
	case "delegator":
		if v, ok := value.(string); ok {
			state.Delegator = v
		}
	case "status":
		if v, ok := value.(string); ok {
			state.Status = v
		}
	case "delegation_hash":
		if v, ok := value.(string); ok {
			state.DelegationHash = v
		}
	case "delegated_at":
		switch v := value.(type) {
		case time.Time:
			state.DelegatedAt = &v
		case *time.Time:
			state.DelegatedAt = v
		case nil:
			state.DelegatedAt = nil
		}
	case "last_checked_at":
		switch v := value.(type) {
		case time.Time:
			state.LastCheckedAt = &v
		case *time.Time:
			state.LastCheckedAt = v
		case nil:
			state.LastCheckedAt = nil
		}
	}
}
