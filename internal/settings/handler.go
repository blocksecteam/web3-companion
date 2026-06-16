// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
)

type Handler struct {
	service    *Service
	cfg        *config.Config
	llm        *LLMService
	manager    LLMProviderApplier
	rpc        RPCVerifier
	rpcApplier RPCSettingsApplier
}

type LLMProviderApplier interface {
	ApplyUserRuntimeProvider(context.Context, string, RuntimeProvider) (LLMProviderApplyResult, error)
}

type LLMProviderApplyResult struct {
	Restarted bool
	Running   bool
}

type RPCVerifier interface {
	Verify(ctx context.Context, rpcURL string, chainID int) error
}

type RPCSettingsApplier interface {
	Apply(RPCSettings)
}

type RouteOption func(*Handler)

func WithRPCSettingsApplier(applier RPCSettingsApplier) RouteOption {
	return func(h *Handler) { h.rpcApplier = applier }
}

type httpRPCVerifier struct {
	client *http.Client
}

func NewHTTPRPCVerifier(client *http.Client) RPCVerifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return httpRPCVerifier{client: client}
}

type saveLLMRequest struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	ModelName string `json:"model_name"`
	APIKey    string `json:"api_key"`
}

type saveRPCRequest struct {
	Chains map[string]saveRPCChainRequest `json:"chains"`
}

type saveRPCChainRequest struct {
	URL string `json:"url"`
}

func RegisterRoutes(r *gin.RouterGroup, cfg *config.Config, service *Service, llmService *LLMService, manager LLMProviderApplier, opts ...RouteOption) {
	h := &Handler{service: service, cfg: cfg, llm: llmService, manager: manager, rpc: NewHTTPRPCVerifier(nil)}
	for _, opt := range opts {
		opt(h)
	}
	group := r.Group("/user/settings")
	group.GET("/llm", h.GetLLM)
	group.PUT("/llm", h.SaveLLM)
	group.POST("/llm/verify", h.VerifyLLM)
	group.GET("/rpc", h.GetRPC)
	group.PUT("/rpc", h.SaveRPC)
	group.POST("/rpc/verify", h.VerifyRPC)
	group.GET("/providers", h.GetProviders)
	group.PUT("/providers", h.SaveProviders)
	group.POST("/providers/verify-portals", h.VerifyPortalsKey)
	group.POST("/providers/verify-metasleuth", h.VerifyMetaSleuthKey)
}

func (h *Handler) initialLLM() LLMSettings {
	if h.cfg == nil {
		return DefaultLLMSettings()
	}
	return InitialLLMSettings(h.cfg.LLM)
}

func (h *Handler) GetLLM(c *gin.Context) {
	settings, err := h.service.ReadLLM(h.initialLLM())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, LLMResponseFromSettings(settings))
}

func (h *Handler) SaveLLM(c *gin.Context) {
	var req saveLLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	current, err := h.service.ReadLLM(h.initialLLM())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", err.Error()))
		return
	}
	next := NormalizeLLM(LLMSettings{
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		ModelName: req.ModelName,
		APIKey:    req.APIKey,
	})
	if next.APIKey == "" {
		next.APIKey = current.APIKey
	}
	if err := h.service.WriteLLM(next); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_save_failed", err.Error()))
		return
	}
	if h.manager != nil {
		userID := auth.GetUserID(c.Request.Context())
		applied, err := h.manager.ApplyUserRuntimeProvider(c.Request.Context(), userID, RuntimeProvider{
			Provider:  next.Provider,
			BaseURL:   next.BaseURL,
			APIKey:    next.APIKey,
			ModelName: next.ModelName,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorBody("companion_restart_failed", err.Error()))
			return
		}
		resp := LLMResponseFromSettings(next)
		c.JSON(http.StatusOK, gin.H{
			"provider":           resp.Provider,
			"base_url":           resp.BaseURL,
			"model_name":         resp.ModelName,
			"api_key_configured": resp.APIKeyConfigured,
			"api_key_hint":       resp.APIKeyHint,
			"verified_at":        resp.VerifiedAt,
			"restart_required":   applied.Running && !applied.Restarted,
			"restarted":          applied.Restarted,
		})
		return
	}
	c.JSON(http.StatusOK, LLMResponseFromSettings(next))
}

func (h *Handler) VerifyLLM(c *gin.Context) {
	var req saveLLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		current, loadErr := h.service.ReadLLM(h.initialLLM())
		if loadErr != nil {
			c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", loadErr.Error()))
			return
		}
		req = saveLLMRequest{
			Provider:  current.Provider,
			BaseURL:   current.BaseURL,
			ModelName: current.ModelName,
			APIKey:    current.APIKey,
		}
	}
	current, _ := h.service.ReadLLM(h.initialLLM())
	runtime := RuntimeProvider{
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		ModelName: req.ModelName,
		APIKey:    req.APIKey,
	}
	if runtime.APIKey == "" {
		runtime.APIKey = current.APIKey
	}
	if h.llm == nil {
		h.llm = NewLLMService(nil, nil, LLMConfig{})
	}
	if err := h.llm.VerifyRuntimeProvider(c.Request.Context(), runtime); err != nil {
		c.JSON(http.StatusBadGateway, errorBody("provider_verify_failed", err.Error()))
		return
	}
	verifiedAt := time.Now().UTC()
	next := NormalizeLLM(LLMSettings{
		Provider:   runtime.Provider,
		BaseURL:    runtime.BaseURL,
		ModelName:  runtime.ModelName,
		APIKey:     runtime.APIKey,
		VerifiedAt: &verifiedAt,
	})
	if err := h.service.WriteLLM(next); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_save_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, LLMResponseFromSettings(next))
}

