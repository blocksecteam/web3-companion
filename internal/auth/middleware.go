// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

type contextKey string

const (
	ctxUserID    contextKey = "user_id"
	ctxSessionID contextKey = "session_id"
	ctxClient    contextKey = "client_type"
	ctxScope     contextKey = "scope"
	ctxRequestID contextKey = "request_id"
)

type middlewareConfig struct {
	jwtSecret []byte
}

type MiddlewareOption func(*middlewareConfig)

func WithJWTSecret(secret []byte) MiddlewareOption {
	return func(cfg *middlewareConfig) { cfg.jwtSecret = secret }
}

func WithJWTSecretString(secret string) MiddlewareOption {
	return func(cfg *middlewareConfig) { cfg.jwtSecret = JWTSecretFromString(secret) }
}

func GetUserID(c context.Context) string {
	if v, ok := c.Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}

func GetSessionID(c context.Context) string {
	if v, ok := c.Value(ctxSessionID).(string); ok {
		return v
	}
	return ""
}

func GetClientType(c context.Context) string {
	if v, ok := c.Value(ctxClient).(string); ok {
		return v
	}
	return ""
}

func GetScope(c context.Context) string {
	if v, ok := c.Value(ctxScope).(string); ok {
		return v
	}
	return ""
}

func GetRequestID(c context.Context) string {
	if v, ok := c.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

func WithRequestIdentity(ctx context.Context, userID, sessionID, clientType, scope, requestID string) context.Context {
	ctx = context.WithValue(ctx, ctxUserID, strings.TrimSpace(userID))
	ctx = context.WithValue(ctx, ctxSessionID, strings.TrimSpace(sessionID))
	ctx = context.WithValue(ctx, ctxClient, strings.TrimSpace(clientType))
	ctx = context.WithValue(ctx, ctxScope, strings.TrimSpace(scope))
	ctx = context.WithValue(ctx, ctxRequestID, strings.TrimSpace(requestID))
	return ctx
}

// TestJWTSecret is the secret MiddlewareForTest and tests-that-sign-tokens
// use. Centralising it ensures signer and verifier in tests always match.
// Do NOT use in production code — production callers must read the real
// secret from config via WithJWTSecretString(cfg.Auth.JWTSecret).
const TestJWTSecret = "test-jwt-secret-that-is-long-enough"

// MiddlewareForTest is an auth middleware preconfigured with TestJWTSecret.
// Intended only for use in unit/integration tests that mount routes
// requiring the auth middleware but don't exercise real-token verification.
// Production callers must use Middleware with WithJWTSecret(String) so
// that a missing secret fails fast at boot.
func MiddlewareForTest(db *gorm.DB) gin.HandlerFunc {
	return Middleware(db, WithJWTSecretString(TestJWTSecret))
}

// Middleware builds the auth gin handler. A JWT secret MUST be supplied
// via WithJWTSecret or WithJWTSecretString — calling without one panics
// at construction so that a forgotten secret on a future caller surfaces
// at boot rather than silently authenticating tokens signed with a
// well-known default.
//
// Tests that need an unauthenticated route should use MiddlewareForTest.
func Middleware(db *gorm.DB, opts ...MiddlewareOption) gin.HandlerFunc {
	var cfg middlewareConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(cfg.jwtSecret) == 0 {
		panic("auth.Middleware: WithJWTSecret(String) is required — see MiddlewareForTest for unit tests")
	}
	tokenSvc := NewTokenService(db, cfg.jwtSecret)
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = newRequestID()
		}
		c.Set(string(ctxRequestID), reqID)

		bearer := bearerFromRequest(c)
		if bearer == "" {
			bearer = bearerFromWebSocketProtocol(c)
		}
		if bearer == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "missing_token", "message": "Authorization: Bearer <token> required"},
			})
			return
		}
		if c.GetHeader("Authorization") == "" {
			c.Request.Header.Set("Authorization", "Bearer "+bearer)
		}
		claims, err := tokenSvc.jwtMW.GetClaimsFromJWT(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "invalid_token", "message": "Invalid access token"},
			})
			return
		}
		userID, _ := claims["sub"].(string)
		sessionID, _ := claims["sid"].(string)
		clientType, _ := claims["client"].(string)
		scope, _ := claims["scope"].(string)
		if userID == "" || sessionID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "invalid_token", "message": "Invalid access token"},
			})
			return
		}
		var session storage.AuthSession
		if err := db.Where("session_id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", sessionID, userID, time.Now()).First(&session).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "invalid_session", "message": "Session not found, expired, or revoked"},
			})
			return
		}
		var user storage.User
		if err := db.Select("id").Where("id = ?", userID).First(&user).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "invalid_user", "message": "User is unavailable"},
			})
			return
		}
		ctx := context.WithValue(c.Request.Context(), ctxUserID, userID)
		ctx = context.WithValue(ctx, ctxSessionID, sessionID)
		ctx = context.WithValue(ctx, ctxClient, clientType)
		ctx = context.WithValue(ctx, ctxScope, scope)
		ctx = context.WithValue(ctx, ctxRequestID, reqID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func RequireScope(scopes ...string) gin.HandlerFunc {
	allowed := stringSet(scopes...)
	return func(c *gin.Context) {
		scope := GetScope(c.Request.Context())
		if _, ok := allowed[scope]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "forbidden_scope",
					"message": "Token scope is not allowed for this route",
				},
			})
			return
		}
		c.Next()
	}
}

func RequireClient(clientTypes ...string) gin.HandlerFunc {
	allowed := stringSet(clientTypes...)
	return func(c *gin.Context) {
		clientType := GetClientType(c.Request.Context())
		if _, ok := allowed[clientType]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "forbidden_client",
					"message": "Token client type is not allowed for this route",
				},
			})
			return
		}
		c.Next()
	}
}

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func bearerFromWebSocketProtocol(c *gin.Context) string {
	parts := webSocketProtocolParts(c)
	for index, part := range parts {
		if part == "web3-bearer" && index+1 < len(parts) {
			return strings.TrimSpace(parts[index+1])
		}
	}
	return ""
}

func webSocketProtocolParts(c *gin.Context) []string {
	rawParts := strings.Split(c.GetHeader("Sec-WebSocket-Protocol"), ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func bearerFromRequest(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func newRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
