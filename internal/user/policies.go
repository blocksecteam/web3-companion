// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package user

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	auditservice "github.com/blocksecteam/web3-companion/internal/audit"
	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/money"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

const defaultPolicyPageSize = 25

type policyBindingRequest struct {
	PolicyDefinitionID string `json:"policy_definition_id"`
	ChallengeID        string `json:"challenge_id"`
}

type policyRequest struct {
	WalletID                  string       `json:"wallet_id"`
	Name                      string       `json:"name"`
	Mode                      string       `json:"mode"`
	MaxPerTransactionUSD      money.Amount `json:"max_per_transaction_usd"`
	DailyTransferLimitUSD     money.Amount `json:"daily_transfer_limit_usd"`
	DailyTradeLimitUSD        money.Amount `json:"daily_trade_limit_usd"`
	AutoSignEnabled           bool         `json:"auto_sign_enabled"`
	AutoSignMaxPerTxUSD       money.Amount `json:"auto_sign_max_per_tx_usd"`
	AutoSignDailyLimitUSD     money.Amount `json:"auto_sign_daily_limit_usd"`
	AutoSignMaxCountDay       int          `json:"auto_sign_max_count_day"`
	RecipientWhitelistEnabled bool         `json:"recipient_whitelist_enabled"`
	RecipientWhitelistAddrs   []string     `json:"recipient_whitelist_addrs"`
	ChallengeID               string       `json:"challenge_id"`
}

func (h *Handler) BindWalletPolicy(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	walletID := c.Param("wallet_id")
	var req policyBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	definitionID := strings.TrimSpace(req.PolicyDefinitionID)
	if definitionID == "" {
		c.JSON(http.StatusBadRequest, errorBody("policy_definition_required", "Policy definition is required"))
		return
	}
	if _, err := h.vault.GetWallet(userID, walletID); err != nil {
		c.JSON(http.StatusNotFound, errorBody("wallet_not_found", err.Error()))
		return
	}
	var def storage.PolicyDefinition
	if err := h.db.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", definitionID, userID).First(&def).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, errorBody("policy_definition_not_found", "Policy definition not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_load_failed", err.Error()))
		return
	}
	payload := gin.H{"operation": "bind_wallet_policy", "wallet_id": walletID, "policy_definition_id": definitionID}
	if req.ChallengeID == "" {
		h.createChallenge(c, "policy_change", payload)
		return
	}
	if !h.consumeChallenge(c, userID, req.ChallengeID, "policy_change", payload) {
		return
	}
	instance, err := policyservice.NewService(h.db).BindPolicyDefinition(userID, walletID, definitionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, errorBody("policy_definition_not_found", "Policy definition not found"))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_binding_failed", err.Error()))
		return
	}
	h.recordPolicyDefinitionAudit(c, "POLICY_INSTANCE_BOUND", definitionID, gin.H{
		"policy_instance_id":            instance.PolicyInstanceID,
		"wallet_id":                     walletID,
		"policy_definition_id":          definitionID,
		"policy_definition_version_id":  instance.PolicyDefinitionVersionID,
		"previous_policy_definition_id": "",
	})
	c.JSON(http.StatusOK, instance)
}

