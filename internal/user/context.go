// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package user

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"gorm.io/gorm"
)

const (
	ReadinessReady             = "ready"
	ReadinessNeedsWallet       = "needs_wallet"
	ReadinessPendingDelegation = "pending_delegation"
)

type contextResponse struct {
	Auth            string                `json:"auth"`
	Readiness       string                `json:"readiness"`
	BlockingReason  string                `json:"blocking_reason,omitempty"`
	FrontendActions []string              `json:"frontend_actions,omitempty"`
	Wallets         []contextWalletStatus `json:"wallets"`
}

type contextChainDelegation struct {
	Chain            string `json:"chain"`
	DelegationStatus string `json:"delegation_status"`
	DelegationHash   string `json:"delegation_hash,omitempty"`
}

type contextWalletStatus struct {
	WalletID string                   `json:"wallet_id"`
	Address  string                   `json:"address"`
	Chains   []contextChainDelegation `json:"chains"`
}

func RegisterContextRoutes(r *gin.RouterGroup, db *gorm.DB, chains []string) {
	h := &Handler{db: db, contextChains: normalizedContextChains(chains)}
	r.GET("/users/context", h.ContextStatus)
}

func (h *Handler) ContextStatus(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	var wallets []storage.Wallet
	if err := h.db.Where("user_id = ? AND deleted_at IS NULL", userID).Find(&wallets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "context_failed", "message": err.Error()}})
		return
	}

	resp := contextResponse{
		Auth:      userContextAuthLabel(c),
		Readiness: ReadinessNeedsWallet,
		Wallets:   make([]contextWalletStatus, 0, len(wallets)),
	}
	if len(wallets) == 0 {
		resp.BlockingReason = "No managed wallet is available"
		resp.FrontendActions = []string{"create_wallet"}
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.Readiness = ReadinessReady
	for _, wallet := range wallets {
		var delegations []storage.WalletDelegation
		h.db.WithContext(c.Request.Context()).
			Where("wallet_uid = ?", wallet.WalletUID).
			Find(&delegations)

		chains := h.contextChainDelegations(delegations)
		for _, d := range delegations {
			if d.Status == ReadinessPendingDelegation {
				resp.Readiness = ReadinessPendingDelegation
				resp.BlockingReason = "Delegation transaction is pending confirmation"
				resp.FrontendActions = []string{"wait_for_delegation"}
			}
		}
		resp.Wallets = append(resp.Wallets, contextWalletStatus{
			WalletID: walletDisplayID(wallet),
			Address:  wallet.Address,
			Chains:   chains,
		})
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) contextChainDelegations(delegations []storage.WalletDelegation) []contextChainDelegation {
	byChain := make(map[string]contextChainDelegation, len(delegations))
	for _, d := range delegations {
		chain := strings.ToLower(strings.TrimSpace(d.Chain))
		if chain == "" {
			continue
		}
		byChain[chain] = contextChainDelegation{
			Chain:            chain,
			DelegationStatus: d.Status,
			DelegationHash:   d.DelegationHash,
		}
	}

	if len(h.contextChains) == 0 {
		chains := make([]contextChainDelegation, 0, len(byChain))
		for _, d := range delegations {
			chain := strings.ToLower(strings.TrimSpace(d.Chain))
			if chain == "" {
				continue
			}
			chains = append(chains, contextChainDelegation{
				Chain:            chain,
				DelegationStatus: d.Status,
				DelegationHash:   d.DelegationHash,
			})
		}
		return chains
	}

	chains := make([]contextChainDelegation, 0, len(h.contextChains))
	seen := make(map[string]struct{}, len(h.contextChains))
	for _, chain := range h.contextChains {
		status, ok := byChain[chain]
		if !ok {
			status = contextChainDelegation{Chain: chain, DelegationStatus: "not_delegated"}
		}
		chains = append(chains, status)
		seen[chain] = struct{}{}
	}
	for _, d := range delegations {
		chain := strings.ToLower(strings.TrimSpace(d.Chain))
		if chain == "" {
			continue
		}
		if _, ok := seen[chain]; ok {
			continue
		}
		chains = append(chains, contextChainDelegation{
			Chain:            chain,
			DelegationStatus: d.Status,
			DelegationHash:   d.DelegationHash,
		})
		seen[chain] = struct{}{}
	}
	return chains
}

func normalizedContextChains(chains []string) []string {
	normalized := make([]string, 0, len(chains))
	seen := make(map[string]struct{}, len(chains))
	for _, chain := range chains {
		chain = strings.ToLower(strings.TrimSpace(chain))
		if chain == "" {
			continue
		}
		if _, ok := seen[chain]; ok {
			continue
		}
		seen[chain] = struct{}{}
		normalized = append(normalized, chain)
	}
	return normalized
}

func userContextAuthLabel(c *gin.Context) string {
	client := strings.TrimSpace(auth.GetClientType(c.Request.Context()))
	if client != "" {
		return client
	}
	return "authenticated"
}
