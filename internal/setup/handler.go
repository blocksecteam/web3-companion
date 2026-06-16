// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package setup

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/settings"
)

type RPCApplier interface {
	Apply(settings.RPCSettings)
}

type Handler struct {
	cfg             *config.Config
	db              *gorm.DB
	secrets         GeneratedSecrets
	service         *settings.Service
	rpcApplier      RPCApplier
	secretsRevealed bool
}

func RegisterRoutes(r *gin.Engine, cfg *config.Config, db *gorm.DB, secrets GeneratedSecrets, rpcApplier ...RPCApplier) {
	h := &Handler{
		cfg:     cfg,
		db:      db,
		secrets: secrets,
		service: settings.NewService(db),
	}
	if len(rpcApplier) > 0 && rpcApplier[0] != nil {
		h.rpcApplier = rpcApplier[0]
	}

	r.GET("/setup/status", h.Status)
	r.POST("/setup/complete", h.Complete)

	g := r.Group("/setup")
	g.Use(Guard(db))
	{
		g.GET("/secrets", h.GetSecrets)
		g.POST("/rpc", h.SaveRPC)
		g.POST("/rpc/verify", h.VerifyRPC)
		g.POST("/llm", h.SaveLLM)
		g.POST("/llm/verify", h.VerifyLLM)
		g.POST("/providers", h.SaveProviders)
	}
}

func (h *Handler) Status(c *gin.Context) {
	complete := IsSetupComplete(h.db) || IsRuntimeComplete()
	chains := make([]gin.H, 0, len(h.cfg.Chains))
	for _, ch := range h.cfg.Chains {
		chains = append(chains, gin.H{
			"name":          ch.Name,
			"chain_id":      ch.ChainID,
			"native_symbol": ch.NativeSymbol,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"complete":   complete,
		"setup_mode": h.cfg.SetupMode,
		"chains":     chains,
	})
}

func (h *Handler) GetSecrets(c *gin.Context) {
	if h.secretsRevealed {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"code":    "secrets_already_revealed",
				"message": "Secrets can only be viewed once. If you lost them, delete the database and restart setup.",
			},
		})
		return
	}
	h.secretsRevealed = true
	c.JSON(http.StatusOK, gin.H{
		"jwt_secret": h.secrets.JWTSecret,
		"ssm_key":    h.secrets.SSMKey,
		"message":    "Save these secrets securely. They are needed to recover your data if you lose the container volume. This is the only time they will be shown.",
	})
}

type saveSetupRPCRequest struct {
	Chains map[string]saveSetupRPCChain `json:"chains"`
}

type saveSetupRPCChain struct {
	URL string `json:"url"`
}

func (h *Handler) SaveRPC(c *gin.Context) {
	var req saveSetupRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_request", err.Error()))
		return
	}

	rpcSettings, err := h.buildRPCSettings(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_rpc_settings", err.Error()))
		return
	}

	if err := h.service.WriteRPC(rpcSettings); err != nil {
		c.JSON(http.StatusInternalServerError, errBody("settings_save_failed", err.Error()))
		return
	}
	h.applyRPC(rpcSettings)

	c.JSON(http.StatusOK, settings.RPCResponseFromSettings(h.cfg, rpcSettings))
}

func (h *Handler) VerifyRPC(c *gin.Context) {
	var req saveSetupRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_request", err.Error()))
		return
	}

	rpcSettings, err := h.buildRPCSettings(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_rpc_settings", err.Error()))
		return
	}

	verifier := settings.NewHTTPRPCVerifier(nil)
	for name, chain := range rpcSettings.Chains {
		cfgChain := h.cfg.ChainByName(name)
		if cfgChain == nil {
			c.JSON(http.StatusBadRequest, errBody("unsupported_chain", fmt.Sprintf("unsupported chain %q", name)))
			return
		}
		if err := verifier.Verify(c.Request.Context(), chain.URL, cfgChain.ChainID); err != nil {
			c.JSON(http.StatusBadGateway, errBody("rpc_verify_failed", err.Error()))
			return
		}
	}

	if err := h.service.WriteRPC(rpcSettings); err != nil {
		c.JSON(http.StatusInternalServerError, errBody("settings_save_failed", err.Error()))
		return
	}
	h.applyRPC(rpcSettings)

	c.JSON(http.StatusOK, settings.RPCResponseFromSettings(h.cfg, rpcSettings))
}

type saveSetupLLMRequest struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	ModelName string `json:"model_name"`
	APIKey    string `json:"api_key"`
}

