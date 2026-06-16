// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/passkey"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func decodeSignIntentRequest(c *gin.Context, req *SignIntentRequest) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.TransactionID) == "" && strings.TrimSpace(req.ChallengeID) == "" {
		return fmt.Errorf("transaction_id is required")
	}
	return nil
}

func (h *SignHandler) signRequestForApprovedChallenge(c *gin.Context, userID, challengeID string, submitted SignRequest) (SignRequest, bool) {
	var ch storage.Challenge
	if err := h.db.Where("challenge_id = ? AND user_id = ? AND type = ? AND status = ?", strings.TrimSpace(challengeID), userID, "sign", "approved").First(&ch).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "challenge_required", "message": err.Error()}})
		return SignRequest{}, false
	}
	if time.Now().After(ch.ExpiresAt) {
		_ = h.db.Model(&ch).Update("status", "timeout").Error
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": "challenge_expired", "message": passkey.ErrChallengeExpired.Error()}})
		return SignRequest{}, false
	}
	req, err := signRequestFromChallenge(ch.PayloadJSON)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_challenge_payload", "message": err.Error()}})
		return SignRequest{}, false
	}
	if submitted.TransactionID != "" && submitted.TransactionID != req.TransactionID {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "challenge_payload_mismatch", "message": passkey.ErrChallengePayloadMismatch.Error()}})
		return SignRequest{}, false
	}
	return req, true
}

func validateSignRequest(c *gin.Context, req SignRequest) bool {
	if strings.TrimSpace(req.TransactionID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "transaction_id is required"}})
		return false
	}
	return true
}

func (h *SignHandler) claimApprovedSignChallenge(c *gin.Context, userID, challengeID string) bool {
	var ch storage.Challenge
	if err := h.db.Where("challenge_id = ? AND user_id = ? AND type = ?", strings.TrimSpace(challengeID), userID, "sign").First(&ch).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "challenge_not_found", "message": "Signing challenge not found"}})
		return false
	}
	if time.Now().After(ch.ExpiresAt) {
		_ = h.db.Model(&ch).Update("status", "timeout").Error
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": "challenge_expired", "message": passkey.ErrChallengeExpired.Error()}})
		return false
	}
	claim := h.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND user_id = ? AND type = ? AND status = ?", ch.ChallengeID, userID, "sign", "approved").
		Update("status", "used")
	if claim.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_claim_failed", "message": claim.Error.Error()}})
		return false
	}
	if claim.RowsAffected != 1 {
		c.JSON(http.StatusConflict, gin.H{"status": ch.Status, "challenge_id": ch.ChallengeID})
		return false
	}
	return true
}

func (h *SignHandler) recordSignedOperation(userID string, req SignRequest, result *policyservice.Result, txHash string, decision policyservice.Decision) error {
	if result == nil || result.Policy == nil {
		return nil
	}
	return policyservice.NewService(h.db).RecordSignedOperation(policyservice.SignedOperation{
		UserID:       userID,
		WalletID:     req.WalletID,
		WalletUID:    result.Policy.WalletUID,
		PolicyID:     result.Policy.PolicyID,
		Chain:        h.canonicalChain(req.Chain),
		TxHash:       txHash,
		TxType:       req.TxType,
		AmountUSD:    req.AmountUSD,
		PriceUnknown: req.PriceUnknown,
		Decision:     decision,
	})
}

func (h *SignHandler) canonicalChain(chain string) string {
	if h.cfg != nil {
		if ch := h.cfg.ChainByName(chain); ch != nil && ch.Name != "" {
			return ch.Name
		}
	}
	return chain
}

func signRequestFromChallenge(payloadJSON string) (SignRequest, error) {
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &envelope); err != nil {
		return SignRequest{}, err
	}
	if len(envelope.Payload) == 0 {
		return SignRequest{}, fmt.Errorf("missing sign payload")
	}
	var req SignRequest
	if err := json.Unmarshal(envelope.Payload, &req); err != nil {
		return SignRequest{}, err
	}
	if req.TransactionID == "" {
		return SignRequest{}, fmt.Errorf("incomplete sign payload")
	}
	return req, nil
}

func SignRequestFromChallenge(payloadJSON string) (SignRequest, error) {
	return signRequestFromChallenge(payloadJSON)
}

func completedSignResponse(payloadJSON string) (SignResponse, bool) {
	var envelope struct {
		Result *SignResponse `json:"result"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &envelope); err != nil || envelope.Result == nil {
		return SignResponse{}, false
	}
	return *envelope.Result, envelope.Result.Status == "signed" && envelope.Result.TransactionID != "" && envelope.Result.TxHash != ""
}

func withSignResult(payloadJSON string, resp SignResponse) (string, error) {
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &envelope); err != nil {
		return "", err
	}
	envelope["result"] = resp
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
