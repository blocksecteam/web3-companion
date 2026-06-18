// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package user

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/portfolio"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"gorm.io/gorm"
)

type Handler struct {
	db            *gorm.DB
	vault         *vault.Vault
	passkeys      *passkey.Server
	manager       *companion.Manager
	kms           *vault.KMSClient
	assets        walletAssetInspector
	contextChains []string
}

type HandlerOption func(*Handler)

func RegisterRoutes(r *gin.RouterGroup, db *gorm.DB, v *vault.Vault, pk *passkey.Server, manager *companion.Manager, kms *vault.KMSClient) {
	RegisterRoutesWithOptions(r, db, v, pk, manager, kms)
}

func RegisterRoutesWithOptions(r *gin.RouterGroup, db *gorm.DB, v *vault.Vault, pk *passkey.Server, manager *companion.Manager, kms *vault.KMSClient, opts ...HandlerOption) {
	h := &Handler{db: db, vault: v, passkeys: pk, manager: manager, kms: kms}
	for _, opt := range opts {
		opt(h)
	}
	user := r.Group("/user")
	user.GET("/ssm-status", h.SSMStatus)
	user.GET("/wallets", h.ListWallets)
	user.POST("/wallets", h.CreateWallet)
	user.POST("/wallets/:wallet_id/export", h.ExportWallet)
	user.DELETE("/wallets/:wallet_id", h.DeleteWallet)
	user.PATCH("/wallets/:wallet_id/set-default", h.SetDefaultWallet)
	user.PUT("/wallets/:wallet_id/policy-binding", h.BindWalletPolicy)
	user.GET("/wallets/:wallet_id/policy-usage", h.GetWalletPolicyUsage)
	user.GET("/policy-definitions", h.ListPolicyDefinitions)
	user.POST("/policy-definitions", h.CreatePolicyDefinition)
	user.PUT("/policy-definitions/:policy_definition_id", h.UpdatePolicyDefinition)
	user.DELETE("/policy-definitions/:policy_definition_id", h.DeletePolicyDefinition)
	user.PATCH("/policy-definitions/:policy_definition_id/set-default", h.SetDefaultPolicyDefinition)
}

func WithWalletAssetInspector(inspector *portfolio.AssetInspector) HandlerOption {
	return func(h *Handler) {
		h.assets = inspector
	}
}
func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (h *Handler) SSMStatus(c *gin.Context) {
	mode := "local"
	if h.kms != nil && h.kms.IsEnabled() {
		mode = "aws_kms"
	}
	c.JSON(200, gin.H{"encryption_mode": mode})
}

func errorBody(code, message string) gin.H {
	return gin.H{"error": gin.H{"code": code, "message": message}}
}
func (h *Handler) createChallenge(c *gin.Context, challengeType string, payload interface{}) {
	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, errorBody("webauthn_unavailable", "Passkey server not configured"))
		return
	}
	data, _ := json.Marshal(payload)
	ch, err := h.passkeys.CreateChallenge(auth.GetUserID(c.Request.Context()), challengeType, string(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("challenge_create_failed", err.Error()))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":       "pending_user_auth",
		"challenge_id": ch.ChallengeID,
		"sign_url":     auth.ConfirmURL("challenge=" + url.QueryEscape(ch.ChallengeID)),
		"timeout_at":   ch.ExpiresAt,
	})
}

func (h *Handler) consumeChallenge(c *gin.Context, userID, challengeID, challengeType string, payload interface{}) bool {
	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, errorBody("webauthn_unavailable", "Passkey server not configured"))
		return false
	}
	if err := h.passkeys.ConsumeApprovedChallenge(userID, challengeID, challengeType, payload); err != nil {
		status, code, message := consumeChallengeError(err)
		c.JSON(status, errorBody(code, message))
		return false
	}
	return true
}

func consumeChallengeError(err error) (int, string, string) {
	switch {
	case errors.Is(err, passkey.ErrChallengeExpired):
		return http.StatusGone, "challenge_expired", "Passkey challenge expired"
	case errors.Is(err, passkey.ErrChallengeCancelled):
		return http.StatusGone, "challenge_cancelled", "Passkey challenge was cancelled"
	case errors.Is(err, passkey.ErrChallengeTypeMismatch):
		return http.StatusBadRequest, "challenge_type_mismatch", "Passkey challenge type does not match this operation"
	case errors.Is(err, passkey.ErrChallengePayloadMismatch):
		return http.StatusForbidden, "challenge_payload_mismatch", "Passkey challenge payload does not match this operation"
	case errors.Is(err, passkey.ErrChallengeAlreadyUsed):
		return http.StatusForbidden, "webauthn_required", "Approved Passkey challenge required"
	default:
		return http.StatusForbidden, "webauthn_required", "Approved Passkey challenge required"
	}
}
