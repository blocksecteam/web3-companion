// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/bridge"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupBridgeRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)

	h := bridge.NewHandler(bridge.NewRegistry())
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterPublicRoutes(api)
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestBridge_ListProtocols(t *testing.T) {
	r, apiKey := setupBridgeRouter(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/bridge/protocols", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestBridge_ListProtocols_Unauthorized(t *testing.T) {
	r, _ := setupBridgeRouter(t)
	w := testutil.DoRequest(r, "GET", "/api/v1/bridge/protocols", nil, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestBridge_Quote_MissingFields(t *testing.T) {
	r, apiKey := setupBridgeRouter(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/bridge/quote", apiKey, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}