func (h *Handler) SaveLLM(c *gin.Context) {
	var req saveSetupLLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_request", err.Error()))
		return
	}

	llm := settings.NormalizeLLM(settings.LLMSettings{
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		ModelName: req.ModelName,
		APIKey:    req.APIKey,
	})
	if err := h.service.WriteLLM(llm); err != nil {
		c.JSON(http.StatusInternalServerError, errBody("settings_save_failed", err.Error()))
		return
	}

	c.JSON(http.StatusOK, settings.LLMResponseFromSettings(llm))
}

func (h *Handler) VerifyLLM(c *gin.Context) {
	var req saveSetupLLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_request", err.Error()))
		return
	}

	llmService := settings.NewLLMService(nil, nil, settings.LLMConfig{})
	runtime := settings.RuntimeProvider{
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		ModelName: req.ModelName,
		APIKey:    req.APIKey,
	}
	if err := llmService.VerifyRuntimeProvider(c.Request.Context(), runtime); err != nil {
		c.JSON(http.StatusBadGateway, errBody("provider_verify_failed", err.Error()))
		return
	}

	llm := settings.NormalizeLLM(settings.LLMSettings{
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		ModelName: req.ModelName,
		APIKey:    req.APIKey,
	})
	if err := h.service.WriteLLM(llm); err != nil {
		c.JSON(http.StatusInternalServerError, errBody("settings_save_failed", err.Error()))
		return
	}

	c.JSON(http.StatusOK, settings.LLMResponseFromSettings(llm))
}

type saveSetupProvidersRequest struct {
	PortalsAPIKey        string `json:"portals_api_key"`
	MetaSleuthAPIKey     string `json:"metasleuth_api_key"`
	AddressLabelProvider string `json:"address_label_provider"`
}

func (h *Handler) SaveProviders(c *gin.Context) {
	var req saveSetupProvidersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errBody("invalid_request", err.Error()))
		return
	}

	p := settings.ProvidersSettings{
		PortalsAPIKey:        strings.TrimSpace(req.PortalsAPIKey),
		MetaSleuthAPIKey:     strings.TrimSpace(req.MetaSleuthAPIKey),
		AddressLabelProvider: settings.NormalizeAddressLabelProvider(req.AddressLabelProvider),
	}
	if err := h.service.WriteProviders(p); err != nil {
		c.JSON(http.StatusInternalServerError, errBody("settings_save_failed", err.Error()))
		return
	}
	h.cfg.Providers.PortalsAPIKey = p.PortalsAPIKey
	h.cfg.Providers.MetaSleuthAPIKey = p.MetaSleuthAPIKey

	c.JSON(http.StatusOK, settings.ProvidersResponseFromSettings(p))
}

func (h *Handler) Complete(c *gin.Context) {
	if IsRuntimeComplete() {
		c.JSON(http.StatusOK, gin.H{"status": "complete", "message": "Setup is complete."})
		return
	}
	if !isLocalhost(c.Request) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "setup_localhost_only", "message": "Setup wizard is only accessible from localhost"}})
		return
	}
	rpcSettings, err := h.service.ReadRPC()
	if err != nil {
		c.JSON(http.StatusInternalServerError, errBody("settings_load_failed", err.Error()))
		return
	}
	if len(rpcSettings.Chains) == 0 {
		c.JSON(http.StatusBadRequest, errBody("no_rpc_configured", "At least one chain RPC must be configured before completing setup"))
		return
	}

	h.applyRPC(rpcSettings)

	SetComplete()
	h.cfg.SetupMode = false

	c.JSON(http.StatusOK, gin.H{
		"status":  "complete",
		"message": "Setup is complete. The application is now fully operational.",
	})
}

func (h *Handler) applyRPC(rpcSettings settings.RPCSettings) {
	settings.ApplyRPCSettings(h.cfg, rpcSettings)
	if h.rpcApplier != nil {
		h.rpcApplier.Apply(rpcSettings)
	}
}

func (h *Handler) buildRPCSettings(req saveSetupRPCRequest) (settings.RPCSettings, error) {
	rpcSettings := settings.RPCSettings{Chains: map[string]settings.RPCChainSettings{}}
	for rawName, chain := range req.Chains {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if h.cfg.ChainByName(name) == nil {
			return settings.RPCSettings{}, fmt.Errorf("unsupported chain %q", rawName)
		}
		rpcURL := strings.TrimSpace(chain.URL)
		if rpcURL == "" {
			continue
		}
		if err := rejectPrivateRPCURL(rpcURL); err != nil {
			return settings.RPCSettings{}, fmt.Errorf("%s: %w", rawName, err)
		}
		rpcSettings.Chains[name] = settings.RPCChainSettings{URL: rpcURL}
	}
	return settings.NormalizeRPC(rpcSettings), nil
}

func rejectPrivateRPCURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("RPC URL must not point to a private or loopback address")
		}
	}
	return nil
}

func errBody(code, message string) gin.H {
	return gin.H{"error": gin.H{"code": code, "message": message}}
}
