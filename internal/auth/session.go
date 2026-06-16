// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type ContainerTokenRequest struct {
	CredentialID string `json:"credential_id" binding:"required"`
	Secret       string `json:"secret" binding:"required"`
}

const refreshCookieName = "web3_refresh"

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	_ = c.ShouldBindJSON(&req)
	refreshToken := strings.TrimSpace(req.RefreshToken)
	fromCookie := false
	if refreshToken == "" {
		if cookieToken, err := c.Cookie(refreshCookieName); err == nil {
			refreshToken = strings.TrimSpace(cookieToken)
			fromCookie = refreshToken != ""
		}
	}
	if refreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "refresh_token is required"},
		})
		return
	}
	tokenPair, err := h.tokens.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "invalid_refresh_token", "message": "Invalid or expired refresh token"},
		})
		return
	}
	if fromCookie {
		setBrowserRefreshCookie(c, tokenPair.RefreshToken)
		tokenPair.RefreshToken = ""
	}
	c.JSON(http.StatusOK, tokenPair)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req LogoutRequest
	_ = c.ShouldBindJSON(&req)
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		if cookieToken, err := c.Cookie(refreshCookieName); err == nil {
			refreshToken = strings.TrimSpace(cookieToken)
		}
	}
	userID := ""
	if refreshToken != "" {
		userID = h.userIDForRefreshToken(c.Request.Context(), refreshToken)
	}
	if refreshToken != "" {
		if err := h.tokens.RevokeRefreshToken(c.Request.Context(), refreshToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"code": "logout_failed", "message": "Failed to revoke session"},
			})
			return
		}
	}
	if userID != "" {
		now := time.Now()
		if err := h.db.WithContext(c.Request.Context()).Model(&storage.WebSocketTicket{}).
			Where("user_id = ? AND revoked_at IS NULL AND consumed_at IS NULL", userID).
			Update("revoked_at", now).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"code": "logout_failed", "message": "Failed to revoke websocket tickets"},
			})
			return
		}
		if err := h.tokens.RevokeContainerCredentialsForUserScope(c.Request.Context(), userID, ScopeCompanion); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"code": "logout_failed", "message": "Failed to revoke companion credentials"},
			})
			return
		}
		if h.companionEnvironment != nil {
			if err := h.companionEnvironment.StopUser(c.Request.Context(), userID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{"code": "logout_failed", "message": "Failed to stop companion environment"},
				})
				return
			}
		}
	}
	clearBrowserRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

func (h *AuthHandler) userIDForRefreshToken(ctx context.Context, refreshToken string) string {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return ""
	}
	var session storage.AuthSession
	if err := h.db.WithContext(ctx).
		Select("user_id").
		Where("refresh_token_hash = ? AND revoked_at IS NULL", hashRefreshToken(refreshToken)).
		First(&session).Error; err != nil {
		return ""
	}
	return session.UserID
}

func (h *AuthHandler) ContainerToken(c *gin.Context) {
	var req ContainerTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "credential_id and secret are required"},
		})
		return
	}
	tokenPair, err := h.tokens.IssueContainerToken(c.Request.Context(), req.CredentialID, req.Secret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": "invalid_container_credential", "message": "Invalid or revoked container credential"},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": tokenPair.AccessToken,
		"token_type":   tokenPair.TokenType,
		"expires_at":   tokenPair.ExpiresAt,
		"session_id":   tokenPair.SessionID,
		"client":       ClientContainer,
		"scope":        ScopeCompanion,
	})
}

func setBrowserRefreshCookie(c *gin.Context, refreshToken string) {
	if strings.TrimSpace(refreshToken) == "" {
		return
	}
	secure := requestIsHTTPS(c.Request)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookieName,
		Value:    refreshToken,
		Path:     "/auth",
		MaxAge:   int(defaultRefreshTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearBrowserRefreshCookie(c *gin.Context) {
	secure := requestIsHTTPS(c.Request)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func isBrowserAuthRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.TrimSpace(r.Header.Get("Origin")) != "" || strings.TrimSpace(r.Header.Get("Referer")) != ""
}

func requestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
