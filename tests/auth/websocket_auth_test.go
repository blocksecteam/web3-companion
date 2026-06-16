// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	authpkg "github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestMiddlewareRejectsWebSocketProtocolAPIKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	r := gin.New()
	r.GET("/ws/chat", authpkg.MiddlewareForTest(db), func(c *gin.Context) {
		c.String(http.StatusOK, "unexpected")
	})

	w := testutil.DoRequest(r, "GET", "/ws/chat", nil, map[string]string{
		"Sec-WebSocket-Protocol": "web3-api-key," + apiKey,
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ws api key auth = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestMiddlewareAcceptsWebSocketBearerJWT(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	tokenPair, err := authpkg.NewTokenService(db, secret).Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	r.GET("/ws/chat", authpkg.Middleware(db, authpkg.WithJWTSecret(secret)), func(c *gin.Context) {
		c.String(http.StatusOK, authpkg.GetSessionID(c.Request.Context()))
	})

	w := testutil.DoRequest(r, "GET", "/ws/chat", nil, map[string]string{
		"Authorization": "Bearer " + tokenPair.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("ws jwt auth = %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != tokenPair.SessionID {
		t.Fatalf("body = %q, want session id", w.Body.String())
	}
}

func TestMiddlewareAcceptsWebSocketProtocolBearerJWT(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	tokenPair, err := authpkg.NewTokenService(db, secret).Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	r.GET("/ws/chat", authpkg.Middleware(db, authpkg.WithJWTSecret(secret)), func(c *gin.Context) {
		c.String(http.StatusOK, authpkg.GetSessionID(c.Request.Context()))
	})

	w := testutil.DoRequest(r, "GET", "/ws/chat", nil, map[string]string{
		"Sec-WebSocket-Protocol": "web3-bearer," + tokenPair.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("ws jwt subprotocol auth = %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != tokenPair.SessionID {
		t.Fatalf("body = %q, want session id", w.Body.String())
	}
}
