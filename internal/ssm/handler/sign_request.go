// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/money"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

func (h *SignHandler) RequestSign(c *gin.Context) {
	var body SignIntentRequest
	if err := decodeSignIntentRequest(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": err.Error()}})
		return
	}
	req := SignRequest{TransactionID: strings.TrimSpace(body.TransactionID), ChallengeID: strings.TrimSpace(body.ChallengeID)}

	ctx := c.Request.Context()
	userID := auth.GetUserID(ctx)
	if req.ChallengeID != "" {
		challengeReq, ok := h.signRequestForApprovedChallenge(c, userID, req.ChallengeID, req)
		if !ok {
			return
		}
		challengeReq.ChallengeID = strings.TrimSpace(req.ChallengeID)
		req = challengeReq
	}
	if !validateSignRequest(c, req) {
		return
	}
	intentSvc := txintent.NewService(h.db, h.vault.KMSClient())
	intent, err := intentSvc.LoadForSigning(ctx, req.TransactionID, userID)
	if errors.Is(err, txintent.ErrIntentExpired) {
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": "transaction_intent_expired", "message": err.Error()}})
		return
	}
	if errors.Is(err, txintent.ErrSecurityNotSatisfied) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "security_check_required", "message": err.Error()}})
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
	challengeID := req.ChallengeID
	req = signRequestFromIntent(intent)
	req.ChallengeID = challengeID
	if intent.Status == txintent.StatusPendingUserAuth && req.ChallengeID == "" {
		// Only reuse intent.ChallengeID when the challenge is still live.
		// CancelChallenge updates the challenge row but does not clear
		// intent.ChallengeID, so a cancelled / timed-out / used / missing
		// challenge must fall through to the policy branch below, which
		// issues a fresh challenge via MarkPendingUserAuth's CAS overwrite.
		// "approved" stays reusable to cover the race where WebAuthn
		// completed just before this re-entry.
		actionable := false
		if h.passkeys != nil && intent.ChallengeID != "" {
			if ch, err := h.passkeys.GetChallengeForUser(userID, intent.ChallengeID); err == nil && ch != nil {
				actionable = (ch.Status == "pending" || ch.Status == "approved") && time.Now().Before(ch.ExpiresAt)
			}
		}
		if actionable {
			c.JSON(http.StatusConflict, SignResponse{
				Status:        "pending_user_auth",
				TransactionID: intent.TransactionID,
				ChallengeID:   intent.ChallengeID,
				SignURL:       auth.ConfirmURL("challenge=" + url.QueryEscape(intent.ChallengeID)),
				TxSummary:     summaryJSONToMap(intent.SummaryJSON),
			})
			return
		}
	}
	wallet, err := h.vault.GetWallet(userID, req.WalletID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "wallet_not_found", "message": err.Error()}})
		return
	}
	if req.Chain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "chain_required", "message": "chain is required for delegation enforcement"}})
		return
	}
	if h.cfg != nil {
		if err := h.cfg.ValidateChain(req.Chain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": err.Error()}})
			return
		}
	}
	if req.Recipient == "" {
		req.Recipient = req.To
	}

	unlock := h.lockWalletChain(req.WalletID, req.Chain)
	defer unlock()

	if !h.ensureDelegatedForSigning(c, intentSvc, userID, req) {
		return
	}

	if err := h.refreshNonce(ctx, userID, &req); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "nonce_fetch_failed", "message": err.Error()}})
		return
	}
	h.enrichFeeParams(ctx, &req)
	tx := buildTx(req)
	if tx == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_tx", "message": "Failed to build transaction"}})
		return
	}

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
	slog.Info("policy evaluate input",
		"transaction_id", intent.TransactionID,
		"tx_type", req.TxType,
		"amount_usd", simAmountUSD.String(),
		"price_unknown", priceUnknown,
	)
	policyResult, err := policyservice.NewService(h.db).Evaluate(policyservice.Request{
		UserID: userID, WalletID: req.WalletID, WalletUID: wallet.WalletUID, TxType: req.TxType,
		AmountUSD: simAmountUSD, Recipient: req.Recipient,
		PriceUnknown: priceUnknown,
	})
	if policyResult != nil {
		slog.Info("policy evaluate result",
			"transaction_id", intent.TransactionID,
			"decision", policyResult.Decision,
			"reason", policyResult.Reason,
		)
	}
	if errors.Is(err, policyservice.ErrNoPolicy) {
		logAudit(c, "SIGN_REJECTED", "HIGH", req.WalletID, "no active policy")
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "policy_not_found", "message": "No active signing policy for wallet"}})
		return
	}
	if errors.Is(err, policyservice.ErrPolicyBlocked) {
		logAudit(c, "SIGN_REJECTED", "HIGH", req.WalletID, policyResult.Reason)
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "policy_rejected", "message": policyResult.Reason}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "policy_error", "message": err.Error()}})
		return
	}
	if policyResult.Decision == policyservice.DecisionWebAuthn {
		if h.passkeys == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "webauthn_unavailable", "message": "WebAuthn server not configured"}})
			return
		}
		if req.ChallengeID != "" {
			if !h.claimApprovedSignChallenge(c, userID, req.ChallengeID) {
				return
			}
			signedTx, err := h.vault.SignTransaction(ctx, userID, req.WalletID, tx, big.NewInt(req.ChainID))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "sign_failed", "message": err.Error()}})
				return
			}
			signedBytes, _ := signedTx.MarshalBinary()
			signedHex := "0x" + common.Bytes2Hex(signedBytes)
			txHash := signedTx.Hash().Hex()
			if err := h.recordSignedOperation(userID, req, policyResult, txHash, policyservice.DecisionWebAuthn); err != nil {
				logAudit(c, "SIGN_FAILED", "HIGH", req.WalletID, "policy usage record failed: "+err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "policy_usage_record_failed", "message": err.Error()}})
				return
			}
			if _, err := intentSvc.StoreSignedTx(ctx, intent.TransactionID, userID, txintent.SignedTx{
				Hex: signedHex, Hash: txHash, PolicyID: policyResult.Policy.PolicyID, PolicyDecision: string(policyservice.DecisionWebAuthn),
			}); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_sign_failed", "message": err.Error()}})
				return
			}
			logAudit(c, "SIGN", "INFO", req.WalletID, "signed approved transaction "+txHash)
			c.JSON(http.StatusOK, SignResponse{Status: "signed", TransactionID: intent.TransactionID, TxHash: txHash})
			return
		}
		payload, _ := json.Marshal(signChallengePayloadFromIntent(intent, req, policyResult.Reason))
		ch, err := h.passkeys.CreateChallenge(userID, "sign", string(payload))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_create_failed", "message": err.Error()}})
			return
		}
		signURL := auth.ConfirmURL("challenge=" + url.QueryEscape(ch.ChallengeID))
		if _, err := intentSvc.MarkPendingUserAuth(ctx, intent.TransactionID, userID, ch.ChallengeID, policyResult.Policy.PolicyID, string(policyservice.DecisionWebAuthn)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_update_failed", "message": err.Error()}})
			return
		}
		logAudit(c, "SIGN", "INFO", req.WalletID, "webauthn required: "+policyResult.Reason)
		c.JSON(http.StatusAccepted, SignResponse{
			Status: "pending_user_auth", TransactionID: intent.TransactionID, ChallengeID: ch.ChallengeID,
			SignURL: signURL, TimeoutAt: &ch.ExpiresAt,
			TxSummary: gin.H{"type": req.TxType, "amount_usd": req.AmountUSD, "to": req.To, "chain": req.Chain, "wallet": wallet.Address},
		})
		return
	}

	chainID := big.NewInt(req.ChainID)
	signedTx, err := h.vault.SignTransaction(ctx, userID, req.WalletID, tx, chainID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "sign_failed", "message": err.Error()}})
		return
	}

	signedBytes, _ := signedTx.MarshalBinary()
	signedHex := "0x" + common.Bytes2Hex(signedBytes)
	txHash := signedTx.Hash().Hex()
	if err := h.recordSignedOperation(userID, req, policyResult, txHash, policyservice.DecisionAuto); err != nil {
		logAudit(c, "SIGN_FAILED", "HIGH", req.WalletID, "policy usage record failed: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "policy_usage_record_failed", "message": err.Error()}})
		return
	}
	if _, err := intentSvc.StoreSignedTx(ctx, intent.TransactionID, userID, txintent.SignedTx{
		Hex: signedHex, Hash: txHash, PolicyID: policyResult.Policy.PolicyID, PolicyDecision: string(policyservice.DecisionAuto),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_sign_failed", "message": err.Error()}})
		return
	}
	logAudit(c, "SIGN_AUTO", "INFO", req.WalletID, "auto signed transaction "+txHash)

	c.JSON(http.StatusOK, SignResponse{Status: "signed", TransactionID: intent.TransactionID, TxHash: txHash})
}
