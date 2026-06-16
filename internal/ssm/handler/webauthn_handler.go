// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func RegisterWebAuthnAPIRoutes(r *gin.RouterGroup, s *passkey.Server) {
	RegisterWebAuthnCredentialRoutes(r, s)
	RegisterWebAuthnBrowserRoutes(r, s)
}

func RegisterWebAuthnCredentialRoutes(r *gin.RouterGroup, s *passkey.Server) {
	h := &webAuthnRoutes{server: s}

	wa := r.Group("/ssm/webauthn")
	{
		wa.GET("/credentials", h.ListCredentials)
		wa.DELETE("/credentials/:cred_id", h.DeleteCredential)
	}
}

func RegisterWebAuthnBrowserRoutes(r *gin.RouterGroup, s *passkey.Server) {
	h := &webAuthnRoutes{server: s}
	wa := r.Group("/ssm/webauthn")
	{
		wa.GET("/challenge/:challenge_id", h.GetChallengeStatus)
		wa.POST("/challenge/:challenge_id/cancel", h.CancelChallenge)
	}
}

func RegisterWebAuthnConfirmRoutes(r *gin.Engine, s *passkey.Server) {
	h := &webAuthnRoutes{server: s}
	r.GET("/auth/challenge/:challenge_id", h.PublicChallengeOptions)
	r.POST("/auth/confirm", h.SubmitConfirmation)
}

type webAuthnRoutes struct {
	server *passkey.Server
}

func (h *webAuthnRoutes) PublicChallengeOptions(c *gin.Context) {
	challengeID := c.Param("challenge_id")
	ch, err := h.server.GetChallenge(challengeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "challenge_not_found", "message": err.Error()},
		})
		return
	}
	if err := h.server.RefreshChallengeStatus(ch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "challenge_status_failed", "message": err.Error()},
		})
		return
	}
	if ch.Status != "pending" || ch.Type == "webauthn_register" || ch.Type == "passkey_registration" {
		code, message := unavailableChallengeError(ch.Status)
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": code, "message": message}})
		return
	}
	opts, err := h.server.BeginAuthentication(ch.UserID, challengeID)
	if err != nil {
		status, code := webauthnErrorStatusCode(err)
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"challenge_id": ch.ChallengeID,
		"type":         ch.Type,
		"status":       ch.Status,
		"created_at":   ch.CreatedAt,
		"expires_at":   ch.ExpiresAt,
		"summary":      passkey.SummaryForChallenge(*ch),
		"public_key":   opts,
	})
}

func (h *webAuthnRoutes) ListCredentials(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	creds, err := h.server.GetCredentials(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "internal_error", "message": err.Error()},
		})
		return
	}
	if creds == nil {
		creds = []storage.WebAuthnCredential{}
	}
	c.JSON(http.StatusOK, gin.H{"credentials": creds})
}

func (h *webAuthnRoutes) DeleteCredential(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	credID := c.Param("cred_id")
	if err := h.server.DeleteCredential(userID, credID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "credential_not_found", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *webAuthnRoutes) GetChallengeStatus(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	challengeID := c.Param("challenge_id")
	ch, err := h.server.GetChallenge(challengeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "challenge_not_found", "message": err.Error()},
		})
		return
	}
	if ch.UserID != userID {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "challenge_not_found", "message": "Challenge not found"},
		})
		return
	}
	if err := h.server.RefreshChallengeStatus(ch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "challenge_status_failed", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"challenge_id": ch.ChallengeID,
		"type":         ch.Type,
		"status":       ch.Status,
		"created_at":   ch.CreatedAt,
		"expires_at":   ch.ExpiresAt,
		"summary":      passkey.SummaryForChallenge(*ch),
	})
}

func (h *webAuthnRoutes) CancelChallenge(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	challengeID := c.Param("challenge_id")
	if err := h.server.CancelChallenge(userID, challengeID); err != nil {
		status, code := webauthnErrorStatusCode(err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
			code = "challenge_not_found"
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "challenge_id": challengeID})
}

func (h *webAuthnRoutes) SubmitConfirmation(c *gin.Context) {
	var req struct {
		ChallengeID       string `json:"challenge_id" binding:"required"`
		CredentialID      string `json:"credential_id" binding:"required"`
		ClientDataJSON    string `json:"client_data_json" binding:"required"`
		AuthenticatorData string `json:"authenticator_data" binding:"required"`
		Signature         string `json:"signature" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": err.Error()}})
		return
	}
	ch, err := h.server.GetChallenge(req.ChallengeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "challenge_not_found", "message": err.Error()}})
		return
	}
	if err := h.server.VerifyAssertion(ch.UserID, req.ChallengeID, req.CredentialID, req.ClientDataJSON, req.AuthenticatorData, req.Signature); err != nil {
		status, code := webauthnErrorStatusCode(err)
		if code == "challenge_unavailable" {
			code = "verification_failed"
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	if err := h.server.ApproveChallenge(req.ChallengeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_update_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

func unavailableChallengeError(status string) (string, string) {
	switch status {
	case "timeout":
		return "challenge_expired", "Challenge expired"
	case "cancelled":
		return "challenge_cancelled", "Challenge cancelled"
	case "approved":
		return "challenge_approved", "Challenge already approved"
	case "used", "completed":
		return "challenge_used", "Challenge already used"
	default:
		return "challenge_unavailable", "Challenge expired or already used"
	}
}

func webauthnErrorStatusCode(err error) (int, string) {
	switch {
	case errors.Is(err, passkey.ErrChallengeExpired):
		return http.StatusGone, "challenge_expired"
	case errors.Is(err, passkey.ErrChallengeCancelled):
		return http.StatusGone, "challenge_cancelled"
	case errors.Is(err, passkey.ErrChallengeTypeMismatch):
		return http.StatusBadRequest, "challenge_type_mismatch"
	case errors.Is(err, passkey.ErrChallengeAlreadyUsed):
		return http.StatusConflict, "challenge_unavailable"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, "challenge_not_found"
	default:
		return http.StatusBadRequest, "challenge_unavailable"
	}
}
