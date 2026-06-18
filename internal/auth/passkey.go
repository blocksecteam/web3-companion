// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authapi "github.com/blocksecteam/web3-companion/internal/auth/api"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func tempTokenExpired(user storage.User) bool {
	return user.TempTokenExpiresAt == nil || time.Now().After(*user.TempTokenExpiresAt)
}

type PasskeySetupRequest struct {
	TempToken  string `json:"temp_token" binding:"required"`
	Callback   string `json:"callback_url"`
	ResultMode string `json:"result_mode"`
}

func (h *AuthHandler) PasskeySetup(c *gin.Context) {
	var req PasskeySetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "temp_token is required"},
		})
		return
	}

	var user storage.User
	if err := h.db.Where("temp_token = ?", hashRefreshToken(req.TempToken)).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "invalid_token", "message": "Invalid or expired temp token"},
		})
		return
	}
	if tempTokenExpired(user) {
		c.JSON(http.StatusGone, gin.H{
			"error": gin.H{"code": "temp_token_expired", "message": "Temporary registration token expired"},
		})
		return
	}
	if strings.TrimSpace(req.Callback) != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "callback_removed", "message": "callback_url is no longer supported; use result_mode=claim and /auth/passkey-result"},
		})
		return
	}
	resultMode := strings.TrimSpace(req.ResultMode)
	if resultMode != "" && resultMode != "claim" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_result_mode", "message": "result_mode must be claim when provided"},
		})
		return
	}
	if resultMode == "" {
		resultMode = "claim"
	}

	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "passkey_unavailable", "message": "Passkey server not configured"},
		})
		return
	}

	ch, opts, err := h.passkeys.BeginRegistration(user.ID, user.Email)
	if err != nil {
		slog.Error("passkey: begin registration failed", "user_id", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "passkey_challenge_failed", "message": "Failed to begin passkey registration"},
		})
		return
	}
	creationOpts := gin.H{
		"challenge":              opts.Challenge,
		"rp":                     opts.RP,
		"user":                   opts.User,
		"pubKeyCredParams":       opts.PubKeyCredParams,
		"timeout":                opts.Timeout,
		"attestation":            opts.Attestation,
		"authenticatorSelection": opts.AuthenticatorSelection,
	}
	if resultMode == "claim" {
		creationOpts["result_mode"] = resultMode
	}

	optsJSON, _ := json.Marshal(creationOpts)
	if err := h.db.Model(ch).Updates(map[string]interface{}{
		"type":    "passkey_registration",
		"payload": string(optsJSON),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "passkey_challenge_failed", "message": "Failed to store Passkey challenge"},
		})
		return
	}

	c.JSON(http.StatusOK, authapi.PasskeySetupResponse{
		Status:      "ready",
		ChallengeID: ch.ChallengeID,
		UserID:      user.ID,
		PublicKey:   publicCredentialCreationOptions(creationOpts),
	})
}

type PasskeyVerifyRequest struct {
	ChallengeID       string `json:"challenge_id" binding:"required"`
	CredentialID      string `json:"credential_id" binding:"required"`
	ClientDataJSON    string `json:"client_data_json" binding:"required"`
	AttestationObject string `json:"attestation_object" binding:"required"`
	DeviceName        string `json:"device_name"`
}

