// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/token"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupTokenRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)

	h := token.NewHandler()
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestToken_Search(t *testing.T) {
	r, apiKey := setupTokenRouter(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/token/search?query=USDC&networks=ethereum", apiKey, nil)

	t.Logf("search response: %d", w.Code)
}

func TestToken_Search_Unauthorized(t *testing.T) {
	r, _ := setupTokenRouter(t)
	w := testutil.DoRequest(r, "GET", "/api/v1/token/search?query=USDC", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestToken_Info(t *testing.T) {
	r, apiKey := setupTokenRouter(t)
	url := "/api/v1/token/info/ethereum/0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	w := testutil.DoAuthRequest(r, "GET", url, apiKey, nil)
	t.Logf("info response: %d", w.Code)
}

func TestToken_Prices_Missing(t *testing.T) {
	r, apiKey := setupTokenRouter(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/token/prices", apiKey, map[string]interface{}{
		"tokens": []map[string]string{},
	})
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}
