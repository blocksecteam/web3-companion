// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/config"
)

func RegisterRoutesWithOptions(r *gin.RouterGroup, cfg *config.Config, opts ...func(*Handler)) {
	h := &Handler{cfg: cfg}
	for _, opt := range opts {
		opt(h)
	}
	registerChainRoutes(r, h)
}

func RegisterPublicRoutes(r *gin.RouterGroup, cfg *config.Config) {
	h := &Handler{cfg: cfg}
	registerChainPublicRoutes(r, h)
}

func RegisterAuthenticatedRoutesWithOptions(r *gin.RouterGroup, cfg *config.Config, opts ...func(*Handler)) {
	h := &Handler{cfg: cfg}
	for _, opt := range opts {
		opt(h)
	}
	registerChainAuthenticatedRoutes(r, h)
}

func RegisterBrowserRoutesWithOptions(r *gin.RouterGroup, cfg *config.Config, opts ...func(*Handler)) {
	h := &Handler{cfg: cfg}
	for _, opt := range opts {
		opt(h)
	}
	registerChainBrowserRoutes(r, h)
}

func RegisterCompanionRoutesWithOptions(r *gin.RouterGroup, cfg *config.Config, opts ...func(*Handler)) {
	h := &Handler{cfg: cfg}
	for _, opt := range opts {
		opt(h)
	}
	registerChainCompanionRoutes(r, h)
}

func RegisterRoutes(r *gin.RouterGroup, cfg *config.Config) {
	h := &Handler{cfg: cfg}
	registerChainRoutes(r, h)
}

func registerChainRoutes(r *gin.RouterGroup, h *Handler) {
	registerChainPublicRoutes(r, h)
	registerChainAuthenticatedRoutes(r, h)
}

func registerChainPublicRoutes(r *gin.RouterGroup, h *Handler) {
	ch := r.Group("/chain")
	{
		ch.GET("/supported", h.ListChains)
	}
}

func registerChainAuthenticatedRoutes(r *gin.RouterGroup, h *Handler) {
	registerChainBrowserRoutes(r, h)
	registerChainCompanionRoutes(r, h)
}

func registerChainBrowserRoutes(r *gin.RouterGroup, h *Handler) {
	ch := r.Group("/chain")
	{
		ch.GET("/gas/:chain", h.GetGasPrice)
		ch.GET("/transactions", h.ListTransactions)
		ch.GET("/transaction/:chain/:hash", h.GetTransaction)
	}
}

func registerChainCompanionRoutes(r *gin.RouterGroup, h *Handler) {
	ch := r.Group("/chain")
	{
		ch.POST("/estimate", h.EstimateGas)
		ch.POST("/broadcast", h.BroadcastTx)
		ch.POST("/transfer", h.Transfer)
	}
}