func (h *AuthHandler) PasskeyVerify(c *gin.Context) {
	var req PasskeyVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "challenge_id, credential_id, client_data_json, and attestation_object are required"},
		})
		return
	}

	var ch storage.Challenge
	if err := h.db.Where("challenge_id = ? AND type = ? AND status = ?", req.ChallengeID, "passkey_registration", "pending").First(&ch).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_challenge", "message": "Challenge not found or already used"},
		})
		return
	}
	if !ch.ExpiresAt.IsZero() && time.Now().After(ch.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "challenge_expired", "message": "Registration challenge expired"},
		})
		return
	}

	var payload map[string]interface{}
	_ = json.Unmarshal([]byte(ch.PayloadJSON), &payload)
	if err := h.passkeys.CompleteRegistrationWithAttestation(ch.UserID, req.ChallengeID, req.CredentialID, req.ClientDataJSON, req.AttestationObject, req.DeviceName); err != nil {
		slog.Error("passkey: attestation verification failed", "challenge_id", req.ChallengeID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_attestation", "message": "Passkey verification failed"},
		})
		return
	}

	updates := map[string]interface{}{
		"passkey_setup": true,
	}
	if err := h.db.Model(&storage.User{}).Where("id = ?", ch.UserID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "registration_failed", "message": "Failed to complete Passkey registration"},
		})
		return
	}

	c.JSON(http.StatusOK, authapi.PasskeyVerifyResponse{
		Status:       "registered",
		UserID:       ch.UserID,
		CredentialID: req.CredentialID,
		ResultStatus: "claimable",
	})
}

type PasskeyResultRequest struct {
	TempToken   string `json:"temp_token" binding:"required"`
	ChallengeID string `json:"challenge_id" binding:"required"`
}

func (h *AuthHandler) PasskeyResult(c *gin.Context) {
	var req PasskeyResultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "temp_token and challenge_id are required"},
		})
		return
	}

	var ch storage.Challenge
	if err := h.db.Where("challenge_id = ? AND type = ?", req.ChallengeID, "passkey_registration").First(&ch).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_challenge", "message": "Registration challenge not found"},
		})
		return
	}
	if !ch.ExpiresAt.IsZero() && time.Now().After(ch.ExpiresAt) {
		c.JSON(http.StatusGone, gin.H{
			"error": gin.H{"code": "challenge_expired", "message": "Registration challenge expired"},
		})
		return
	}

	var user storage.User
	hashedTemp := hashRefreshToken(req.TempToken)
	if err := h.db.Where("id = ? AND temp_token = ?", ch.UserID, hashedTemp).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "invalid_token", "message": "Invalid or already redeemed temp token"},
		})
		return
	}
	if tempTokenExpired(user) {
		c.JSON(http.StatusGone, gin.H{
			"error": gin.H{"code": "temp_token_expired", "message": "Temporary registration token expired"},
		})
		return
	}

	if ch.Status != "approved" {
		c.JSON(http.StatusAccepted, authapi.StatusResponse{Status: "pending"})
		return
	}
	credentialID := ""
	var cred storage.WebAuthnCredential
	if err := h.db.Where("user_id = ?", user.ID).Order("created_at DESC").First(&cred).Error; err == nil {
		credentialID = cred.CredentialID
	}

	claimed := false
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&storage.User{}).Where("id = ? AND temp_token = ? AND temp_token_expires_at > ?", user.ID, hashedTemp, time.Now()).Updates(map[string]interface{}{
			"temp_token":            "",
			"temp_token_expires_at": nil,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		claimed = true
		return tx.Model(&storage.Challenge{}).Where("challenge_id = ? AND status = ?", ch.ChallengeID, "approved").Update("status", "completed").Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "result_claim_failed", "message": "Failed to finalize registration result"},
		})
		return
	}
	if !claimed {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "invalid_token", "message": "Invalid or already redeemed temp token"},
		})
		return
	}
	tokenPair, err := h.tokens.Issue(c.Request.Context(), SessionRequest{
		UserID:       user.ID,
		ClientType:   ClientBrowser,
		Scope:        ScopeBrowser,
		CredentialID: credentialID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "token_issue_failed", "message": "Failed to issue access token"},
		})
		return
	}
	if isBrowserAuthRequest(c.Request) {
		setBrowserRefreshCookie(c, tokenPair.RefreshToken)
		tokenPair.RefreshToken = ""
	}

	c.JSON(http.StatusOK, authapi.PasskeyResultResponse{
		Status:            "saved",
		UserID:            user.ID,
		CredentialID:      credentialID,
		TokenPairResponse: tokenPair,
	})
}

func publicCredentialCreationOptions(payload gin.H) gin.H {
	opts := gin.H{}
	for k, v := range payload {
		if k == "result_mode" {
			continue
		}
		opts[k] = v
	}
	return opts
}
