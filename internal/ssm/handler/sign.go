// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"gorm.io/gorm"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"sync"
	"time"
)

type SignHandler struct {
	vault    *vault.Vault
	db       *gorm.DB
	passkeys *passkey.Server
	cfg      interface {
		ValidateChain(string) error
		ChainByName(string) *config.ChainConfig
	}
	locks *walletChainLocks
}

func NewSignHandler(v *vault.Vault, db *gorm.DB, pk *passkey.Server) *SignHandler {
	return &SignHandler{vault: v, db: db, passkeys: pk, locks: newWalletChainLocks()}
}

func NewSignHandlerWithConfig(v *vault.Vault, db *gorm.DB, pk *passkey.Server, cfg interface {
	ValidateChain(string) error
	ChainByName(string) *config.ChainConfig
}) *SignHandler {
	return &SignHandler{vault: v, db: db, passkeys: pk, cfg: cfg, locks: newWalletChainLocks()}
}

type SignRequest struct {
	TransactionID        string       `json:"transaction_id,omitempty"`
	WalletID             string       `json:"wallet_id"`
	TxType               string       `json:"tx_type"`
	ChainID              int64        `json:"chain_id"`
	Chain                string       `json:"chain"`
	To                   string       `json:"to"`
	Value                string       `json:"value"`
	Data                 string       `json:"data"`
	GasLimit             uint64       `json:"gas_limit"`
	MaxFeePerGas         string       `json:"max_fee_per_gas"`
	MaxPriorityFeePerGas string       `json:"max_priority_fee_per_gas"`
	Nonce                uint64       `json:"nonce"`
	AmountUSD            money.Amount `json:"amount_usd"`
	PriceUnknown         bool         `json:"price_unknown"`
	Recipient            string       `json:"recipient"`
	ChallengeID          string       `json:"challenge_id,omitempty"`
}

type SignIntentRequest struct {
	TransactionID string `json:"transaction_id,omitempty"`
	ChallengeID   string `json:"challenge_id,omitempty"`
}

type SignChallengePayload struct {
	SignRequest
	Reason      string      `json:"reason,omitempty"`
	PayloadHash string      `json:"payload_hash,omitempty"`
	Security    interface{} `json:"security,omitempty"`
}

type SignResponse struct {
	Status        string      `json:"status"`
	TransactionID string      `json:"transaction_id,omitempty"`
	TxHash        string      `json:"tx_hash,omitempty"`
	ChallengeID   string      `json:"challenge_id,omitempty"`
	SignURL       string      `json:"sign_url,omitempty"`
	TimeoutAt     *time.Time  `json:"timeout_at,omitempty"`
	TxSummary     interface{} `json:"tx_summary,omitempty"`
}

func requireApprovedChallenge(c *gin.Context, pk *passkey.Server, userID, challengeID, challengeType string, payload interface{}) bool {
	if challengeID == "" {
		return false
	}
	if pk == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "webauthn_unavailable", "message": "Passkey server not configured"}})
		return false
	}
	if err := pk.ConsumeApprovedChallenge(userID, challengeID, challengeType, payload); err != nil {
		if err == passkey.ErrChallengeExpired {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "challenge_expired", "message": "WebAuthn challenge expired"}})
			return false
		}
		if err == passkey.ErrChallengePayloadMismatch {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "challenge_payload_mismatch", "message": "Approved WebAuthn challenge does not match this operation"}})
			return false
		}
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "webauthn_required", "message": "Approved WebAuthn challenge required"}})
		return false
	}
	return true
}

func createGateChallenge(c *gin.Context, pk *passkey.Server, challengeType string, payload interface{}) {
	userID := auth.GetUserID(c.Request.Context())
	data, _ := json.Marshal(payload)
	ch, err := pk.CreateChallenge(userID, challengeType, string(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":       "pending_user_auth",
		"challenge_id": ch.ChallengeID,
		"sign_url":     auth.ConfirmURL("challenge=" + url.QueryEscape(ch.ChallengeID)),
		"timeout_at":   ch.ExpiresAt,
	})
}
func (h *SignHandler) ensureDelegatedForSigning(c *gin.Context, intentSvc *txintent.Service, userID string, req SignRequest) bool {
	ctx := c.Request.Context()
	result, err := h.vault.EnsureDelegated(ctx, req.Chain, userID, req.WalletID)
	if err == nil {
		return true
	}
	if errors.Is(err, vault.ErrDelegationPending) {
		h.respondPendingDelegation(c, intentSvc, userID, req, result)
		return false
	}
	if errors.Is(err, vault.ErrNotDelegated) {
		if result != nil && result.NeedsGas {
			body := gin.H{
				"code":         "insufficient_gas",
				"message":      "Wallet needs native token to pay for EIP-7702 delegation gas.",
				"gas_estimate": result.GasEstimate,
			}
			if result.CurrentBalance != "" {
				body["current_balance"] = result.CurrentBalance
			}
			if result.GasEstimateSource != "" {
				body["gas_estimate_source"] = result.GasEstimateSource
			}
			c.JSON(http.StatusPaymentRequired, gin.H{"error": body})
			return false
		}
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code":    "not_delegated",
			"message": "Wallet must be delegated on this chain before signing. Run the explicit delegation flow first.",
		}})
		return false
	}
	if errors.Is(err, vault.ErrWrongDelegator) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code":    "wrong_delegator",
			"message": "Wallet is delegated to an unsupported EIP-7702 delegator.",
		}})
		return false
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "delegation_failed", "message": err.Error()}})
	return false
}

