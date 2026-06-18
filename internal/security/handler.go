// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

type Handler struct {
	simulator                 Simulator
	labelProvider             AddressLabelProvider
	functionSignatureResolver FunctionSignatureResolver
	client                    *http.Client
	cfg                       SecurityConfig
	db                        *gorm.DB
	passkeys                  *passkey.Server
	checks                    *CheckRegistry
}

type HandlerOption func(*Handler)

func WithSimulator(simulator Simulator) HandlerOption {
	return func(h *Handler) {
		if simulator != nil {
			h.simulator = simulator
		}
	}
}

func WithAddressLabelProvider(provider AddressLabelProvider) HandlerOption {
	return func(h *Handler) {
		if provider != nil {
			h.labelProvider = provider
		}
	}
}

func WithFunctionSignatureResolver(resolver FunctionSignatureResolver) HandlerOption {
	return func(h *Handler) {
		if resolver != nil {
			h.functionSignatureResolver = resolver
		}
	}
}

func WithHTTPClient(client *http.Client) HandlerOption {
	return func(h *Handler) {
		if client != nil {
			h.client = client
		}
	}
}

func WithSecurityConfig(cfg SecurityConfig) HandlerOption {
	return func(h *Handler) {
		if cfg.MaxGasUSD != 0 {
			h.cfg.MaxGasUSD = cfg.MaxGasUSD
		}
		if cfg.MaxUSDValueLossBPS != 0 {
			h.cfg.MaxUSDValueLossBPS = cfg.MaxUSDValueLossBPS
		}
		if cfg.KnownRoutersByChainID != nil {
			h.cfg.KnownRoutersByChainID = cfg.KnownRoutersByChainID
		}
		if strings.TrimSpace(cfg.SecurityAgentReviewMode) != "" {
			h.cfg.SecurityAgentReviewMode = strings.TrimSpace(cfg.SecurityAgentReviewMode)
		}
		if cfg.AllowReviewClearSet {
			h.cfg.AllowReviewClear = cfg.AllowReviewClear
		}
	}
}

func WithIntentStore(db *gorm.DB) HandlerOption {
	return func(h *Handler) { h.db = db }
}

func WithPasskeyServer(server *passkey.Server) HandlerOption {
	return func(h *Handler) { h.passkeys = server }
}

func NewHandler(opts ...HandlerOption) *Handler {
	h := &Handler{
		client: &http.Client{Timeout: 5 * time.Second},
		cfg:    defaultSecurityConfig(),
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.simulator == nil {
		h.simulator = unavailableSimulator{}
	}
	if h.labelProvider == nil {
		h.labelProvider = noopAddressLabelProvider{}
	}
	if h.functionSignatureResolver == nil {
		h.functionSignatureResolver = NewSourcifyFunctionSignatureResolver("", h.client)
	}
	if h.checks == nil {
		// Default to an empty registry rather than auto-registering the
		// built-in checks: the built-in set lives in the checks subpackage,
		// and importing it from here would create a security ↔ checks cycle.
		// Callers wanting the standard rule set must build a registry via
		// checks.RegisterDefaultChecks and inject it with WithCheckRegistry.
		h.checks = NewCheckRegistry()
	}
	return h
}

// WithCheckRegistry overrides the default check registry. Useful in tests that
// want to inject a custom set of checks (e.g. only one check, or a stub
// check that records invocations).
func WithCheckRegistry(reg *CheckRegistry) HandlerOption {
	return func(h *Handler) {
		if reg != nil {
			h.checks = reg
		}
	}
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	s := r.Group("/security")
	{
		s.POST("/check", h.Check)
		s.POST("/clear-review", h.ClearReview)
		s.POST("/override", h.Override)
	}
}

func (h *Handler) Check(c *gin.Context) {
	var req CheckIntentRequest
	if err := decodeCheckRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"},
		})
		return
	}
	intentSvc := txintent.NewService(h.db, nil)
	intent, err := intentSvc.Get(c.Request.Context(), req.TransactionID, auth.GetUserID(c.Request.Context()))
	if err != nil {
		if errors.Is(err, txintent.ErrIntentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "transaction_intent_not_found", "message": err.Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_load_failed", "message": err.Error()}})
		return
	}

	if intent.SecurityResultJSON != "" {
		var cached CheckResponse
		if err := json.Unmarshal([]byte(intent.SecurityResultJSON), &cached); err == nil {
			cached.TransactionID = intent.TransactionID
			cached.Status = intent.Status
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	checkReq := CheckRequestFromIntent(intent)
	resp, err := h.checkTransaction(c.Request.Context(), checkReq)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "simulation_failed", "message": err.Error()},
		})
		return
	}
	resultJSON, _ := json.Marshal(resp)
	updated, err := intentSvc.RecordSecurityResult(c.Request.Context(), req.TransactionID, auth.GetUserID(c.Request.Context()), txintent.SecurityResult{
		Verdict:        resp.Verdict,
		JSON:           string(resultJSON),
		RequiresReview: h.securityAgentMustReview(resp.Verdict),
	})
	if err != nil {
		// Map the state-machine errors RecordSecurityResult can return so the
		// caller can tell apart "you raced an expiry" / "intent vanished" /
		// "the intent isn't in a state that accepts a security result" from a
		// genuine server failure. Same vocabulary the sign / broadcast
		// endpoints use for the equivalent intent errors.
		if errors.Is(err, txintent.ErrIntentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "transaction_intent_not_found", "message": err.Error()}})
			return
		}
		if errors.Is(err, txintent.ErrIntentExpired) {
			c.JSON(http.StatusGone, gin.H{"error": gin.H{"code": "transaction_intent_expired", "message": err.Error()}})
			return
		}
		if errors.Is(err, txintent.ErrInvalidTransition) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "invalid_state_transition", "message": err.Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "security_record_failed", "message": err.Error()}})
		return
	}
	resp.TransactionID = updated.TransactionID
	resp.Status = updated.Status
	c.JSON(http.StatusOK, resp)
}

