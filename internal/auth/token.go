// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	ginjwt "github.com/appleboy/gin-jwt/v3"
	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	authapi "github.com/blocksecteam/web3-companion/internal/auth/api"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

const (
	ClientBrowser   = "browser"
	ClientContainer = "container"

	ScopeBrowser   = "browser"
	ScopeCompanion = "companion"

	defaultAccessTTL  = 15 * time.Minute
	defaultRefreshTTL = 72 * time.Hour
)

type SessionRequest struct {
	UserID       string
	ClientType   string
	Scope        string
	CredentialID string
}

type ContainerCredentialResponse struct {
	Version      int    `json:"version"`
	Client       string `json:"client"`
	CredentialID string `json:"credential_id"`
	Secret       string `json:"secret"`
	Scope        string `json:"scope"`
	APIURL       string `json:"api_url,omitempty"`
}

type TokenService struct {
	db         *gorm.DB
	accessTTL  time.Duration
	refreshTTL time.Duration
	jwtSecret  []byte
	jwtMW      *ginjwt.GinJWTMiddleware
}

type TokenServiceOption func(*TokenService)

func WithAccessTTL(ttl time.Duration) TokenServiceOption {
	return func(s *TokenService) { s.accessTTL = ttl }
}

func WithRefreshTTL(ttl time.Duration) TokenServiceOption {
	return func(s *TokenService) { s.refreshTTL = ttl }
}

func JWTSecretFromString(secret string) []byte {
	return []byte(strings.TrimSpace(secret))
}

// NewTokenService constructs a TokenService. The secret must be non-empty
// or construction panics — falling back to a hardcoded dev secret would
// risk callers shipping with a known key. Tests should use
// auth.TestJWTSecret instead of an empty argument.
func NewTokenService(db *gorm.DB, secret []byte, opts ...TokenServiceOption) *TokenService {
	if len(secret) == 0 {
		panic("auth.NewTokenService: secret is required")
	}
	s := &TokenService{
		db:         db,
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
		jwtSecret:  secret,
	}
	for _, opt := range opts {
		opt(s)
	}
	mw, err := ginjwt.New(&ginjwt.GinJWTMiddleware{
		Realm:            "web3-api",
		Key:              secret,
		SigningAlgorithm: "HS256",
		Timeout:          s.accessTTL,
		IdentityKey:      "sub",
		TokenLookup:      "header:Authorization,cookie:web3_access",
		TokenHeadName:    "Bearer",
		PayloadFunc: func(data any) jwt.MapClaims {
			identity, _ := data.(tokenIdentity)
			now := time.Now()
			return jwt.MapClaims{
				"sub":    identity.UserID,
				"sid":    identity.SessionID,
				"aud":    "web3-api",
				"client": identity.ClientType,
				"scope":  identity.Scope,
				"cred":   identity.CredentialID,
				"iat":    now.Unix(),
				"nbf":    now.Unix(),
				"jti":    identity.JWTID,
			}
		},
		Authenticator: func(c *gin.Context) (any, error) {
			return nil, errors.New("passkey authentication required")
		},
		Authorizer: func(c *gin.Context, data any) bool {
			return true
		},
	})
	if err != nil {
		panic(err)
	}
	s.jwtMW = mw
	return s
}

func ConfiguredTokenService(db *gorm.DB, secret string) *TokenService {
	return NewTokenService(db, JWTSecretFromString(secret))
}

func (s *TokenService) Issue(ctx context.Context, req SessionRequest) (authapi.TokenPairResponse, error) {
	req = normalizeSessionRequest(req)
	if err := s.ensureUserActive(ctx, req.UserID); err != nil {
		return authapi.TokenPairResponse{}, err
	}
	refreshToken, err := randomToken()
	if err != nil {
		return authapi.TokenPairResponse{}, err
	}
	sessionID := "sess_" + generateID()
	now := time.Now()
	session := storage.AuthSession{
		SessionID:        sessionID,
		UserID:           req.UserID,
		ClientType:       req.ClientType,
		Scope:            req.Scope,
		CredentialID:     req.CredentialID,
		RefreshTokenHash: hashRefreshToken(refreshToken),
		CreatedAt:        now,
		ExpiresAt:        now.Add(s.refreshTTL),
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return authapi.TokenPairResponse{}, err
	}
	accessToken, expiresAt, err := s.generateAccessToken(tokenIdentityFromSession(session), session.ExpiresAt)
	if err != nil {
		return authapi.TokenPairResponse{}, err
	}
	return authapi.TokenPairResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt.Unix(),
		SessionID:    session.SessionID,
	}, nil
}

