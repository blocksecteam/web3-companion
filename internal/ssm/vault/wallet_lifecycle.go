// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (v *Vault) nextWalletGeneration(userID, walletID string) (int, error) {
	var maxGeneration int
	err := v.db.Model(&storage.Wallet{}).
		Where("user_id = ? AND display_wallet_id = ?", userID, walletID).
		Select("COALESCE(MAX(generation), 0)").
		Scan(&maxGeneration).Error
	if err != nil {
		return 0, err
	}
	if maxGeneration < 0 {
		maxGeneration = 0
	}
	return maxGeneration + 1, nil
}

func (v *Vault) CreateWallet(ctx context.Context, userID, walletID string) (*storage.Wallet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	userID = strings.TrimSpace(userID)
	walletID = strings.TrimSpace(walletID)

	var existing storage.Wallet
	err := v.db.Where("user_id = ? AND display_wallet_id = ? AND deleted_at IS NULL", userID, walletID).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("wallet_id %q already exists", walletID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("check existing wallet: %w", err)
	}

	generation, err := v.nextWalletGeneration(userID, walletID)
	if err != nil {
		return nil, fmt.Errorf("check wallet generation: %w", err)
	}
	storageWalletID := storageWalletIDForGeneration(walletID, generation)
	walletUID := "wuid_" + randomHex(12)

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	privateKeyBytes := crypto.FromECDSA(privateKey)

	dataKey, encryptedDataKey, err := v.kms.GenerateDataKey(ctx)
	if err != nil {
		ZeroBytes(privateKeyBytes)
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	defer ZeroBytes(dataKey)

	encryptedPrivKey, err := Encrypt(dataKey, privateKeyBytes)
	ZeroBytes(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	keyFile := v.keyFilePathForGeneration(userID, walletID, generation)
	kf := keyFileContent{Version: 1, Address: address.Hex(), Encrypted: encryptedPrivKey, DataKeyEnc: encryptedDataKey}
	if err := v.writeKeyFile(keyFile, &kf); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}

	meta := map[string]string{"user_id": userID, "wallet_id": walletID, "wallet_uid": walletUID, "address": address.Hex()}
	metaBytes, _ := json.Marshal(meta)
	metaFile := v.metaFilePathForGeneration(userID, walletID, generation)
	if err := os.WriteFile(metaFile, metaBytes, 0o600); err != nil {
		v.secureDelete(keyFile)
		return nil, fmt.Errorf("write meta file: %w", err)
	}

	var existingCount int64
	v.db.Model(&storage.Wallet{}).Where("user_id = ? AND deleted_at IS NULL", userID).Count(&existingCount)
	w := &storage.Wallet{
		WalletID:        storageWalletID,
		DisplayWalletID: walletID,
		WalletUID:       walletUID,
		Generation:      generation,
		UserID:          userID,
		Address:         address.Hex(),
		KeyFile:         keyFile,
		KeyStatus:       "active",
		IsDefault:       existingCount == 0,
	}
	if err := v.db.Create(w).Error; err != nil {
		v.secureDelete(keyFile)
		_ = os.Remove(metaFile)
		return nil, fmt.Errorf("db create wallet: %w", err)
	}
	w.WalletID = walletID
	return w, nil
}

func (v *Vault) GetWallet(userID, walletID string) (*storage.Wallet, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrWalletNotFound
	}
	query := v.db.Where("user_id = ? AND display_wallet_id = ? AND deleted_at IS NULL", userID, walletID)
	var wallet storage.Wallet
	if err := query.First(&wallet).Error; err != nil {
		return nil, ErrWalletNotFound
	}
	return &wallet, nil
}

func (v *Vault) ListWallets(userID string, limit, offset int) ([]storage.Wallet, error) {
	var wallets []storage.Wallet
	q := v.db.Where("user_id = ? AND deleted_at IS NULL", userID).Order("is_default DESC, created_at DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	if err := q.Find(&wallets).Error; err != nil {
		return nil, err
	}
	return wallets, nil
}

func (v *Vault) SetDefaultWallet(userID, walletID string) error {
	return v.db.Transaction(func(tx *gorm.DB) error {
		var wallet storage.Wallet
		if err := tx.Where("display_wallet_id = ? AND user_id = ? AND deleted_at IS NULL", walletID, userID).First(&wallet).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Wallet{}).
			Where("user_id = ?", userID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&storage.Wallet{}).
			Where("user_id = ? AND wallet_id = ?", userID, wallet.WalletID).
			Update("is_default", true).Error
	})
}

func (v *Vault) DeleteWallet(ctx context.Context, userID, walletID string) error {
	_ = ctx
	v.mu.Lock()
	defer v.mu.Unlock()

	var wallet storage.Wallet
	if err := v.db.Where("user_id = ? AND display_wallet_id = ? AND deleted_at IS NULL", userID, walletID).First(&wallet).Error; err != nil {
		return ErrWalletNotFound
	}
	if err := v.softDeleteWalletRecords(userID, wallet); err != nil {
		return fmt.Errorf("delete wallet records: %w", err)
	}
	return nil
}

func (v *Vault) softDeleteWalletRecords(userID string, wallet storage.Wallet) error {
	return v.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Where("user_id = ? AND wallet_id = ?", userID, wallet.WalletID).
			Delete(&storage.PolicyInstance{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.WalletDelegation{}).
			Where("wallet_uid = ?", wallet.WalletUID).
			Updates(map[string]interface{}{
				"delegated":       false,
				"status":          "deleted",
				"delegation_hash": "",
				"delegated_at":    nil,
				"last_checked_at": now,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&storage.Wallet{}).
			Where("user_id = ? AND wallet_id = ? AND deleted_at IS NULL", userID, wallet.WalletID).
			Updates(map[string]interface{}{
				"deleted_at": &now,
				"updated_at": now,
				"key_status": "soft_deleted",
				"is_default": false,
			}).Error
	})
}