func (h *Handler) ListPolicyDefinitions(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	limit := defaultPolicyPageSize
	offset := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	svc := policyservice.NewService(h.db)
	if _, err := svc.EnsureUserDefaultPolicy(userID); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("default_policy_create_failed", err.Error()))
		return
	}
	defs, err := svc.ListPolicyDefinitionResponses(userID, limit+1, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_load_failed", err.Error()))
		return
	}
	hasMore := len(defs) > limit
	if hasMore {
		defs = defs[:limit]
	}
	resp := gin.H{
		"policy_definitions": defs,
		"has_more":           hasMore,
	}

	if offset == 0 {
		if summary, err := svc.DefinitionSummary(userID); err != nil {
			slog.Warn("policy definition summary query failed", "error", err)
		} else {
			resp["summary"] = gin.H{
				"manual":       summary.Manual,
				"auto":         summary.Auto,
				"default_name": summary.DefaultName,
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) CreatePolicyDefinition(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	var req policyRequest
	requestBody, err := bindPolicyJSON(c, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	input, ok := h.validatePolicyDefinitionInput(c, req)
	if !ok {
		return
	}
	payload := gin.H{"operation": "create_policy_definition", "request": requestBody}
	if req.ChallengeID == "" {
		h.createChallenge(c, "policy_change", payload)
		return
	}
	if !h.consumeChallenge(c, userID, req.ChallengeID, "policy_change", payload) {
		return
	}
	def, err := policyservice.NewService(h.db).CreatePolicyDefinition(userID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_create_failed", err.Error()))
		return
	}
	h.recordPolicyDefinitionAudit(c, "POLICY_DEFINITION_CREATED", def.PolicyDefinitionID, gin.H{
		"policy_definition_id": def.PolicyDefinitionID,
		"current_version_id":   def.CurrentVersionID,
		"name":                 def.Name,
	})
	resp, err := policyservice.NewService(h.db).ResponseForDefinition(*def)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_load_failed", err.Error()))
		return
	}
	c.JSON(http.StatusCreated, resp)
}

func (h *Handler) UpdatePolicyDefinition(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	definitionID := c.Param("policy_definition_id")
	var req policyRequest
	requestBody, err := bindPolicyJSON(c, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	input, ok := h.validatePolicyDefinitionInput(c, req)
	if !ok {
		return
	}
	payload := gin.H{"operation": "update_policy_definition", "policy_definition_id": definitionID, "request": requestBody}
	if req.ChallengeID == "" {
		h.createChallenge(c, "policy_change", payload)
		return
	}
	if !h.consumeChallenge(c, userID, req.ChallengeID, "policy_change", payload) {
		return
	}
	def, err := policyservice.NewService(h.db).UpdatePolicyDefinition(userID, definitionID, input)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, errorBody("policy_definition_not_found", "Policy definition not found"))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_update_failed", err.Error()))
		return
	}
	h.recordPolicyDefinitionAudit(c, "POLICY_DEFINITION_UPDATED", def.PolicyDefinitionID, gin.H{
		"policy_definition_id": def.PolicyDefinitionID,
		"current_version_id":   def.CurrentVersionID,
		"name":                 def.Name,
		"usage_refreshed":      true,
	})
	resp, err := policyservice.NewService(h.db).ResponseForDefinition(*def)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_load_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeletePolicyDefinition(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	definitionID := c.Param("policy_definition_id")
	challengeID := c.Query("challenge_id")
	if deleteCheck := h.policyDefinitionDeletePrecheck(userID, definitionID); deleteCheck != nil {
		c.JSON(deleteCheck.status, errorBody(deleteCheck.code, deleteCheck.message))
		return
	}
	payload := gin.H{"operation": "delete_policy_definition", "policy_definition_id": definitionID}
	if challengeID == "" {
		h.createChallenge(c, "policy_change", payload)
		return
	}
	if !h.consumeChallenge(c, userID, challengeID, "policy_change", payload) {
		return
	}
	err := policyservice.NewService(h.db).DeletePolicyDefinition(userID, definitionID)
	if errors.Is(err, policyservice.ErrDefaultPolicyDeleteBlocked) {
		c.JSON(http.StatusConflict, errorBody("default_policy_delete_blocked", "Default policy cannot be deleted"))
		return
	}
	if errors.Is(err, policyservice.ErrPolicyDefinitionInUse) {
		c.JSON(http.StatusConflict, errorBody("policy_in_use", "Policy is still bound to one or more wallets"))
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, errorBody("policy_definition_not_found", "Policy definition not found"))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_definition_delete_failed", err.Error()))
		return
	}
	h.recordPolicyDefinitionAudit(c, "POLICY_DEFINITION_DELETED", definitionID, gin.H{"policy_definition_id": definitionID})
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "policy_definition_id": definitionID})
}

func (h *Handler) SetDefaultPolicyDefinition(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	definitionID := c.Param("policy_definition_id")
	if err := policyservice.NewService(h.db).SetDefaultPolicyDefinition(userID, definitionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, errorBody("policy_definition_not_found", "Policy definition not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody("set_default_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "policy_definition_id": definitionID})
}

type policyDeleteCheck struct {
	status  int
	code    string
	message string
}

func (h *Handler) policyDefinitionDeletePrecheck(userID, definitionID string) *policyDeleteCheck {
	var def storage.PolicyDefinition
	err := h.db.Where("policy_definition_id = ? AND user_id = ? AND deleted_at IS NULL", definitionID, userID).First(&def).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &policyDeleteCheck{status: http.StatusNotFound, code: "policy_definition_not_found", message: "Policy definition not found"}
	}
	if err != nil {
		return &policyDeleteCheck{status: http.StatusInternalServerError, code: "policy_definition_load_failed", message: err.Error()}
	}
	if def.IsDefault {
		return &policyDeleteCheck{status: http.StatusConflict, code: "default_policy_delete_blocked", message: "Default policy cannot be deleted"}
	}
	var bound int64
	if err := h.db.Model(&storage.PolicyInstance{}).Where("policy_definition_id = ? AND user_id = ?", definitionID, userID).Count(&bound).Error; err != nil {
		return &policyDeleteCheck{status: http.StatusInternalServerError, code: "policy_definition_load_failed", message: err.Error()}
	}
	if bound > 0 {
		return &policyDeleteCheck{status: http.StatusConflict, code: "policy_in_use", message: "Policy is still bound to one or more wallets"}
	}
	return nil
}