func (h *Handler) initialRPC() RPCSettings {
	return InitialRPCSettings(h.cfg)
}

func (h *Handler) GetRPC(c *gin.Context) {
	settings, err := h.service.ReadRPCWithInitial(h.initialRPC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, RPCResponseFromSettings(h.cfg, settings))
}

func (h *Handler) SaveRPC(c *gin.Context) {
	var req saveRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	current, err := h.service.ReadRPCWithInitial(h.initialRPC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", err.Error()))
		return
	}
	submitted, err := h.rpcSettingsFromRequest(req, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_rpc_settings", err.Error()))
		return
	}
	if len(submitted.Chains) == 0 {
		c.JSON(http.StatusOK, RPCResponseFromSettings(h.cfg, current))
		return
	}
	if !h.verifyRPCSettings(c, submitted) {
		return
	}
	verifiedAt := time.Now().UTC()
	for name, chain := range submitted.Chains {
		chain.VerifiedAt = &verifiedAt
		submitted.Chains[name] = chain
	}
	next := mergeRPCSettings(current, submitted)
	if err := h.service.WriteRPC(next); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_save_failed", err.Error()))
		return
	}
	h.applyRPCSettings(next)
	c.JSON(http.StatusOK, RPCResponseFromSettings(h.cfg, next))
}

func (h *Handler) VerifyRPC(c *gin.Context) {
	var req saveRPCRequest
	current, loadErr := h.service.ReadRPCWithInitial(h.initialRPC())
	if loadErr != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", loadErr.Error()))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Chains) == 0 {
		req = requestFromRPCSettings(current)
	}
	next, err := h.rpcSettingsFromRequest(req, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_rpc_settings", err.Error()))
		return
	}
	if !h.verifyRPCSettings(c, next) {
		return
	}
	verifiedAt := time.Now().UTC()
	for name, chain := range next.Chains {
		chain.VerifiedAt = &verifiedAt
		next.Chains[name] = chain
	}
	if err := h.service.WriteRPC(next); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_save_failed", err.Error()))
		return
	}
	h.applyRPCSettings(next)
	c.JSON(http.StatusOK, RPCResponseFromSettings(h.cfg, next))
}

func (h *Handler) initialProviders() ProvidersSettings {
	if h.cfg == nil {
		return ProvidersSettings{}
	}
	return ProvidersSettings{
		PortalsAPIKey:    h.cfg.Providers.PortalsAPIKey,
		MetaSleuthAPIKey: h.cfg.Providers.MetaSleuthAPIKey,
	}
}

func (h *Handler) GetProviders(c *gin.Context) {
	settings, err := h.service.ReadProviders(h.initialProviders())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_load_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, ProvidersResponseFromSettings(settings))
}

type saveProvidersRequest struct {
	PortalsAPIKey        string `json:"portals_api_key"`
	MetaSleuthAPIKey     string `json:"metasleuth_api_key"`
	AddressLabelProvider string `json:"address_label_provider"`
}

func (h *Handler) SaveProviders(c *gin.Context) {
	var req saveProvidersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	current, _ := h.service.ReadProviders(h.initialProviders())
	next := ProvidersSettings{
		PortalsAPIKey:        stripBearerPrefix(req.PortalsAPIKey),
		MetaSleuthAPIKey:     strings.TrimSpace(req.MetaSleuthAPIKey),
		AddressLabelProvider: NormalizeAddressLabelProvider(req.AddressLabelProvider),
	}
	if next.PortalsAPIKey == "" {
		next.PortalsAPIKey = current.PortalsAPIKey
	}
	if next.MetaSleuthAPIKey == "" {
		next.MetaSleuthAPIKey = current.MetaSleuthAPIKey
	}
	if err := h.service.WriteProviders(next); err != nil {
		c.JSON(http.StatusInternalServerError, errorBody("settings_save_failed", err.Error()))
		return
	}
	h.cfg.Providers.PortalsAPIKey = next.PortalsAPIKey
	h.cfg.Providers.MetaSleuthAPIKey = next.MetaSleuthAPIKey
	c.JSON(http.StatusOK, ProvidersResponseFromSettings(next))
}

type verifyPortalsRequest struct {
	APIKey string `json:"api_key"`
}

