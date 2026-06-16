// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
)

type DelegationHandler struct {
	vault    *vault.Vault
	db       *gorm.DB
	passkeys *passkey.Server
	cfg      interface{ ValidateChain(string) error }
}

func NewDelegationHandlerWithConfig(v *vault.Vault, db *gorm.DB, pk *passkey.Server, cfg interface{ ValidateChain(string) error }) *DelegationHandler {
	return &DelegationHandler{vault: v, db: db, passkeys: pk, cfg: cfg}
}

type delegationRequest struct {
	WalletID    string `json:"wallet_id"`
	Chain       string `json:"chain"`
	ChallengeID string `json:"challenge_id,omitempty"`
}

func (h *DelegationHandler) Delegate(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	var req delegationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": err.Error()}})
		return
	}
	walletID := strings.TrimSpace(req.WalletID)
	chain := strings.TrimSpace(req.Chain)
	challengeID := strings.TrimSpace(req.ChallengeID)
	if walletID == "" || chain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "wallet_id and chain are required"}})
		return
	}
	if _, err := h.vault.GetWallet(userID, walletID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "wallet_not_found", "message": err.Error()}})
		return
	}
	if h.cfg != nil {
		if err := h.cfg.ValidateChain(chain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": err.Error()}})
			return
		}
	}

	payload := gin.H{"operation": "delegate_wallet", "wallet_id": walletID, "chain": chain}
	if challengeID == "" {
		// Run the read-only precheck *before* creating the Passkey gate so
		// that callers in a state that cannot proceed (already delegated,
		// wrong delegator, insufficient gas) get the actionable response
		// immediately without burning a Passkey approval.
		preview, err := h.vault.CheckDelegation(c.Request.Context(), chain, userID, walletID)
		if errors.Is(err, vault.ErrWrongDelegator) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{
				"code":    "wrong_delegator",
				"message": "Wallet is delegated to an unsupported EIP-7702 delegator.",
			}})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "delegation_failed", "message": err.Error()}})
			return
		}
		if preview != nil && preview.Delegated {
			c.JSON(http.StatusOK, preview)
			return
		}
		if preview != nil && preview.Status == "pending_delegation" {
			// An earlier delegation tx is still confirming on-chain.
			// Surface the state and let the caller poll; do not burn a new
			// Passkey for a duplicate attempt.
			c.JSON(http.StatusAccepted, preview)
			return
		}
		if preview != nil && preview.NeedsGas {
			body := gin.H{
				"code":         "insufficient_gas",
				"message":      "Wallet needs native token to pay for EIP-7702 delegation gas.",
				"gas_estimate": preview.GasEstimate,
			}
			if preview.CurrentBalance != "" {
				body["current_balance"] = preview.CurrentBalance
			}
			if preview.GasEstimateSource != "" {
				body["gas_estimate_source"] = preview.GasEstimateSource
			}
			c.JSON(http.StatusPaymentRequired, gin.H{"error": body})
			return
		}
		if h.passkeys == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "webauthn_required", "message": "Approved Passkey challenge required"}})
			return
		}
		createGateChallenge(c, h.passkeys, "delegate_wallet", payload)
		return
	}
	if !requireApprovedChallenge(c, h.passkeys, userID, challengeID, "delegate_wallet", payload) {
		return
	}

	result, err := h.vault.Delegate(c.Request.Context(), chain, userID, walletID)
	if errors.Is(err, vault.ErrDelegationPending) {
		logAudit(c, "DELEGATION_PENDING", "INFO", walletID, fmt.Sprintf("pending hash=%s chain=%s", result.DelegationHash, chain))
		c.JSON(http.StatusAccepted, result)
		return
	}
	if errors.Is(err, vault.ErrInsufficientGas) {
		body := gin.H{"code": "insufficient_gas", "message": "Wallet needs native token to pay for EIP-7702 delegation gas."}
		if result != nil && result.GasEstimate != "" {
			body["gas_estimate"] = result.GasEstimate
		}
		if result != nil && result.CurrentBalance != "" {
			body["current_balance"] = result.CurrentBalance
		}
		if result != nil && result.GasEstimateSource != "" {
			body["gas_estimate_source"] = result.GasEstimateSource
		}
		c.JSON(http.StatusPaymentRequired, gin.H{"error": body})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "delegation_failed", "message": err.Error()}})
		return
	}

	logAudit(c, "DELEGATION_INITIATED", "INFO", walletID, fmt.Sprintf("delegated=%v delegator=%s chain=%s", result.Delegated, result.Delegator, chain))
	c.JSON(http.StatusOK, result)
}

// DelegateEstimate is a read-only preview of the delegation precheck:
// resolves the wallet's current delegation state and, when not yet
// delegated, returns the live gas cost and balance so the caller can
// decide whether to proceed (or ask the user to top up first) BEFORE
// burning a Passkey approval. Used by `web3 ssm delegate-estimate`.
func (h *DelegationHandler) DelegateEstimate(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	walletID := strings.TrimSpace(c.Query("wallet_id"))
	chain := strings.TrimSpace(c.Query("chain"))
	if walletID == "" || chain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "wallet_id and chain are required"}})
		return
	}
	if _, err := h.vault.GetWallet(userID, walletID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "wallet_not_found", "message": err.Error()}})
		return
	}
	if h.cfg != nil {
		if err := h.cfg.ValidateChain(chain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": err.Error()}})
			return
		}
	}
	result, err := h.vault.CheckDelegation(c.Request.Context(), chain, userID, walletID)
	if errors.Is(err, vault.ErrWrongDelegator) {
		// Surface the same conflict signal as the write-path; the result
		// payload still carries the offending delegator address.
		c.JSON(http.StatusConflict, gin.H{
			"error":  gin.H{"code": "wrong_delegator", "message": "Wallet is delegated to an unsupported EIP-7702 delegator."},
			"result": result,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "delegation_check_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, result)
}
