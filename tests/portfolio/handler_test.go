// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package portfolio

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/portfolio"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupPortfolioRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)

	h := portfolio.NewHandler()
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestPortfolio_Get(t *testing.T) {
	r, apiKey := setupPortfolioRouter(t)
	url := "/api/v1/portfolio/ethereum/0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	w := testutil.DoAuthRequest(r, "GET", url, apiKey, nil)
	t.Logf("portfolio response: %d", w.Code)
}

func TestPortfolio_Get_Unauthorized(t *testing.T) {
	r, _ := setupPortfolioRouter(t)
	url := "/api/v1/portfolio/ethereum/0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	w := testutil.DoRequest(r, "GET", url, nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}
