// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

func (h *Handler) BroadcastTx(c *gin.Context) {
	var req BroadcastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if strings.TrimSpace(req.SignedTx) != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "raw_signed_tx_disabled", "message": "Raw signed transaction broadcast is disabled; use transaction_id"},
		})
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"},
		})
		return
	}
	ctx := c.Request.Context()
	intentSvc := txintent.NewService(h.db, h.kms)
	userID := auth.GetUserID(ctx)
	intent, err := intentSvc.LoadForBroadcast(ctx, req.TransactionID, userID)
	if errors.Is(err, txintent.ErrIntentExpired) {
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": "transaction_intent_expired", "message": err.Error()}})
		return
	}
	if errors.Is(err, txintent.ErrIntentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "transaction_intent_not_found", "message": err.Error()}})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_transaction_intent", "message": err.Error()}})
		return
	}
	if intent.Status != txintent.StatusSigned {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "transaction_not_signed", "message": "Transaction intent has not been signed"}})
		return
	}
	intent, err = intentSvc.ClaimBroadcast(ctx, intent.TransactionID, userID)
	if errors.Is(err, txintent.ErrInvalidTransition) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "transaction_broadcast_in_progress", "message": "Transaction intent is not available for broadcast"}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_broadcast_claim_failed", "message": err.Error()}})
		return
	}
	signedTx, err := intentSvc.DecryptSignedTx(ctx, intent)
	if err != nil {
		_, _ = intentSvc.ReleaseBroadcastClaim(ctx, intent.TransactionID, userID)
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "signed_tx_unavailable", "message": err.Error()}})
		return
	}
	req.Chain = intent.Chain
	req.SignedTx = signedTx

	ch := resolveChain(h.cfg, req.Chain)
	if ch == nil {
		_, _ = intentSvc.ReleaseBroadcastClaim(ctx, intent.TransactionID, userID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "unsupported_chain", "message": "Unsupported chain: " + req.Chain},
		})
		return
	}

	txHash, err := signedTxHash(req.SignedTx)
	if err != nil {
		_, _ = intentSvc.ReleaseBroadcastClaim(ctx, intent.TransactionID, userID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_signed_tx", "message": err.Error()},
		})
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "policy_usage_unavailable", "message": "Policy usage store is unavailable"},
		})
		return
	}
	policyEvent, err := policyservice.NewService(h.db).SignedOperationForBroadcast(userID, txHash, ch.Name)
	if errors.Is(err, policyservice.ErrSignedOperationNotFound) {
		_, _ = intentSvc.ReleaseBroadcastClaim(ctx, intent.TransactionID, userID)
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{"code": "signed_operation_not_found", "message": "Signed transaction was not produced by an active policy-approved signing flow"},
		})
		return
	}
	if err != nil {
		_, _ = intentSvc.ReleaseBroadcastClaim(ctx, intent.TransactionID, userID)
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{"code": "policy_usage_failed", "message": err.Error()},
		})
		return
	}
	rpcClient := h.transactionRPC(ch)
	broadcastHash, err := rpcClient.SendRawTransaction(ctx, req.SignedTx)
	if err != nil {
		_, _ = intentSvc.ReleaseBroadcastClaim(ctx, intent.TransactionID, userID)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "broadcast_failed", "message": err.Error()},
		})
		return
	}
	if strings.EqualFold(broadcastHash, txHash) {
		txHash = broadcastHash
	}
	// Past this point: tx is on chain. DB write failures must not block the
	// user — the broadcast itself succeeded and the user's `hash` is the
	// real on-chain hash. Any bookkeeping miss here leaves a recoverable
	// drift in intent/policy state but does not change chain truth.
	policyEvent, perr := policyservice.NewService(h.db).MarkOperationSubmitted(userID, txHash, ch.Name)
	if perr != nil {
		policyEvent = nil
	}
	h.recordBroadcast(ctx, ch.Name, txHash, policyEvent, intent.SummaryJSON)
	_, _ = intentSvc.MarkSubmitted(ctx, intent.TransactionID, userID)

	c.JSON(http.StatusOK, gin.H{
		"status":         "submitted",
		"transaction_id": intent.TransactionID,
		"chain":          ch.Name,
		"hash":           txHash,
	})
}

type BroadcastRequest struct {
	TransactionID string `json:"transaction_id" binding:"required"`
	// Chain is overwritten from the loaded intent — the client value is
	// kept on the struct only so the same JSON shape can be parsed when
	// debugging old request payloads.
	Chain string `json:"chain"`
	// SignedTx is filled from DecryptSignedTx; clients are rejected if they
	// try to push a raw signed transaction (see the StatusBadRequest check
	// in BroadcastTx).
	SignedTx string `json:"signed_tx"`
}

func signedTxHash(signedHex string) (string, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(common.FromHex(signedHex)); err != nil {
		return "", err
	}
	return strings.ToLower(tx.Hash().Hex()), nil
}
