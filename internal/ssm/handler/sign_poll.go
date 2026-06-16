// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/money"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

func (h *SignHandler) PollSign(c *gin.Context) {
	challengeID := c.Param("challenge_id")
	userID := auth.GetUserID(c.Request.Context())

	var ch storage.Challenge
	if err := h.db.Where("challenge_id = ? AND user_id = ? AND type = ?", challengeID, userID, "sign").First(&ch).Error; err != nil {
		var typed storage.Challenge
		if typedErr := h.db.Where("challenge_id = ? AND user_id = ?", challengeID, userID).First(&typed).Error; typedErr == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
				"code":    "wrong_challenge_type",
				"message": "Challenge is for " + typed.Type + "; resume with the matching operation command.",
				"type":    typed.Type,
			}})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "challenge_not_found", "message": "Signing challenge not found"}})
		return
	}
	if time.Now().After(ch.ExpiresAt) && ch.Status != "completed" {
		_ = h.db.Model(&ch).Update("status", "timeout").Error
		c.JSON(http.StatusGone, gin.H{"status": "timeout", "challenge_id": challengeID})
		return
	}
	if ch.Status == "pending" {
		c.JSON(http.StatusOK, gin.H{"status": "pending", "challenge_id": challengeID})
		return
	}
	if ch.Status == "completed" || ch.Status == "used" {
		if resp, ok := completedSignResponse(ch.PayloadJSON); ok {
			c.JSON(http.StatusOK, resp)
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "challenge_completed", "message": "Signing challenge already completed"}})
		return
	}
	if ch.Status != "approved" {
		c.JSON(http.StatusConflict, gin.H{"status": ch.Status, "challenge_id": challengeID})
		return
	}

	claim := h.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND user_id = ? AND type = ? AND status = ?", challengeID, userID, "sign", "approved").
		Update("status", "signing")
	if claim.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_claim_failed", "message": claim.Error.Error()}})
		return
	}
	if claim.RowsAffected != 1 {
		var current storage.Challenge
		if err := h.db.Where("challenge_id = ? AND user_id = ? AND type = ?", challengeID, userID, "sign").First(&current).Error; err == nil {
			if current.Status == "completed" || current.Status == "used" {
				if resp, ok := completedSignResponse(current.PayloadJSON); ok {
					c.JSON(http.StatusOK, resp)
					return
				}
			}
			c.JSON(http.StatusConflict, gin.H{"status": current.Status, "challenge_id": challengeID})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"status": "claimed", "challenge_id": challengeID})
		return
	}

	req, err := signRequestFromChallenge(ch.PayloadJSON)
	if err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_challenge_payload", "message": err.Error()}})
		return
	}
	intentSvc := txintent.NewService(h.db, h.vault.KMSClient())
	intent, err := intentSvc.LoadForSigning(c.Request.Context(), req.TransactionID, userID)
	if errors.Is(err, txintent.ErrIntentExpired) {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": "transaction_intent_expired", "message": err.Error()}})
		return
	}
	if errors.Is(err, txintent.ErrSecurityNotSatisfied) {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "security_check_required", "message": err.Error()}})
		return
	}
	if err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_transaction_intent", "message": err.Error()}})
		return
	}
	req = signRequestFromIntent(intent)
	req.ChallengeID = challengeID
	wallet, err := h.vault.GetWallet(userID, req.WalletID)
	if err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "wallet_not_found", "message": err.Error()}})
		return
	}
	if req.Recipient == "" {
		req.Recipient = req.To
	}

	unlock := h.lockWalletChain(req.WalletID, req.Chain)
	defer unlock()

	var secResult struct {
		Effect *struct {
			TotalSpendingUSD *string `json:"total_spending_usd"`
		} `json:"effect"`
	}
	json.Unmarshal([]byte(intent.SecurityResultJSON), &secResult) //nolint:errcheck
	var simAmountUSD money.Amount
	priceUnknown := true
	if secResult.Effect != nil && secResult.Effect.TotalSpendingUSD != nil {
		if amt, err := money.Parse(*secResult.Effect.TotalSpendingUSD); err == nil {
			simAmountUSD = amt
			priceUnknown = false
		}
	}
	req.AmountUSD = simAmountUSD
	req.PriceUnknown = priceUnknown
	policyResult, err := policyservice.NewService(h.db).Evaluate(policyservice.Request{
		UserID: userID, WalletID: req.WalletID, WalletUID: wallet.WalletUID, TxType: req.TxType,
		AmountUSD: simAmountUSD, Recipient: req.Recipient,
		PriceUnknown: priceUnknown,
	})
	if errors.Is(err, policyservice.ErrNoPolicy) {
		h.failSigningChallenge(challengeID)
		logAudit(c, "SIGN_REJECTED", "HIGH", req.WalletID, "no active policy")
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "policy_not_found", "message": "No active signing policy for wallet"}})
		return
	}
	if errors.Is(err, policyservice.ErrPolicyBlocked) {
		h.failSigningChallenge(challengeID)
		logAudit(c, "SIGN_REJECTED", "HIGH", req.WalletID, policyResult.Reason)
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "policy_rejected", "message": policyResult.Reason}})
		return
	}
	if err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "policy_error", "message": err.Error()}})
		return
	}

	if err := h.refreshNonce(c.Request.Context(), userID, &req); err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "nonce_fetch_failed", "message": err.Error()}})
		return
	}
	h.enrichFeeParams(c.Request.Context(), &req)
	tx := buildTx(req)
	if tx == nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_tx", "message": "Failed to build transaction"}})
		return
	}
	signedTx, err := h.vault.SignTransaction(c.Request.Context(), userID, req.WalletID, tx, big.NewInt(req.ChainID))
	if err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "sign_failed", "message": err.Error()}})
		return
	}

	signedBytes, _ := signedTx.MarshalBinary()
	signedHex := "0x" + common.Bytes2Hex(signedBytes)
	resp := SignResponse{
		Status:        "signed",
		TransactionID: req.TransactionID,
		TxHash:        signedTx.Hash().Hex(),
		TxSummary: gin.H{
			"type": req.TxType, "amount_usd": req.AmountUSD,
			"to": req.To, "chain": req.Chain, "wallet": wallet.Address,
		},
	}
	if err := h.recordSignedOperation(userID, req, policyResult, resp.TxHash, policyservice.DecisionWebAuthn); err != nil {
		h.failSigningChallenge(challengeID)
		logAudit(c, "SIGN_FAILED", "HIGH", req.WalletID, "policy usage record failed: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "policy_usage_record_failed", "message": err.Error()}})
		return
	}
	if _, err := intentSvc.StoreSignedTx(c.Request.Context(), req.TransactionID, userID, txintent.SignedTx{
		Hex: signedHex, Hash: resp.TxHash, PolicyID: policyResult.Policy.PolicyID, PolicyDecision: string(policyservice.DecisionWebAuthn),
	}); err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_sign_failed", "message": err.Error()}})
		return
	}
	updatedPayload, err := withSignResult(ch.PayloadJSON, resp)
	if err != nil {
		h.failSigningChallenge(challengeID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_update_failed", "message": err.Error()}})
		return
	}
	completed := h.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND status = ?", challengeID, "signing").
		Updates(map[string]interface{}{"status": "completed", "payload": updatedPayload})
	if completed.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_update_failed", "message": completed.Error.Error()}})
		return
	}
	if completed.RowsAffected != 1 {
		c.JSON(http.StatusConflict, gin.H{"status": "claimed", "challenge_id": challengeID})
		return
	}
	logAudit(c, "SIGN", "INFO", req.WalletID, "signed approved transaction "+resp.TxHash)
	c.JSON(http.StatusOK, resp)
}

func (h *SignHandler) failSigningChallenge(challengeID string) {
	_ = h.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND status = ?", challengeID, "signing").
		Update("status", "failed").Error
}