func (s *TokenService) Refresh(ctx context.Context, refreshToken string) (authapi.TokenPairResponse, error) {
	hash := hashRefreshToken(refreshToken)
	var session storage.AuthSession
	now := time.Now()
	if err := s.db.WithContext(ctx).
		Where("refresh_token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, now).
		First(&session).Error; err != nil {
		return authapi.TokenPairResponse{}, fmt.Errorf("invalid_refresh_token")
	}
	if err := s.ensureUserActive(ctx, session.UserID); err != nil {
		return authapi.TokenPairResponse{}, fmt.Errorf("invalid_refresh_token")
	}
	newRefreshToken, err := randomToken()
	if err != nil {
		return authapi.TokenPairResponse{}, err
	}
	newHash := hashRefreshToken(newRefreshToken)
	result := s.db.WithContext(ctx).Model(&storage.AuthSession{}).
		Where("session_id = ? AND refresh_token_hash = ? AND revoked_at IS NULL AND expires_at > ?", session.SessionID, hash, now).
		Updates(map[string]interface{}{
			"refresh_token_hash": newHash,
			"last_used_at":       now,
		})
	if result.Error != nil {
		return authapi.TokenPairResponse{}, result.Error
	}
	if result.RowsAffected != 1 {
		return authapi.TokenPairResponse{}, fmt.Errorf("invalid_refresh_token")
	}
	session.RefreshTokenHash = newHash
	session.LastUsedAt = &now
	accessToken, expiresAt, err := s.generateAccessToken(tokenIdentityFromSession(session), session.ExpiresAt)
	if err != nil {
		return authapi.TokenPairResponse{}, err
	}
	return authapi.TokenPairResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt.Unix(),
		SessionID:    session.SessionID,
	}, nil
}

func (s *TokenService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&storage.AuthSession{}).
		Where("refresh_token_hash = ? AND revoked_at IS NULL", hashRefreshToken(refreshToken)).
		Update("revoked_at", now)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (s *TokenService) CreateContainerCredential(ctx context.Context, userID, scope string) (ContainerCredentialResponse, error) {
	userID = strings.TrimSpace(userID)
	scope = strings.TrimSpace(scope)
	if userID == "" {
		return ContainerCredentialResponse{}, fmt.Errorf("user_id_required")
	}
	if scope == "" {
		scope = ScopeCompanion
	}
	secret, err := randomToken()
	if err != nil {
		return ContainerCredentialResponse{}, err
	}
	now := time.Now()
	credential := storage.ContainerCredential{
		CredentialID: "cc_" + generateID(),
		UserID:       userID,
		SecretHash:   hashRefreshToken(secret),
		Scope:        scope,
		CreatedAt:    now,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&storage.ContainerCredential{}).
			Where("user_id = ? AND scope = ? AND revoked_at IS NULL", userID, scope).
			Update("revoked_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&credential).Error
	}); err != nil {
		return ContainerCredentialResponse{}, err
	}
	return ContainerCredentialResponse{
		Version:      1,
		Client:       ClientContainer,
		CredentialID: credential.CredentialID,
		Secret:       secret,
		Scope:        credential.Scope,
	}, nil
}

func (s *TokenService) RevokeContainerCredential(ctx context.Context, credentialID string) error {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return fmt.Errorf("credential_id_required")
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&storage.ContainerCredential{}).
		Where("credential_id = ? AND revoked_at IS NULL", credentialID).
		Update("revoked_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("invalid_container_credential")
	}
	return nil
}

func (s *TokenService) RevokeContainerCredentialsForUserScope(ctx context.Context, userID, scope string) error {
	userID = strings.TrimSpace(userID)
	scope = strings.TrimSpace(scope)
	if userID == "" {
		return fmt.Errorf("user_id_required")
	}
	if scope == "" {
		scope = ScopeCompanion
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&storage.ContainerCredential{}).
		Where("user_id = ? AND scope = ? AND revoked_at IS NULL", userID, scope).
		Update("revoked_at", now)
	return result.Error
}

func (s *TokenService) ValidateContainerCredential(ctx context.Context, userID, scope, credentialID, secret string) error {
	userID = strings.TrimSpace(userID)
	scope = strings.TrimSpace(scope)
	credentialID = strings.TrimSpace(credentialID)
	secret = strings.TrimSpace(secret)
	if userID == "" || credentialID == "" || secret == "" {
		return fmt.Errorf("invalid_container_credential")
	}
	if scope == "" {
		scope = ScopeCompanion
	}
	var credential storage.ContainerCredential
	if err := s.db.WithContext(ctx).
		Where("credential_id = ? AND user_id = ? AND scope = ? AND secret_hash = ? AND revoked_at IS NULL", credentialID, userID, scope, hashRefreshToken(secret)).
		First(&credential).Error; err != nil {
		return fmt.Errorf("invalid_container_credential")
	}
	if err := s.ensureUserActive(ctx, credential.UserID); err != nil {
		return fmt.Errorf("invalid_container_credential")
	}
	return nil
}

