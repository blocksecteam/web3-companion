// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/gin-gonic/gin"

	authapi "github.com/blocksecteam/web3-companion/internal/auth/api"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"

	"github.com/blocksecteam/web3-companion/internal/storage"
	"gorm.io/gorm"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"time"
)

var externalURL atomic.Value

type routeConfig struct {
	tokenService         *TokenService
	companionEnvironment CompanionEnvironment
	vault                *vault.Vault
}

type RouteOption func(*routeConfig)

func WithTokenService(svc *TokenService) RouteOption {
	return func(cfg *routeConfig) { cfg.tokenService = svc }
}

func WithVault(v *vault.Vault) RouteOption {
	return func(cfg *routeConfig) { cfg.vault = v }
}

type CompanionEnvironment interface {
	StopUser(ctx context.Context, userID string) error
}

func WithCompanionEnvironment(env CompanionEnvironment) RouteOption {
	return func(cfg *routeConfig) { cfg.companionEnvironment = env }
}

func RegisterRoutes(r *gin.Engine, db *gorm.DB, pk *passkey.Server, opts ...RouteOption) {
	var cfg routeConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.tokenService == nil {
		panic("auth.RegisterRoutes: WithTokenService is required")
	}
	h := &AuthHandler{db: db, passkeys: pk, tokens: cfg.tokenService, companionEnvironment: cfg.companionEnvironment, vault: cfg.vault}

	auth := r.Group("/auth")
	{
		auth.POST("/register", h.Register)
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", h.Logout)
		auth.POST("/container-token", h.ContainerToken)
		auth.POST("/passkey-setup", h.PasskeySetup)
		auth.POST("/passkey-verify", h.PasskeyVerify)
		auth.POST("/passkey-result", h.PasskeyResult)
		auth.POST("/login-options", h.LoginOptions)
		auth.POST("/login-verify", h.LoginVerify)
	}
}

func SetExternalURL(rawURL string) {
	externalURL.Store(strings.TrimSpace(rawURL))
}

type AuthHandler struct {
	db                   *gorm.DB
	passkeys             *passkey.Server
	tokens               *TokenService
	companionEnvironment CompanionEnvironment
	vault                *vault.Vault
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func ConfirmURL(rawQuery string) string {
	values, _ := url.ParseQuery(rawQuery)
	if challengeID := strings.TrimSpace(values.Get("challenge")); challengeID != "" {
		return buildConfirmURL("/confirm/"+url.PathEscape(challengeID), "")
	}
	return buildConfirmURL("/confirm", rawQuery)
}

func buildConfirmURL(path, rawQuery string) string {
	scheme := "http"
	host := "localhost:8080"
	if raw, _ := externalURL.Load().(string); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			if parsed.Scheme == "https" {
				scheme = "https"
			}
			host = parsed.Host
		} else {
			host = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
		}
	}
	if strings.HasPrefix(host, "0.0.0.0:") {
		host = "localhost:" + strings.TrimPrefix(host, "0.0.0.0:")
	} else if host == "0.0.0.0" || host == "" {
		host = "localhost:8080"
	}
	u := url.URL{Scheme: scheme, Host: host, Path: path, RawQuery: rawQuery}
	return u.String()
}

type RegisterRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_email", "message": "A valid email address is required"},
		})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	var existing storage.User
	if err := h.db.Where("email = ?", email).First(&existing).Error; err == nil {
		if existing.PasskeySetup {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"code": "already_registered",
					"message": "This email is already registered. Use your existing Passkey to authenticate."},
			})
			return
		}
		tempToken, err := h.issueRegistrationTempToken(existing.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"code": "registration_failed", "message": "Failed to start Passkey setup"},
			})
			return
		}
		c.JSON(http.StatusOK, authapi.RegisterResponse{
			Status:    "pending_passkey",
			UserID:    existing.ID,
			TempToken: tempToken,
		})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "registration_failed", "message": "Failed to create account"},
		})
		return
	}

	userID := "user_" + generateID()
	result, err := NewRegistrationProvisioner(h.db, h.vault).Provision(c.Request.Context(), RegistrationProvisionRequest{
		UserID: userID,
		Email:  email,
	})
	if err != nil {
		stage, code, message := registrationProvisionErrorResponse(err)
		slog.Error("registration: provision failed",
			"stage", stage,
			"email", email,
			"user_id", userID,
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": code, "message": message},
		})
		return
	}
	if result.UserID != "" {
		userID = result.UserID
	}

	tempToken, err := h.issueRegistrationTempToken(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "registration_failed", "message": "Failed to start Passkey setup"},
		})
		return
	}

	slog.Info("registration: user created", "email", email, "user_id", userID)
	c.JSON(http.StatusCreated, authapi.RegisterResponse{
		Status:    "pending_passkey",
		UserID:    userID,
		TempToken: tempToken,
	})
}

