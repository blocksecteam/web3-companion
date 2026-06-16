// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

const registrationDefaultWalletID = "default"

type registrationProvisionStep string

const (
	registrationStepRequest              registrationProvisionStep = "request"
	registrationStepCreateUser           registrationProvisionStep = "create_user"
	registrationStepDefaultPolicy        registrationProvisionStep = "default_policy"
	registrationStepDefaultWallet        registrationProvisionStep = "default_wallet"
	registrationStepDefaultPolicyBinding registrationProvisionStep = "default_policy_binding"
)

type registrationProvisionError struct {
	step registrationProvisionStep
	err  error
}

func (e registrationProvisionError) Error() string {
	return fmt.Sprintf("%s: %v", e.step, e.err)
}

func (e registrationProvisionError) Unwrap() error {
	return e.err
}

type RegistrationProvisioner struct {
	db    *gorm.DB
	vault *vault.Vault
}

type RegistrationProvisionRequest struct {
	UserID string
	Email  string
}

type RegistrationProvisionResult struct {
	UserID string
}

func NewRegistrationProvisioner(db *gorm.DB, v *vault.Vault) *RegistrationProvisioner {
	return &RegistrationProvisioner{db: db, vault: v}
}

func (p *RegistrationProvisioner) Provision(ctx context.Context, req RegistrationProvisionRequest) (RegistrationProvisionResult, error) {
	userID := strings.TrimSpace(req.UserID)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if userID == "" || email == "" {
		return RegistrationProvisionResult{}, registrationProvisionError{step: registrationStepRequest, err: fmt.Errorf("incomplete registration provision request")}
	}
	user := &storage.User{
		ID:    userID,
		Email: email,
		Name:  email,
	}
	if err := p.db.WithContext(ctx).Create(user).Error; err != nil {
		return RegistrationProvisionResult{}, registrationProvisionError{step: registrationStepCreateUser, err: err}
	}

	needsCleanup := true
	defer func() {
		if needsCleanup {
			p.cleanup(ctx, userID)
		}
	}()

	policies := policyservice.NewService(p.db)
	if _, err := policies.EnsureUserDefaultPolicy(userID); err != nil {
		return RegistrationProvisionResult{}, registrationProvisionError{step: registrationStepDefaultPolicy, err: err}
	}
	if p.vault != nil {
		wallet, err := p.vault.CreateWallet(ctx, userID, registrationDefaultWalletID)
		if err != nil {
			return RegistrationProvisionResult{}, registrationProvisionError{step: registrationStepDefaultWallet, err: err}
		}
		if _, err := policies.EnsureDefaultPolicy(userID, wallet.WalletID); err != nil {
			return RegistrationProvisionResult{}, registrationProvisionError{step: registrationStepDefaultPolicyBinding, err: err}
		}
	}
	needsCleanup = false
	return RegistrationProvisionResult{UserID: userID}, nil
}

func (p *RegistrationProvisioner) cleanup(ctx context.Context, userID string) {
	if err := p.cleanupDB(context.WithoutCancel(ctx), userID); err != nil {
		slog.Error("registration: cleanup failed", "user_id", userID, "error", err)
	}
}

func (p *RegistrationProvisioner) cleanupDB(ctx context.Context, userID string) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&storage.Challenge{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&storage.PolicyInstance{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&storage.PolicyDefinitionVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&storage.PolicyDefinition{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&storage.WalletDelegation{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&storage.Wallet{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("id = ?", userID).Delete(&storage.User{}).Error; err != nil {
			return err
		}
		return nil
	})
}
