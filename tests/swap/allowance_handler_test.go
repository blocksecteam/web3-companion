// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/token"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func setupAllowanceRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)
	reg := swap.NewRegistry()
	h := swap.NewHandler(reg)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	h.RegisterRoutes(api)
	return r, apiKey
}

func TestSwapCheckAllowance_NativeToken_NeedsNoApprove(t *testing.T) {
	r, apiKey := setupAllowanceRouter(t)

	body := map[string]interface{}{
		"chain":   "ethereum",
		"token":   "native",
		"owner":   "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"spender": "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
		"amount":  "1000000000000000",
	}
	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/check-allowance", apiKey, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	calls, _ := resp["approve_calls"].([]interface{})
	if len(calls) != 0 {
		t.Errorf("native token should need no approval, got %d calls", len(calls))
	}
}

func TestSwapCheckAllowance_MissingFields_Returns400(t *testing.T) {
	r, apiKey := setupAllowanceRouter(t)

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/check-allowance", apiKey, map[string]interface{}{
		"chain": "ethereum",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing fields, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwapCheckAllowance_Permit2Spender_WithRouter_ResponseIncludesPermit2Approve(t *testing.T) {

	r, apiKey := setupAllowanceRouter(t)

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/check-allowance", apiKey, map[string]interface{}{
		"chain":   "ethereum",
		"token":   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"owner":   "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"spender": token.Permit2Address,
		"router":  "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af",
		"amount":  "1000000",
	})

	if w.Code == http.StatusInternalServerError {
		t.Errorf("handler should not panic on Permit2 path, got 500: %s", w.Body.String())
	}
}

func TestSwapCheckAllowance_UnknownApproveStyle_Returns400(t *testing.T) {
	r, apiKey := setupAllowanceRouter(t)

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/swap/check-allowance", apiKey, map[string]interface{}{
		"chain":         "ethereum",
		"token":         "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"owner":         "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"spender":       "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5",
		"amount":        "1000000",
		"approve_style": "permit2_custom",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown approve_style, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj["code"] != "unsupported_style" {
		t.Errorf("expected error code unsupported_style, got %v", errObj["code"])
	}
}