func (h *AuthHandler) issueRegistrationTempToken(userID string) (string, error) {
	tempToken := generateToken()
	expiresAt := time.Now().Add(15 * time.Minute)
	err := h.db.Model(&storage.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"temp_token":            tempToken,
		"temp_token_expires_at": expiresAt,
	}).Error
	return tempToken, err
}

func registrationProvisionErrorResponse(err error) (stage, code, message string) {
	var provisionErr registrationProvisionError
	if !errors.As(err, &provisionErr) {
		slog.Error("registration: create user failed", "error", err)
		return "unknown", "registration_failed", "Failed to create account: " + err.Error()
	}
	switch provisionErr.step {
	case registrationStepCreateUser:
		return string(provisionErr.step), "registration_failed", "Failed to create account: " + provisionErr.err.Error()
	case registrationStepDefaultPolicy:
		return "default_policy", "default_policy_create_failed", "Failed to create default policy"
	case registrationStepDefaultWallet:
		return "default_wallet", "default_wallet_create_failed", "Failed to create default wallet"
	case registrationStepDefaultPolicyBinding:
		return "default_policy_binding", "default_policy_create_failed", "Failed to bind default policy"
	default:
		return string(provisionErr.step), "registration_failed", "Failed to create account: " + provisionErr.err.Error()
	}
}

type LoginOptionsRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h *AuthHandler) LoginOptions(c *gin.Context) {
	var req LoginOptionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_email", "message": "A valid email address is required"},
		})
		return
	}
	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "passkey_unavailable", "message": "Passkey server not configured"},
		})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	var user storage.User
	if err := h.db.Where("email = ?", email).First(&user).Error; err != nil || !user.PasskeySetup {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "login_unavailable", "message": "No Passkey account is registered for this email"},
		})
		return
	}

	ch, err := h.passkeys.CreateChallenge(user.ID, "passkey_login", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "login_challenge_failed", "message": err.Error()},
		})
		return
	}
	opts, err := h.passkeys.BeginAuthentication(user.ID, ch.ChallengeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "login_challenge_failed", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, authapi.LoginOptionsResponse{
		Status:      "ready",
		ChallengeID: ch.ChallengeID,
		UserID:      user.ID,
		PublicKey:   opts,
	})
}

type LoginVerifyRequest struct {
	ChallengeID       string `json:"challenge_id" binding:"required"`
	CredentialID      string `json:"credential_id" binding:"required"`
	ClientDataJSON    string `json:"client_data_json" binding:"required"`
	AuthenticatorData string `json:"authenticator_data" binding:"required"`
	Signature         string `json:"signature" binding:"required"`
}

func (h *AuthHandler) LoginVerify(c *gin.Context) {
	var req LoginVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "challenge_id, credential_id, client_data_json, authenticator_data, and signature are required"},
		})
		return
	}
	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "passkey_unavailable", "message": "Passkey server not configured"},
		})
		return
	}

	var ch storage.Challenge
	if err := h.db.Where("challenge_id = ? AND type = ? AND status = ?", req.ChallengeID, "passkey_login", "pending").First(&ch).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_challenge", "message": "Login challenge not found or already used"},
		})
		return
	}

	if err := h.passkeys.VerifyAssertion(ch.UserID, req.ChallengeID, req.CredentialID, req.ClientDataJSON, req.AuthenticatorData, req.Signature); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "verification_failed", "message": err.Error()},
		})
		return
	}

	var user storage.User
	if err := h.db.Where("id = ?", ch.UserID).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "login_unavailable", "message": "Account is unavailable"},
		})
		return
	}
	if err := h.db.Model(&storage.Challenge{}).Where("challenge_id = ? AND status = ?", ch.ChallengeID, "pending").Update("status", "completed").Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "challenge_update_failed", "message": "Failed to complete login challenge"},
		})
		return
	}
	tokenPair, err := h.tokens.Issue(c.Request.Context(), SessionRequest{
		UserID:       user.ID,
		ClientType:   ClientBrowser,
		Scope:        ScopeBrowser,
		CredentialID: req.CredentialID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "token_issue_failed", "message": "Failed to issue access token"},
		})
		return
	}
	setBrowserRefreshCookie(c, tokenPair.RefreshToken)
	tokenPair.RefreshToken = ""

	c.JSON(http.StatusOK, authapi.LoginVerifyResponse{
		Status:            "authenticated",
		UserID:            user.ID,
		Email:             user.Email,
		CredentialID:      req.CredentialID,
		TokenPairResponse: tokenPair,
	})
}