type ClearReviewRequest struct {
	TransactionID string `json:"transaction_id"`
	Reviewer      string `json:"reviewer"`
	Decision      string `json:"decision"`
	Confidence    string `json:"confidence"`
	Reason        string `json:"reason"`
}

func (h *Handler) ClearReview(c *gin.Context) {
	var req ClearReviewRequest
	if err := decodeClearReviewRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"},
		})
		return
	}
	if !h.cfg.AllowReviewClear {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"code":    "security_review_clear_disabled",
			"message": "Security Agent review clearance is disabled by server configuration",
		}})
		return
	}
	intentSvc := txintent.NewService(h.db, nil)
	intent, err := intentSvc.Get(c.Request.Context(), req.TransactionID, auth.GetUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "transaction_intent_not_found", "message": err.Error()}})
		return
	}
	if requiresPasskeyOverride(intent.SecurityResultJSON) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"code":    "passkey_override_required",
			"message": "Address label check returned warn or fail — Passkey override is required to proceed",
		}})
		return
	}
	updated, err := intentSvc.ClearSecurityReview(c.Request.Context(), req.TransactionID, auth.GetUserID(c.Request.Context()), txintent.SecurityClearance{
		Reviewer:   req.Reviewer,
		Decision:   req.Decision,
		Confidence: req.Confidence,
		Reason:     req.Reason,
	})
	if err != nil {
		status := http.StatusForbidden
		code := "security_clearance_rejected"
		if errors.Is(err, txintent.ErrIntentNotFound) {
			status = http.StatusNotFound
			code = "transaction_intent_not_found"
		} else if errors.Is(err, txintent.ErrIntentExpired) {
			status = http.StatusGone
			code = "transaction_intent_expired"
		} else if !errors.Is(err, txintent.ErrSecurityNotSatisfied) {
			status = http.StatusInternalServerError
			code = "security_clearance_failed"
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"transaction_id":        updated.TransactionID,
		"status":                updated.Status,
		"security_verdict":      updated.SecurityVerdict,
		"security_confirmed_at": updated.SecurityConfirmedAt,
	})
}

type OverrideRequest struct {
	TransactionID string `json:"transaction_id"`
	Reason        string `json:"reason"`
	ChallengeID   string `json:"challenge_id"`
}

