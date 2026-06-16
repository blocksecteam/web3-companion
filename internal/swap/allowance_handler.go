// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"log/slog"

	"github.com/blocksecteam/web3-companion/internal/token"
)

type CheckAllowanceRequest struct {
	Chain        string `json:"chain"         binding:"required"`
	Token        string `json:"token"         binding:"required"`
	Owner        string `json:"owner"         binding:"required"`
	Spender      string `json:"spender"       binding:"required"`
	Amount       string `json:"amount"        binding:"required"`
	Router       string `json:"router"`
	ApproveStyle string `json:"approve_style,omitempty"`
}

func (h *Handler) CheckAllowance(c *gin.Context) {
	var req CheckAllowanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": err.Error()}})
		return
	}
	if !h.validateChain(c, req.Chain) {
		return
	}

	switch req.ApproveStyle {
	case "", "erc20", "permit2":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code":    "unsupported_style",
			"message": fmt.Sprintf("approve_style must be \"erc20\" or \"permit2\" (got %q)", req.ApproveStyle),
		}})
		return
	}

	if token.IsNativeToken(req.Token) {
		c.JSON(http.StatusOK, gin.H{"approve_calls": []interface{}{}})
		return
	}

	amount := new(big.Int)
	if _, ok := amount.SetString(req.Amount, 0); !ok || amount.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_amount", "message": "amount must be a positive integer"}})
		return
	}

	rpcURL := h.rpcURLForChain(req.Chain)
	if rpcURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": "no RPC URL configured for chain: " + req.Chain}})
		return
	}

	current, err := FetchAllowance(c.Request.Context(), rpcURL, req.Token, req.Owner, req.Spender)
	if err != nil {
		slog.Error("allowance fetch failed", "chain", req.Chain, "token", req.Token, "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "allowance_fetch_failed", "message": err.Error()}})
		return
	}

	calls := GetApproveCalls(req.ApproveStyle, req.Token, req.Spender, req.Router, amount, current)

	type callJSON struct {
		To    string `json:"to"`
		Data  string `json:"data"`
		Value string `json:"value"`
	}
	out := make([]callJSON, len(calls))
	for i, call := range calls {
		v := "0x0"
		if call.Value != nil && call.Value.Sign() > 0 {
			v = fmt.Sprintf("0x%x", call.Value)
		}
		out[i] = callJSON{To: call.To, Data: call.Data, Value: v}
	}
	c.JSON(http.StatusOK, gin.H{
		"approve_calls":     out,
		"current_allowance": current.String(),
	})
}

func (h *Handler) approveCallsForBuild(ctx context.Context, chain, tokenAddr, owner, router string, quote *QuoteResult) ([]ApproveCall, error) {
	if quote == nil || token.IsNativeToken(tokenAddr) {
		return nil, nil
	}
	rpcURL := h.rpcURLForChain(chain)
	if rpcURL == "" {
		return nil, fmt.Errorf("no RPC URL configured for chain: %s", chain)
	}
	amount := new(big.Int)
	if _, ok := amount.SetString(quote.InputAmount, 0); !ok || amount.Sign() <= 0 {
		return nil, fmt.Errorf("invalid approval amount: %s", quote.InputAmount)
	}
	spender := strings.TrimSpace(quote.ApproveTarget)
	if spender == "" {
		spender = router
	}
	if strings.TrimSpace(spender) == "" {
		return nil, fmt.Errorf("approval spender unavailable")
	}
	current, err := FetchAllowance(ctx, rpcURL, tokenAddr, owner, spender)
	if err != nil {
		return nil, err
	}
	return GetApproveCalls(quote.ApproveStyle, tokenAddr, spender, router, amount, current), nil
}

func (h *Handler) rpcURLForChain(chain string) string {
	key := strings.ToLower(strings.TrimSpace(chain))
	if h.rpcProvider != nil {
		return strings.TrimSpace(h.rpcProvider()[key])
	}
	if h.rpcURLs == nil {
		return ""
	}
	return strings.TrimSpace(h.rpcURLs[key])
}
