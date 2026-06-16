// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package user

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/money"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

const (
	walletDeleteAssetLimitUSD = "0.1"
	walletAssetSnapshotMaxAge = 15 * time.Minute
	defaultWalletPageSize     = 25
)

var (
	errWalletAssetSnapshotMissing = errors.New("wallet asset snapshot required")
	errWalletAssetSnapshotStale   = errors.New("wallet asset snapshot stale")
)

type walletAssetInspector interface {
	TotalUSD(ctx context.Context, address string) (money.Amount, error)
}

type walletRequest struct {
	WalletID     string `json:"wallet_id"`
	Confirmation string `json:"confirmation"`
}

type walletAssetCheck struct {
	TotalUSD   money.Amount
	ObservedAt time.Time
}

func (h *Handler) ListWallets(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	limit := defaultWalletPageSize
	offset := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	wallets, err := h.vault.ListWallets(userID, limit+1, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("wallets_load_failed", err.Error()))
		return
	}
	hasMore := len(wallets) > limit
	if hasMore {
		wallets = wallets[:limit]
	}
	c.JSON(http.StatusOK, gin.H{
		"wallets":  h.walletResponses(userID, wallets),
		"has_more": hasMore,
	})
}

func (h *Handler) walletResponses(userID string, wallets []storage.Wallet) []gin.H {
	responses := make([]gin.H, 0, len(wallets))
	for _, wallet := range wallets {
		responses = append(responses, h.walletResponse(userID, wallet))
	}
	return responses
}

func (h *Handler) walletResponse(userID string, wallet storage.Wallet) gin.H {
	response := gin.H{
		"wallet_id":  walletDisplayID(wallet),
		"wallet_uid": wallet.WalletUID,
		"generation": wallet.Generation,
		"user_id":    wallet.UserID,
		"address":    wallet.Address,
		"is_default": wallet.IsDefault,
		"created_at": wallet.CreatedAt,
	}
	var instance storage.PolicyInstance
	if err := h.db.Where("user_id = ? AND wallet_id = ?", userID, wallet.WalletID).First(&instance).Error; err == nil {
		response["policy_instance_id"] = instance.PolicyInstanceID
		response["policy_definition_id"] = instance.PolicyDefinitionID
		response["policy_definition_version_id"] = instance.PolicyDefinitionVersionID
		var def storage.PolicyDefinition
		if err := h.db.Where("policy_definition_id = ?", instance.PolicyDefinitionID).First(&def).Error; err == nil {
			response["policy_name"] = def.Name
		}
	}
	var delegations []storage.WalletDelegation
	h.db.Where("wallet_uid = ?", wallet.WalletUID).Find(&delegations)
	type chainDelegation struct {
		Chain            string `json:"chain"`
		DelegationStatus string `json:"delegation_status"`
	}
	chains := make([]chainDelegation, 0, len(delegations))
	for _, d := range delegations {
		chains = append(chains, chainDelegation{
			Chain:            d.Chain,
			DelegationStatus: d.Status,
		})
	}
	response["chains"] = chains
	return response
}

func (h *Handler) CreateWallet(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	var req walletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	walletID := strings.TrimSpace(req.WalletID)
	if walletID == "" {
		walletID = "wallet_" + randomID()
	}
	wallet, err := h.vault.CreateWallet(c.Request.Context(), userID, walletID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("wallet_create_failed", err.Error()))
		return
	}
	if err := h.ensureDefaultPolicy(userID, wallet.WalletID); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("default_policy_create_failed", err.Error()))
		return
	}
	c.JSON(http.StatusCreated, h.walletResponse(userID, *wallet))
}

func (h *Handler) ExportWallet(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	walletID := c.Param("wallet_id")
	challengeID := c.Query("challenge_id")
	if _, err := h.vault.GetWallet(userID, walletID); err != nil {
		c.JSON(http.StatusNotFound, errorBody("wallet_not_found", err.Error()))
		return
	}
	payload := gin.H{"operation": "export_wallet", "wallet_id": walletID}
	if challengeID == "" {
		h.createChallenge(c, "export_wallet", payload)
		return
	}
	if !h.consumeChallenge(c, userID, challengeID, "export_wallet", payload) {
		return
	}
	address, privateKey, err := h.vault.ExportPrivateKey(c.Request.Context(), userID, walletID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("wallet_export_failed", err.Error()))
		return
	}
	if err := h.vault.DeleteWallet(c.Request.Context(), userID, walletID); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("wallet_delete_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "exported", "wallet_id": walletID, "address": address, "private_key": privateKey, "deleted": true})
}

