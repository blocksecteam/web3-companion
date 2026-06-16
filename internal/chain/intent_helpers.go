// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

func (h *Handler) walletForIntent(c *gin.Context, walletID string) (storage.Wallet, bool) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"},
		})
		return storage.Wallet{}, false
	}
	userID := auth.GetUserID(c.Request.Context())
	var wallet storage.Wallet
	if err := h.db.WithContext(c.Request.Context()).
		Where("display_wallet_id = ? AND user_id = ? AND deleted_at IS NULL", strings.TrimSpace(walletID), userID).
		First(&wallet).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "wallet_not_found", "message": "Wallet not found"}})
		return storage.Wallet{}, false
	}
	return wallet, true
}

func walletDisplayID(wallet storage.Wallet) string {
	if strings.TrimSpace(wallet.DisplayWalletID) != "" {
		return strings.TrimSpace(wallet.DisplayWalletID)
	}
	return strings.TrimSpace(wallet.WalletID)
}

func (h *Handler) respondWithIntent(c *gin.Context, req txintent.CreateRequest) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"},
		})
		return
	}
	intent, err := txintent.NewService(h.db, nil).Create(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"transaction_id": intent.TransactionID,
		"status":         intent.Status,
		"summary":        summaryJSONToMap(intent.SummaryJSON),
		"expires_at":     intent.ExpiresAt,
	})
}

func mustSummaryJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func summaryJSONToMap(raw string) interface{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func resolveChain(cfg *config.Config, identifier string) *config.ChainConfig {
	if id, err := strconv.Atoi(identifier); err == nil {
		return cfg.ChainByID(id)
	}
	return cfg.ChainByName(identifier)
}

func weiToGwei(wei *big.Int) string {
	gwei := new(big.Int).Div(wei, big.NewInt(1e9))
	return gwei.String()
}

func weiToEther(wei *big.Int) string {
	ether := new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		new(big.Float).SetFloat64(1e18),
	)
	return ether.Text('f', 18)
}
