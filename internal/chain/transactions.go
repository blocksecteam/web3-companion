// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

func (h *Handler) ListTransactions(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transactions_unavailable", "message": "Transaction store is unavailable"},
		})
		return
	}
	if auth.GetSessionID(c.Request.Context()) == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{"code": "jwt_required", "message": "Transactions require a browser JWT session"},
		})
		return
	}
	userID := auth.GetUserID(c.Request.Context())
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "missing_user", "message": "Authenticated user is unavailable"},
		})
		return
	}

	txPageSize := 25
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		if n, err := strconv.Atoi(rawLimit); err == nil && n > 0 {
			txPageSize = n
		}
	}

	offset := 0
	if rawOffset := strings.TrimSpace(c.Query("offset")); rawOffset != "" {
		n, err := strconv.Atoi(rawOffset)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"code": "invalid_offset", "message": "offset must be a non-negative integer"},
			})
			return
		}
		offset = n
	}

	chainFilter := strings.TrimSpace(c.Query("chain"))
	statusFilter := strings.TrimSpace(c.Query("status"))

	baseQuery := h.db.WithContext(c.Request.Context()).Where("user_id = ?", userID)
	if chainFilter != "" && chainFilter != "all" {
		baseQuery = baseQuery.Where("chain = ?", chainFilter)
	}
	if statusFilter != "" && statusFilter != "all" {
		baseQuery = baseQuery.Where("status = ?", statusFilter)
	}

	var total int64
	if err := baseQuery.Model(&storage.Transaction{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "transactions_failed", "message": err.Error()},
		})
		return
	}

	var txs []storage.Transaction
	if err := baseQuery.Order("submitted_at DESC").Limit(txPageSize).Offset(offset).Find(&txs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "transactions_failed", "message": err.Error()},
		})
		return
	}

	for i := range txs {
		if txs[i].Status == "pending" {
			h.refreshPendingTransaction(c.Request.Context(), &txs[i])
		}
	}

	resp := gin.H{
		"transactions": transactionResponses(txs),
		"total":        total,
		"offset":       offset,
		"page_size":    txPageSize,
	}

	if offset == 0 {
		type statusRow struct {
			Status string
			Count  int64
		}
		var rows []statusRow
		cntQ := h.db.WithContext(c.Request.Context()).Model(&storage.Transaction{}).Where("user_id = ?", userID)
		if chainFilter != "" && chainFilter != "all" {
			cntQ = cntQ.Where("chain = ?", chainFilter)
		}
		if statusFilter != "" && statusFilter != "all" {
			cntQ = cntQ.Where("status = ?", statusFilter)
		}
		if err := cntQ.Select("status, COUNT(*) as count").Group("status").Scan(&rows).Error; err != nil {
			slog.Warn("transaction counts query failed", "error", err)
		} else {
			counts := map[string]int64{"pending": 0, "confirmed": 0, "failed": 0}
			for _, r := range rows {
				counts[r.Status] = r.Count
			}
			resp["counts"] = counts
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetTransaction(c *gin.Context) {
	chainName := c.Param("chain")
	txHash := c.Param("hash")

	ch := resolveChain(h.cfg, chainName)
	if ch == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "unsupported_chain", "message": "Unsupported chain: " + chainName},
		})
		return
	}

	ctx := c.Request.Context()
	rpcClient := getRPCClient(ch.RPCURL)
	receipt, err := rpcClient.TransactionReceipt(ctx, txHash)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"chain":  ch.Name,
			"hash":   txHash,
			"status": "not_found",
		})
		return
	}

	status := "failed"
	if receipt.Status == 1 {
		status = "confirmed"
	}

	// Best-effort drift recovery: if BroadcastTx's RPC succeeded but the
	// follow-up DB transition failed, the intent is stuck in `broadcasting`.
	// Now that chain confirms the tx is on-chain, advance any matching intent
	// of this user to `submitted`. Silent no-op when there's nothing to
	// reconcile.
	if userID := auth.GetUserID(ctx); userID != "" && h.db != nil {
		if err := txintent.NewService(h.db, nil).MarkSubmittedByHash(ctx, userID, txHash, ch.Name); err != nil {
			slog.Warn("intent drift recovery failed", "txHash", txHash, "chain", ch.Name, "error", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"chain":        ch.Name,
		"hash":         txHash,
		"status":       status,
		"block_number": receipt.BlockNumber,
		"gas_used":     receipt.GasUsed,
	})
}