func (h *Handler) DeleteWallet(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	walletID := c.Param("wallet_id")
	challengeID := c.Query("challenge_id")
	var req walletRequest
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Confirmation) != walletID {
		c.JSON(http.StatusBadRequest, errorBody("wallet_confirmation_mismatch", "Type the wallet id to delete this wallet"))
		return
	}
	wallet, err := h.vault.GetWallet(userID, walletID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorBody("wallet_not_found", err.Error()))
		return
	}
	assetCheck, err := h.walletAssetCheck(c.Request.Context(), userID, *wallet)
	if err != nil {
		if errors.Is(err, errWalletAssetSnapshotMissing) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{
					"code":    "wallet_asset_snapshot_required",
					"message": "Refresh this wallet portfolio before deleting it",
				},
			})
			return
		}
		if errors.Is(err, errWalletAssetSnapshotStale) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{
					"code":    "wallet_asset_snapshot_stale",
					"message": "Refresh this wallet portfolio before deleting it",
				},
				"observed_at": assetCheck.ObservedAt,
				"total_usd":   assetCheck.TotalUSD,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody("wallet_balance_check_failed", err.Error()))
		return
	}
	if assetCheck.TotalUSD.GreaterThan(money.MustParse(walletDeleteAssetLimitUSD)) {
		c.JSON(http.StatusConflict, gin.H{
			"error":     gin.H{"code": "wallet_has_assets", "message": "Wallet cannot be deleted while assets exceed $0.10"},
			"total_usd": assetCheck.TotalUSD,
		})
		return
	}
	payload := gin.H{"operation": "delete_wallet", "wallet_id": walletID, "confirmation": walletID}
	if challengeID == "" {
		h.createChallenge(c, "delete_wallet", payload)
		return
	}
	if !h.consumeChallenge(c, userID, challengeID, "delete_wallet", payload) {
		return
	}
	if err := h.vault.DeleteWallet(c.Request.Context(), userID, walletID); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("wallet_delete_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "wallet_id": walletID})
}

func (h *Handler) SetDefaultWallet(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	walletID := c.Param("wallet_id")
	if err := h.vault.SetDefaultWallet(userID, walletID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, errorBody("wallet_not_found", "Wallet not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody("set_default_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "wallet_id": walletID})
}

func (h *Handler) walletAssetCheck(ctx context.Context, userID string, wallet storage.Wallet) (walletAssetCheck, error) {
	if h.assets != nil {
		totalUSD, err := h.assets.TotalUSD(ctx, wallet.Address)
		if err != nil {
			return walletAssetCheck{}, err
		}
		now := time.Now()
		snapshot := storage.WalletAssetSnapshot{
			UserID:     userID,
			WalletID:   walletDisplayID(wallet),
			WalletUID:  wallet.WalletUID,
			TotalUSD:   totalUSD,
			ObservedAt: now,
		}
		if err := h.db.Create(&snapshot).Error; err != nil {
			return walletAssetCheck{}, err
		}
		return walletAssetCheck{TotalUSD: totalUSD, ObservedAt: now}, nil
	}

	var snapshot storage.WalletAssetSnapshot
	err := h.db.Where("user_id = ? AND wallet_uid = ?", userID, wallet.WalletUID).Order("observed_at DESC, id DESC").First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = h.db.Where("user_id = ? AND wallet_id = ?", userID, walletDisplayID(wallet)).Order("observed_at DESC, id DESC").First(&snapshot).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return walletAssetCheck{}, errWalletAssetSnapshotMissing
	}
	if err != nil {
		return walletAssetCheck{}, err
	}
	result := walletAssetCheck{TotalUSD: snapshot.TotalUSD, ObservedAt: snapshot.ObservedAt}
	if snapshot.ObservedAt.Before(time.Now().Add(-walletAssetSnapshotMaxAge)) {
		return result, errWalletAssetSnapshotStale
	}
	return result, nil
}

func (h *Handler) ensureDefaultPolicy(userID, walletID string) error {
	_, err := policyservice.NewService(h.db).EnsureDefaultPolicy(userID, walletID)
	return err
}

func walletDisplayID(wallet storage.Wallet) string {
	if strings.TrimSpace(wallet.DisplayWalletID) != "" {
		return strings.TrimSpace(wallet.DisplayWalletID)
	}
	return strings.TrimSpace(wallet.WalletID)
}
