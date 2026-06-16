// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"net/http"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func TestMiddleware_MissingAuthorization(t *testing.T) {
	db := testutil.NewTestDB(t)
	r := gin.New()
	r.Use(auth.MiddlewareForTest(db))
	r.GET("/api/v1/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := testutil.DoRequest(r, "GET", "/api/v1/test", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestMiddleware_InvalidAPIKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	r := gin.New()
	r.Use(auth.MiddlewareForTest(db))
	r.GET("/api/v1/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/test", "web3_invalid_key", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestMiddleware_WrongPrefix(t *testing.T) {
	db := testutil.NewTestDB(t)
	r := gin.New()
	r.Use(auth.MiddlewareForTest(db))
	r.GET("/api/v1/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := testutil.DoRequest(r, "GET", "/api/v1/test", nil, map[string]string{
		"Authorization": "Token abc123",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestMiddleware_PassesRequestID(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	tokenPair, err := auth.NewTokenService(db, []byte(auth.TestJWTSecret)).Issue(t.Context(), auth.SessionRequest{
		UserID: userID,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	r.Use(auth.Middleware(db, auth.WithJWTSecret([]byte(auth.TestJWTSecret))))
	r.GET("/api/v1/test", func(c *gin.Context) {
		reqID := auth.GetRequestID(c.Request.Context())
		if reqID == "" {
			c.JSON(500, gin.H{"error": "no request_id"})
			return
		}
		c.JSON(200, gin.H{"request_id": reqID})
	})

	w := testutil.DoRequest(r, "GET", "/api/v1/test", nil, map[string]string{
		"Authorization": "Bearer " + tokenPair.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}