func (h *Handler) recordBroadcast(ctx context.Context, chainName, txHash string, event *storage.PolicyUsageEvent, txInfo string) {
	if h.db == nil {
		return
	}
	userID := auth.GetUserID(ctx)
	if userID == "" {
		return
	}
	now := time.Now().UTC()
	tx := storage.Transaction{
		Chain:       chainName,
		Hash:        txHash,
		UserID:      userID,
		TxInfo:      txInfo,
		Status:      "pending",
		SubmittedAt: &now,
	}
	if event != nil {
		tx.WalletID = event.WalletID
		tx.WalletUID = event.WalletUID
		tx.TxType = event.TxType
		tx.AmountUSD = event.AmountUSD
		tx.PolicyID = event.PolicyID
		tx.PolicyDecision = event.Decision
		tx.PolicyUsageStatus = event.Status
	}
	if err := h.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&tx).Error; err != nil {
		slog.Warn("record broadcast tx failed", "chain", chainName, "hash", txHash, "error", err)
		return
	}
}

func (h *Handler) refreshPendingTransaction(ctx context.Context, tx *storage.Transaction) {
	ch := resolveChain(h.cfg, tx.Chain)
	if ch == nil {
		return
	}
	receipt, err := h.transactionRPC(ch).TransactionReceipt(ctx, tx.Hash)
	if err != nil {
		return
	}
	tx.Status = "failed"
	if receipt.Status == 1 {
		tx.Status = "confirmed"
	}
	if confirmedAt, err := h.transactionRPC(ch).BlockTimestamp(ctx, receipt.BlockNumber); err == nil {
		tx.ConfirmedAt = confirmedAt
	}
	_ = h.db.WithContext(ctx).
		Model(&storage.Transaction{}).
		Where("chain = ? AND hash = ? AND user_id = ?", tx.Chain, tx.Hash, tx.UserID).
		Updates(map[string]interface{}{
			"status":       tx.Status,
			"confirmed_at": tx.ConfirmedAt,
		}).Error
}

type transactionResponse struct {
	Chain             string       `json:"chain"`
	Hash              string       `json:"hash"`
	Status            string       `json:"status"`
	WalletID          string       `json:"wallet_id,omitempty"`
	WalletUID         string       `json:"wallet_uid,omitempty"`
	TxType            string       `json:"tx_type,omitempty"`
	TxInfo            string       `json:"tx_info,omitempty"`
	AmountUSD         money.Amount `json:"amount_usd,omitempty"`
	PolicyID          string       `json:"policy_id,omitempty"`
	PolicyDecision    string       `json:"policy_decision,omitempty"`
	PolicyUsageStatus string       `json:"policy_usage_status,omitempty"`
	SubmittedAt       *time.Time   `json:"submitted_at"`
	ConfirmedAt       *time.Time   `json:"confirmed_at,omitempty"`
}

func transactionResponses(txs []storage.Transaction) []transactionResponse {
	responses := make([]transactionResponse, 0, len(txs))
	for _, tx := range txs {
		responses = append(responses, transactionResponse{
			Chain:             tx.Chain,
			Hash:              tx.Hash,
			Status:            tx.Status,
			WalletID:          tx.WalletID,
			WalletUID:         tx.WalletUID,
			TxType:            tx.TxType,
			TxInfo:            tx.TxInfo,
			AmountUSD:         tx.AmountUSD,
			PolicyID:          tx.PolicyID,
			PolicyDecision:    tx.PolicyDecision,
			PolicyUsageStatus: tx.PolicyUsageStatus,
			SubmittedAt:       tx.SubmittedAt,
			ConfirmedAt:       tx.ConfirmedAt,
		})
	}
	return responses
}