func (h *Handler) Override(c *gin.Context) {
	var req OverrideRequest
	if err := decodeOverrideRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"},
		})
		return
	}
	intentSvc := txintent.NewService(h.db, nil)
	intent, err := intentSvc.Get(c.Request.Context(), req.TransactionID, auth.GetUserID(c.Request.Context()))
	if err != nil {
		status := http.StatusNotFound
		code := "transaction_intent_not_found"
		if !errors.Is(err, txintent.ErrIntentNotFound) {
			status = http.StatusInternalServerError
			code = "transaction_intent_load_failed"
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	payload := securityOverridePayload(intent, req.Reason)
	if strings.TrimSpace(req.ChallengeID) == "" {
		// Pre-check expiry before issuing a Passkey challenge so an expired
		// intent fails fast instead of asking the user to tap Passkey and then
		// failing at the ClearSecurityReview CAS. Status mismatch is left to
		// the CAS in phase 2 — only an off-skill agent ever lands there.
		if !intent.ExpiresAt.IsZero() && time.Now().After(intent.ExpiresAt) {
			c.JSON(http.StatusGone, gin.H{"error": gin.H{
				"code":    "transaction_intent_expired",
				"message": "intent expired",
			}})
			return
		}
		h.createOverrideChallenge(c, payload)
		return
	}
	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "webauthn_unavailable", "message": "Passkey server not configured"}})
		return
	}
	if err := h.passkeys.ConsumeApprovedChallenge(auth.GetUserID(c.Request.Context()), req.ChallengeID, "security_override", payload); err != nil {
		status, code := passkeyErrorStatus(err, "security_override_rejected")
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	updated, err := intentSvc.ClearSecurityReview(c.Request.Context(), req.TransactionID, auth.GetUserID(c.Request.Context()), txintent.SecurityClearance{
		Reviewer: "user_override",
		Decision: "override",
		Reason:   req.Reason,
	})
	if err != nil {
		status := http.StatusForbidden
		code := "security_override_rejected"
		if errors.Is(err, txintent.ErrIntentNotFound) {
			status = http.StatusNotFound
			code = "transaction_intent_not_found"
		} else if errors.Is(err, txintent.ErrIntentExpired) {
			status = http.StatusGone
			code = "transaction_intent_expired"
		} else if !errors.Is(err, txintent.ErrSecurityNotSatisfied) {
			status = http.StatusInternalServerError
			code = "security_override_failed"
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"transaction_id":        updated.TransactionID,
		"status":                updated.Status,
		"security_verdict":      updated.SecurityVerdict,
		"security_confirmed_at": updated.SecurityConfirmedAt,
	})
}

func decodeCheckRequest(c *gin.Context, req *CheckIntentRequest) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.TransactionID) == "" {
		return fmt.Errorf("transaction_id is required")
	}
	return nil
}

func requiresPasskeyOverride(securityResultJSON string) bool {
	if strings.TrimSpace(securityResultJSON) == "" {
		return true
	}
	var result struct {
		Checks []struct {
			Rule   string `json:"rule"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(securityResultJSON), &result); err != nil {
		return true
	}
	for _, check := range result.Checks {
		if check.Rule == "address_label_risk" && (check.Status == "warn" || check.Status == "fail") {
			return true
		}
	}
	return false
}

func decodeClearReviewRequest(c *gin.Context, req *ClearReviewRequest) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.TransactionID) == "" {
		return fmt.Errorf("transaction_id is required")
	}
	if strings.TrimSpace(req.Decision) != "safe" {
		return fmt.Errorf("decision must be safe")
	}
	return nil
}

func decodeOverrideRequest(c *gin.Context, req *OverrideRequest) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.TransactionID) == "" {
		return fmt.Errorf("transaction_id is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	return nil
}

func (h *Handler) createOverrideChallenge(c *gin.Context, payload map[string]interface{}) {
	if h.passkeys == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "webauthn_unavailable", "message": "Passkey server not configured"}})
		return
	}
	ch, err := h.passkeys.CreateChallenge(auth.GetUserID(c.Request.Context()), "security_override", mustJSON(payload))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "challenge_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":       txintent.StatusPendingUserAuth,
		"challenge_id": ch.ChallengeID,
		"sign_url":     auth.ConfirmURL("challenge=" + url.QueryEscape(ch.ChallengeID)),
		"timeout_at":   ch.ExpiresAt,
	})
}

func securityOverridePayload(intent storage.TransactionIntent, reason string) map[string]interface{} {
	payload := map[string]interface{}{
		"operation":      "security_override",
		"transaction_id": intent.TransactionID,
		"wallet_id":      intent.WalletID,
		"chain":          intent.Chain,
		"tx_type":        intent.TxType,
		"to":             intent.To,
		"payload_hash":   intent.PayloadHash,
		"reason":         strings.TrimSpace(reason),
	}
	if security := securitySummaryPayload(intent); len(security) > 0 {
		payload["security"] = security
	}
	return payload
}

func securitySummaryPayload(intent storage.TransactionIntent) map[string]interface{} {
	security := map[string]interface{}{}
	if strings.TrimSpace(intent.SecurityVerdict) != "" {
		security["verdict"] = strings.TrimSpace(intent.SecurityVerdict)
	}
	if strings.TrimSpace(intent.SecurityResultJSON) != "" {
		var result struct {
			Checks []CheckResult `json:"checks"`
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
	return security
}

func passkeyErrorStatus(err error, fallbackCode string) (int, string) {
	switch {
	case errors.Is(err, passkey.ErrChallengeExpired):
		return http.StatusGone, "challenge_expired"
	case errors.Is(err, passkey.ErrChallengeCancelled):
		return http.StatusGone, "challenge_cancelled"
	case errors.Is(err, passkey.ErrChallengeTypeMismatch):
		return http.StatusBadRequest, "challenge_type_mismatch"
	case errors.Is(err, passkey.ErrChallengePayloadMismatch):
		return http.StatusForbidden, "challenge_payload_mismatch"
	default:
		return http.StatusForbidden, fallbackCode
	}
}

func mustJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}