func (h *SignHandler) respondPendingDelegation(c *gin.Context, intentSvc *txintent.Service, userID string, req SignRequest, result *vault.DelegationResult) {
	if result == nil {
		result = &vault.DelegationResult{WalletID: req.WalletID, Status: txintent.StatusPendingDelegation}
	}
	if result.Status == "" {
		result.Status = txintent.StatusPendingDelegation
	}
	if _, err := intentSvc.MarkPendingDelegation(c.Request.Context(), req.TransactionID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_update_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":              txintent.StatusPendingDelegation,
		"transaction_id":      req.TransactionID,
		"delegation":          result,
		"retry_after_seconds": 10,
	})
}
func signRequestFromIntent(intent storage.TransactionIntent) SignRequest {
	return SignRequest{
		TransactionID: intent.TransactionID,
		WalletID:      intent.WalletID,
		TxType:        intent.TxType,
		ChainID:       intent.ChainID,
		Chain:         intent.Chain,
		To:            intent.To,
		Value:         intent.Value,
		Data:          intent.Data,
		GasLimit:      intent.GasLimit,
		Recipient:     intent.Recipient,
	}
}

func summaryJSONToMap(raw string) interface{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func signChallengePayloadFromIntent(intent storage.TransactionIntent, req SignRequest, reason string) SignChallengePayload {
	req.TransactionID = intent.TransactionID
	req.WalletID = intent.WalletID
	req.TxType = intent.TxType
	req.ChainID = intent.ChainID
	req.Chain = intent.Chain
	req.To = intent.To
	req.Value = intent.Value
	req.Data = intent.Data
	req.GasLimit = intent.GasLimit
	req.Recipient = intent.Recipient
	return SignChallengePayload{
		SignRequest: req,
		Reason:      strings.TrimSpace(reason),
		PayloadHash: strings.TrimSpace(intent.PayloadHash),
		Security:    signSecuritySummary(intent),
	}
}

func signSecuritySummary(intent storage.TransactionIntent) interface{} {
	security := map[string]interface{}{}
	if verdict := strings.TrimSpace(intent.SecurityVerdict); verdict != "" {
		security["verdict"] = verdict
	}
	if strings.TrimSpace(intent.SecurityResultJSON) != "" {
		var result struct {
			Checks []struct {
				Rule   string `json:"rule"`
				Status string `json:"status"`
				Detail string `json:"detail"`
			} `json:"checks"`
		}
		if err := json.Unmarshal([]byte(intent.SecurityResultJSON), &result); err == nil && len(result.Checks) > 0 {
			checks := make([]map[string]string, 0, len(result.Checks))
			for _, check := range result.Checks {
				status := strings.ToLower(strings.TrimSpace(check.Status))
				if status != "warn" && status != "fail" {
					continue
				}
				checks = append(checks, map[string]string{
					"rule":   check.Rule,
					"status": status,
					"detail": check.Detail,
				})
			}
			if len(checks) > 0 {
				security["checks"] = checks
			}
		}
	}
	if len(security) == 0 {
		return nil
	}
	return security
}

func (h *SignHandler) refreshNonce(ctx context.Context, userID string, req *SignRequest) error {
	nonce, err := h.vault.PendingNonce(ctx, req.Chain, userID, req.WalletID)
	if err != nil {
		return err
	}
	req.Nonce = nonce
	return nil
}

func (h *SignHandler) lockWalletChain(walletID, chain string) func() {
	if h.locks == nil {
		h.locks = newWalletChainLocks()
	}
	return h.locks.Lock(walletID, chain)
}

type walletChainLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newWalletChainLocks() *walletChainLocks {
	return &walletChainLocks{locks: make(map[string]*sync.Mutex)}
}

func (l *walletChainLocks) Lock(walletID, chain string) func() {
	key := strings.TrimSpace(walletID) + "\x00" + strings.ToLower(strings.TrimSpace(chain))
	l.mu.Lock()
	mu := l.locks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		l.locks[key] = mu
	}
	l.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}
func (h *SignHandler) enrichFeeParams(ctx context.Context, req *SignRequest) {
	if req.MaxFeePerGas != "" && req.MaxPriorityFeePerGas != "" {
		return
	}
	fp := h.vault.FetchFeeParams(ctx, req.Chain)
	if req.MaxFeePerGas == "" {
		req.MaxFeePerGas = fp.MaxFeePerGas.String()
	}
	if req.MaxPriorityFeePerGas == "" {
		req.MaxPriorityFeePerGas = fp.MaxPriorityFeePerGas.String()
	}
}

func buildTx(req SignRequest) *types.Transaction {
	if req.ChainID <= 0 || !common.IsHexAddress(req.To) {
		return nil
	}
	to := common.HexToAddress(req.To)
	value := new(big.Int)
	if req.Value == "" {
		req.Value = "0"
	}
	if _, ok := value.SetString(req.Value, 0); !ok {
		return nil
	}
	data := common.FromHex(req.Data)
	maxFee := new(big.Int)
	if req.MaxFeePerGas != "" {
		if _, ok := maxFee.SetString(req.MaxFeePerGas, 0); !ok {
			return nil
		}
	}
	maxPriorityFee := new(big.Int)
	if req.MaxPriorityFeePerGas != "" {
		if _, ok := maxPriorityFee.SetString(req.MaxPriorityFeePerGas, 0); !ok {
			return nil
		}
	}
	if req.GasLimit == 0 {
		req.GasLimit = 21000
	}
	return types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(req.ChainID),
		Nonce:     req.Nonce,
		GasTipCap: maxPriorityFee,
		GasFeeCap: maxFee,
		Gas:       req.GasLimit,
		To:        &to,
		Value:     value,
		Data:      data,
	})
}