func (h *Handler) VerifyPortalsKey(c *gin.Context) {
	var req verifyPortalsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		current, _ := h.service.ReadProviders(h.initialProviders())
		key = current.PortalsAPIKey
	}
	if key == "" {
		c.JSON(http.StatusBadRequest, errorBody("missing_key", "Portals API key is required"))
		return
	}
	if err := verifyPortalsAPIKey(c.Request.Context(), key); err != nil {
		c.JSON(http.StatusBadGateway, errorBody("portals_verify_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func verifyPortalsAPIKey(ctx context.Context, apiKey string) error {
	apiKey = stripBearerPrefix(apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.portals.fi/v2/tokens?ids=ethereum:0x0000000000000000000000000000000000000000&limit=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("portals API unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("portals API error (HTTP %d)", resp.StatusCode)
	}
	return nil
}

type verifyMetaSleuthRequest struct {
	APIKey string `json:"api_key"`
}

func (h *Handler) VerifyMetaSleuthKey(c *gin.Context) {
	var req verifyMetaSleuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		current, _ := h.service.ReadProviders(h.initialProviders())
		key = current.MetaSleuthAPIKey
	}
	if key == "" {
		c.JSON(http.StatusBadRequest, errorBody("missing_key", "MetaSleuth API key is required"))
		return
	}
	if err := verifyMetaSleuthAPIKey(c.Request.Context(), key); err != nil {
		c.JSON(http.StatusBadGateway, errorBody("metasleuth_verify_failed", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func verifyMetaSleuthAPIKey(ctx context.Context, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	payload, _ := json.Marshal(map[string]interface{}{
		"chain_id":  1,
		"addresses": []string{"0x0000000000000000000000000000000000000000"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://aml.blocksec.com/address-label/api/v3/batch-labels", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-KEY", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("MetaSleuth API unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("MetaSleuth API error (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (h *Handler) verifyRPCSettings(c *gin.Context, settings RPCSettings) bool {
	for name, chain := range settings.Chains {
		cfgChain := h.chainByName(name)
		if cfgChain == nil {
			c.JSON(http.StatusBadRequest, errorBody("unsupported_chain", fmt.Sprintf("unsupported chain %q", name)))
			return false
		}
		if err := h.rpc.Verify(c.Request.Context(), chain.URL, cfgChain.ChainID); err != nil {
			c.JSON(http.StatusBadGateway, errorBody("rpc_verify_failed", err.Error()))
			return false
		}
	}
	return true
}

func (h *Handler) rpcSettingsFromRequest(req saveRPCRequest, requireChains bool) (RPCSettings, error) {
	if requireChains && len(req.Chains) == 0 {
		return RPCSettings{}, fmt.Errorf("chains are required")
	}
	settings := RPCSettings{Chains: map[string]RPCChainSettings{}}
	for rawName, chain := range req.Chains {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if h.chainByName(name) == nil {
			return RPCSettings{}, fmt.Errorf("unsupported chain %q", rawName)
		}
		rpcURL := strings.TrimSpace(chain.URL)
		if err := validateRPCURL(rpcURL); err != nil {
			return RPCSettings{}, fmt.Errorf("%s: %w", name, err)
		}
		settings.Chains[name] = RPCChainSettings{URL: rpcURL}
	}
	return NormalizeRPC(settings), nil
}

func (h *Handler) applyRPCSettings(next RPCSettings) {
	if h.rpcApplier != nil {
		h.rpcApplier.Apply(next)
		return
	}
	ApplyRPCSettings(h.cfg, next)
}

func (h *Handler) chainByName(name string) *config.ChainConfig {
	if h.cfg == nil {
		return nil
	}
	return h.cfg.ChainByName(name)
}

func requestFromRPCSettings(settings RPCSettings) saveRPCRequest {
	req := saveRPCRequest{Chains: map[string]saveRPCChainRequest{}}
	for name, chain := range NormalizeRPC(settings).Chains {
		req.Chains[name] = saveRPCChainRequest{URL: chain.URL}
	}
	return req
}

func mergeRPCSettings(current, submitted RPCSettings) RPCSettings {
	next := NormalizeRPC(current)
	if next.Chains == nil {
		next.Chains = map[string]RPCChainSettings{}
	}
	for name, chain := range NormalizeRPC(submitted).Chains {
		next.Chains[name] = chain
	}
	return NormalizeRPC(next)
}

func validateRPCURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("url host is required")
	}
	return nil
}

func (v httpRPCVerifier) Verify(ctx context.Context, rpcURL string, chainID int) error {
	if err := validateRPCURL(rpcURL); err != nil {
		return err
	}
	client := v.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	payload := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "eth_chainId", "params": []interface{}{}}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	if decoded.Error != nil {
		return fmt.Errorf("rpc error: %s", decoded.Error.Message)
	}
	got, err := parseHexChainID(decoded.Result)
	if err != nil {
		return err
	}
	if got != chainID {
		return fmt.Errorf("rpc chain id %d does not match configured chain id %d", got, chainID)
	}
	return nil
}

func parseHexChainID(value string) (int, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "0x")
	if value == "" {
		return 0, fmt.Errorf("empty eth_chainId result")
	}
	id, err := strconv.ParseInt(value, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid eth_chainId result %q", value)
	}
	return int(id), nil
}

func errorBody(code, message string) gin.H {
	return gin.H{"error": gin.H{"code": code, "message": message}}
}