func (s *TokenService) IssueContainerToken(ctx context.Context, credentialID, secret string) (authapi.TokenPairResponse, error) {
	credentialID = strings.TrimSpace(credentialID)
	secret = strings.TrimSpace(secret)
	if credentialID == "" || secret == "" {
		return authapi.TokenPairResponse{}, fmt.Errorf("invalid_container_credential")
	}
	var credential storage.ContainerCredential
	if err := s.db.WithContext(ctx).
		Where("credential_id = ? AND secret_hash = ? AND revoked_at IS NULL", credentialID, hashRefreshToken(secret)).
		First(&credential).Error; err != nil {
		return authapi.TokenPairResponse{}, fmt.Errorf("invalid_container_credential")
	}
	if err := s.ensureUserActive(ctx, credential.UserID); err != nil {
		return authapi.TokenPairResponse{}, fmt.Errorf("invalid_container_credential")
	}
	now := time.Now()
	sessionExpiresAt := now.Add(s.accessTTL)
	session := storage.AuthSession{
		SessionID:        "sess_" + generateID(),
		UserID:           credential.UserID,
		ClientType:       ClientContainer,
		Scope:            credential.Scope,
		CredentialID:     credential.CredentialID,
		RefreshTokenHash: hashRefreshToken("container-access-only:" + credential.CredentialID + ":" + generateID()),
		CreatedAt:        now,
		ExpiresAt:        sessionExpiresAt,
		LastUsedAt:       &now,
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return authapi.TokenPairResponse{}, err
	}
	if err := s.db.WithContext(ctx).Model(&storage.ContainerCredential{}).
		Where("credential_id = ?", credential.CredentialID).
		Update("last_used_at", now).Error; err != nil {
		return authapi.TokenPairResponse{}, err
	}
	accessToken, expiresAt, err := s.generateAccessToken(tokenIdentityFromSession(session), session.ExpiresAt)
	if err != nil {
		return authapi.TokenPairResponse{}, err
	}
	return authapi.TokenPairResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt.Unix(),
		SessionID:   session.SessionID,
	}, nil
}

func (s *TokenService) ensureUserActive(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("invalid_user")
	}
	var user storage.User
	if err := s.db.WithContext(ctx).Select("id").Where("id = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("invalid_user")
	}
	return nil
}

func (s *TokenService) Middleware() gin.HandlerFunc {
	return s.jwtMW.MiddlewareFunc()
}

func (s *TokenService) Claims(c *gin.Context) jwt.MapClaims {
	claims, _ := c.Get("JWT_PAYLOAD")
	if typed, ok := claims.(jwt.MapClaims); ok {
		return typed
	}
	return nil
}

func (s *TokenService) generateAccessToken(identity tokenIdentity, notAfter time.Time) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.accessTTL)
	if !notAfter.IsZero() && notAfter.Before(expiresAt) {
		expiresAt = notAfter
	}
	claims := jwt.MapClaims{
		"sub":      identity.UserID,
		"sid":      identity.SessionID,
		"aud":      "web3-api",
		"client":   identity.ClientType,
		"scope":    identity.Scope,
		"cred":     identity.CredentialID,
		"iat":      now.Unix(),
		"nbf":      now.Unix(),
		"jti":      identity.JWTID,
		"exp":      expiresAt.Unix(),
		"orig_iat": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

type tokenIdentity struct {
	UserID       string
	SessionID    string
	ClientType   string
	Scope        string
	CredentialID string
	JWTID        string
}

func tokenIdentityFromSession(session storage.AuthSession) tokenIdentity {
	return tokenIdentity{
		UserID:       session.UserID,
		SessionID:    session.SessionID,
		ClientType:   session.ClientType,
		Scope:        session.Scope,
		CredentialID: session.CredentialID,
		JWTID:        "jti_" + generateID(),
	}
}

func normalizeSessionRequest(req SessionRequest) SessionRequest {
	req.ClientType = strings.TrimSpace(req.ClientType)
	req.Scope = strings.TrimSpace(req.Scope)
	if req.ClientType == "" {
		req.ClientType = ClientBrowser
	}
	if req.Scope == "" {
		switch req.ClientType {
		case ClientBrowser:
			req.Scope = ScopeBrowser
		case ClientContainer:
			req.Scope = ScopeCompanion
		default:
			req.Scope = ScopeBrowser
		}
	}
	return req
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