func (h *Handler) validatePolicyDefinitionInput(c *gin.Context, req policyRequest) (policyservice.DefinitionInput, bool) {
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, errorBody("policy_name_required", "Policy name is required"))
		return policyservice.DefinitionInput{}, false
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "manual"
	}
	if mode != "manual" && mode != "auto" {
		c.JSON(http.StatusBadRequest, errorBody("invalid_mode", "mode must be manual or auto"))
		return policyservice.DefinitionInput{}, false
	}
	return policyservice.DefinitionInput{
		Name:                      strings.TrimSpace(req.Name),
		Mode:                      mode,
		MaxPerTransactionUSD:      req.MaxPerTransactionUSD,
		DailyTransferLimitUSD:     req.DailyTransferLimitUSD,
		DailyTradeLimitUSD:        req.DailyTradeLimitUSD,
		AutoSignEnabled:           req.AutoSignEnabled,
		AutoSignMaxPerTxUSD:       req.AutoSignMaxPerTxUSD,
		AutoSignDailyLimitUSD:     req.AutoSignDailyLimitUSD,
		AutoSignMaxCountDay:       req.AutoSignMaxCountDay,
		RecipientWhitelistEnabled: req.RecipientWhitelistEnabled,
		RecipientWhitelistAddrs:   req.RecipientWhitelistAddrs,
	}, true
}

func (h *Handler) recordPolicyDefinitionAudit(c *gin.Context, eventType, definitionID string, data gin.H) {
	userID := auth.GetUserID(c.Request.Context())
	if data == nil {
		data = gin.H{}
	}
	data["event"] = eventType
	data["policy_definition_id"] = definitionID
	if challengeID := c.Query("challenge_id"); challengeID != "" {
		data["challenge_id"] = challengeID
	}
	_, _ = auditservice.NewService(h.db).Record(c.Request.Context(), auditservice.Event{
		EventType: eventType,
		Severity:  auditservice.SeverityHigh,
		UserID:    userID,
		Data:      data,
	})
}

func bindPolicyJSON(c *gin.Context, req *policyRequest) (map[string]interface{}, error) {
	var body map[string]interface{}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&body); err != nil {
		return nil, err
	}
	bodyBytes, _ := json.Marshal(body)
	if err := json.Unmarshal(bodyBytes, req); err != nil {
		return nil, err
	}
	delete(body, "challenge_id")
	return body, nil
}

func (h *Handler) GetWalletPolicyUsage(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	walletID := c.Param("wallet_id")

	wallet, err := h.vault.GetWallet(userID, walletID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorBody("wallet_not_found", err.Error()))
		return
	}

	var instance storage.PolicyInstance
	if err := h.db.Where("user_id = ? AND wallet_id = ?", userID, wallet.WalletID).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, errorBody("policy_not_bound", "No policy bound to this wallet"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody("policy_usage_load_failed", err.Error()))
		return
	}

	var version storage.PolicyDefinitionVersion
	if err := h.db.Where("policy_definition_version_id = ?", instance.PolicyDefinitionVersionID).First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, errorBody("policy_version_not_found", "Policy version not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody("policy_version_load_failed", err.Error()))
		return
	}

	// Use UsageForPolicy so the daily counters are reset for the current UTC
	// day before being returned. Reading instance.* directly would surface
	// stale yesterday-values on the first poll after midnight if no sign or
	// broadcast has fired the lazy reset yet.
	usage, err := policyservice.NewService(h.db).UsageForPolicy(instance.PolicyInstanceID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("policy_usage_load_failed", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"wallet_id":                 walletID,
		"policy_instance_id":        instance.PolicyInstanceID,
		"mode":                      version.Mode,
		"max_per_transaction_usd":   version.MaxPerTransactionUSD,
		"daily_transfer_limit_usd":  version.DailyTransferLimitUSD,
		"daily_trade_limit_usd":     version.DailyTradeLimitUSD,
		"auto_sign_max_per_tx_usd":  version.AutoSignMaxPerTxUSD,
		"auto_sign_daily_limit_usd": version.AutoSignDailyLimitUSD,
		"auto_sign_max_count_day":   version.AutoSignMaxCountDay,
		"daily_transfer_used_usd":   usage.DailyTransferUsedUSD,
		"daily_trade_used_usd":      usage.DailyTradeUsedUSD,
		"auto_sign_daily_used_usd":  usage.AutoSignDailyUsedUSD,
		"auto_sign_count_today":     usage.AutoSignCountToday,
		"last_reset_date":           usage.LastResetDate,
	})
}
