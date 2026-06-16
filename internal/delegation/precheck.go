// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package delegation

import (
	"context"
	"errors"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

type Status int

const (
	OK Status = iota
	NotDelegated
	PendingDelegation
	DelegationConfirmed
)

type CheckResult struct {
	Status         Status
	DelegationHash string
	Message        string
}

func ErrorCode(status Status) string {
	switch status {
	case PendingDelegation:
		return "pending_delegation"
	case DelegationConfirmed:
		return "delegation_confirmed"
	default:
		return "not_delegated"
	}
}

func CheckBeforeBuild(ctx context.Context, db *gorm.DB, rpcURL string, wallet storage.Wallet, chain string) CheckResult {
	userID := wallet.UserID
	walletID := wallet.DisplayWalletID
	if walletID == "" {
		walletID = wallet.WalletID
	}
	var state storage.WalletDelegation
	err := db.WithContext(ctx).
		Where("wallet_uid = ? AND chain = ?", wallet.WalletUID, chain).
		First(&state).Error
	if err == nil {
		if state.Delegated && state.Status == "delegated" {
			return CheckResult{Status: OK, DelegationHash: state.DelegationHash}
		}
		if state.Status == "pending_delegation" && state.DelegationHash != "" {
			if rpcURL == "" {
				return CheckResult{
					Status:         PendingDelegation,
					Message:        "Delegation transaction is pending. Please wait for confirmation.",
					DelegationHash: state.DelegationHash,
				}
			}
			return refreshPendingDelegation(ctx, db, rpcURL, state)
		}
	}
	if rpcURL != "" {
		if refreshed := refreshDelegationCode(ctx, db, rpcURL, wallet, walletID, chain); refreshed.Status == OK {
			return refreshed
		}
	}

	var tx storage.Transaction
	err = db.WithContext(ctx).
		Where("user_id = ? AND wallet_id = ? AND tx_type = ? AND status = ? AND chain = ?",
			userID, walletID, "delegate", "pending", chain).
		Order("submitted_at DESC").
		First(&tx).Error
	if err != nil {
		return CheckResult{
			Status:  NotDelegated,
			Message: "Wallet is not EIP-7702 delegated. Fund the wallet with native token and retry signing to initiate delegation automatically.",
		}
	}

	if rpcURL == "" {
		return CheckResult{
			Status:         PendingDelegation,
			Message:        "Delegation transaction is pending. Please wait for confirmation.",
			DelegationHash: tx.Hash,
		}
	}

	client, dialErr := ethclient.DialContext(ctx, rpcURL)
	if dialErr != nil {
		return CheckResult{
			Status:         PendingDelegation,
			Message:        "Network error while checking delegation status. Please retry in a moment.",
			DelegationHash: tx.Hash,
		}
	}
	defer client.Close()

	receipt, receiptErr := client.TransactionReceipt(ctx, common.HexToHash(tx.Hash))
	if receiptErr != nil {
		if errors.Is(receiptErr, ethereum.NotFound) {
			return CheckResult{
				Status:         PendingDelegation,
				Message:        "Delegation transaction is pending on-chain. Please wait for confirmation, or re-delegate if it has been stuck for too long.",
				DelegationHash: tx.Hash,
			}
		}
		return CheckResult{
			Status:         PendingDelegation,
			Message:        "Network error while checking delegation status. Please retry in a moment.",
			DelegationHash: tx.Hash,
		}
	}

	if receipt.Status == 1 {
		confirmedAt := time.Now()
		if header, hErr := client.HeaderByNumber(ctx, receipt.BlockNumber); hErr == nil {
			confirmedAt = time.Unix(int64(header.Time), 0)
		}
		_ = db.WithContext(ctx).Model(&storage.Transaction{}).
			Where("hash = ? AND chain = ?", tx.Hash, chain).
			Updates(map[string]interface{}{
				"status":       "confirmed",
				"confirmed_at": &confirmedAt,
			})
		_ = db.WithContext(ctx).Save(&storage.WalletDelegation{
			WalletUID:      wallet.WalletUID,
			Chain:          chain,
			UserID:         userID,
			WalletID:       walletID,
			Delegated:      true,
			Status:         "delegated",
			DelegationHash: tx.Hash,
			DelegatedAt:    &confirmedAt,
			LastCheckedAt:  &confirmedAt,
		}).Error
		return CheckResult{
			Status:         DelegationConfirmed,
			DelegationHash: tx.Hash,
			Message:        "Delegation confirmed. Retrying...",
		}
	}

	_ = db.WithContext(ctx).Model(&storage.Transaction{}).
		Where("hash = ? AND chain = ?", tx.Hash, chain).
		Updates(map[string]interface{}{"status": "failed"})
	return CheckResult{
		Status:         NotDelegated,
		Message:        "Delegation transaction failed. Fund the wallet with native token and retry signing to initiate delegation automatically.",
		DelegationHash: tx.Hash,
	}
}

func refreshDelegationCode(ctx context.Context, db *gorm.DB, rpcURL string, wallet storage.Wallet, walletID, chain string) CheckResult {
	client, dialErr := ethclient.DialContext(ctx, rpcURL)
	if dialErr != nil {
		return CheckResult{Status: NotDelegated}
	}
	defer client.Close()

	code, err := client.CodeAt(ctx, common.HexToAddress(wallet.Address), nil)
	if err != nil {
		return CheckResult{Status: NotDelegated}
	}
	delegator := parseDelegationDesignator(code)
	if !strings.EqualFold(delegator, vault.MMDelegatorAddress) {
		return CheckResult{Status: NotDelegated}
	}
	now := time.Now().UTC()
	_ = db.WithContext(ctx).Save(&storage.WalletDelegation{
		WalletUID:     wallet.WalletUID,
		Chain:         chain,
		UserID:        wallet.UserID,
		WalletID:      walletID,
		Delegated:     true,
		Delegator:     delegator,
		Status:        "delegated",
		DelegatedAt:   &now,
		LastCheckedAt: &now,
	}).Error
	return CheckResult{Status: OK}
}

func refreshPendingDelegation(ctx context.Context, db *gorm.DB, rpcURL string, state storage.WalletDelegation) CheckResult {
	client, dialErr := ethclient.DialContext(ctx, rpcURL)
	if dialErr != nil {
		return CheckResult{
			Status:         PendingDelegation,
			Message:        "Network error while checking delegation status. Please retry in a moment.",
			DelegationHash: state.DelegationHash,
		}
	}
	defer client.Close()

	txHash := common.HexToHash(state.DelegationHash)
	receipt, receiptErr := client.TransactionReceipt(ctx, txHash)
	if receiptErr != nil {
		if errors.Is(receiptErr, ethereum.NotFound) {
			return CheckResult{
				Status:         PendingDelegation,
				Message:        "Delegation transaction is pending on-chain. Please wait for confirmation, or re-delegate if it has been stuck for too long.",
				DelegationHash: state.DelegationHash,
			}
		}
		return CheckResult{
			Status:         PendingDelegation,
			Message:        "Network error while checking delegation status. Please retry in a moment.",
			DelegationHash: state.DelegationHash,
		}
	}
	now := time.Now()
	state.LastCheckedAt = &now
	if receipt.Status == 1 {
		confirmedAt := now
		if header, hErr := client.HeaderByNumber(ctx, receipt.BlockNumber); hErr == nil {
			confirmedAt = time.Unix(int64(header.Time), 0)
		}
		state.Delegated = true
		state.Status = "delegated"
		state.DelegatedAt = &confirmedAt
		state.LastCheckedAt = &confirmedAt
		_ = db.WithContext(ctx).Save(&state).Error
		_ = db.WithContext(ctx).Model(&storage.Transaction{}).
			Where("hash = ? AND chain = ?", state.DelegationHash, state.Chain).
			Updates(map[string]interface{}{
				"status":       "confirmed",
				"confirmed_at": &confirmedAt,
			})
		return CheckResult{
			Status:         DelegationConfirmed,
			DelegationHash: state.DelegationHash,
			Message:        "Delegation confirmed. Retrying...",
		}
	}

	state.Delegated = false
	state.Status = "failed"
	_ = db.WithContext(ctx).Save(&state).Error
	_ = db.WithContext(ctx).Model(&storage.Transaction{}).
		Where("hash = ? AND chain = ?", state.DelegationHash, state.Chain).
		Updates(map[string]interface{}{"status": "failed"})
	return CheckResult{
		Status:         NotDelegated,
		Message:        "Delegation transaction failed. Fund the wallet with native token and retry signing to initiate delegation automatically.",
		DelegationHash: state.DelegationHash,
	}
}

func parseDelegationDesignator(code []byte) string {
	if len(code) != 23 {
		return ""
	}
	if code[0] != 0xef || code[1] != 0x01 || code[2] != 0x00 {
		return ""
	}
	return "0x" + common.Bytes2Hex(code[3:])
}
