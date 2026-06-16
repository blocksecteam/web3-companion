// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/token"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Resolver interface {
	Resolve(ctx context.Context, chain, asset string) (uint8, error)
}

type HandlerOption func(*Handler)

func WithResolver(r Resolver) HandlerOption {
	return func(h *Handler) { h.resolver = r }
}

func WithDB(db *gorm.DB) HandlerOption {
	return func(h *Handler) { h.db = db }
}

func WithConfig(cfg *config.Config) HandlerOption {
	return func(h *Handler) { h.cfg = cfg }
}

type Handler struct {
	registry *Registry
	cfg      *config.Config
	resolver Resolver
	db       *gorm.DB
}

// NewHandler is the primary constructor, aligned with the swap.NewHandler(reg, opts...) pattern.
// Callers are responsible for populating the registry (via bridgeadapters.RegisterAll).
func NewHandler(reg *Registry, opts ...HandlerOption) *Handler {
	h := &Handler{registry: reg}
	for _, o := range opts {
		o(h)
	}
	return h
}

// NewHandlerWithAdapters is for testing; accepts fake adapters directly.
func NewHandlerWithAdapters(adapters ...Adapter) *Handler {
	reg := NewRegistry()
	for _, a := range adapters {
		reg.Register(a)
	}
	return &Handler{registry: reg}
}

// NewHandlerWithOptions is for testing; accepts fake adapters plus HandlerOptions.
func NewHandlerWithOptions(opts []HandlerOption, adapters ...Adapter) *Handler {
	reg := NewRegistry()
	for _, a := range adapters {
		reg.Register(a)
	}
	h := &Handler{registry: reg}
	for _, o := range opts {
		o(h)
	}
	return h
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	b := r.Group("/bridge")
	{
		b.POST("/quote", h.Quote)
		b.POST("/build", h.Build)
	}
}

func (h *Handler) RegisterPublicRoutes(r *gin.RouterGroup) {
	b := r.Group("/bridge")
	{
		b.GET("/protocols", h.ListProtocols)
	}
}

func (h *Handler) ListAll() []string {
	return h.registry.List()
}

// protocolInfo is the per-protocol shape returned by GET /bridge/protocols.
// Combines the adapter's identity (Name) with its declared ApproveCapability —
// the skill / agent uses `mode` and `supports_slippage` to explain to the user
// how this bridge collects funds and whether `--slippage` actually does anything.
type protocolInfo struct {
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	Style            string `json:"approve_style,omitempty"`
	SupportsSlippage bool   `json:"supports_slippage"`
}

func (h *Handler) ListProtocols(c *gin.Context) {
	all := h.registry.All()
	out := make([]protocolInfo, 0, len(all))
	for _, a := range all {
		caps := a.Capabilities()
		out = append(out, protocolInfo{
			Name:             a.Name(),
			Mode:             caps.Approve.Mode,
			Style:            caps.Approve.Style,
			SupportsSlippage: caps.SupportsSlippage,
		})
	}
	c.JSON(http.StatusOK, gin.H{"protocols": out})
}

func validateBridgeTokens(c *gin.Context, fromToken, toToken string) bool {
	for _, tok := range []string{fromToken, toToken} {
		if tok == "" {
			continue
		}
		if token.IsNativeToken(tok) {
			continue
		}
		if token.IsNativeHexAddress(tok) || isValidBridgeAddress(tok) {
			continue
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code":    "invalid_token",
			"message": "token must be 'native' or a contract address; bare symbols (e.g. 'usdc') are not accepted",
		}})
		return false
	}
	return true
}

func isValidBridgeAddress(s string) bool {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func pairSupported(pairs []ChainPair, fromChain, toChain string) bool {
	for _, p := range pairs {
		if p.FromChain == fromChain && p.ToChain == toChain {
			return true
		}
	}
	return false
}

func (h *Handler) validateChains(c *gin.Context, fromChain, toChain string) bool {
	if h.cfg == nil {
		return true
	}
	if fromChain != "" {
		if err := h.cfg.ValidateChain(fromChain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": err.Error()}})
			return false
		}
	}
	if toChain != "" {
		if err := h.cfg.ValidateChain(toChain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": err.Error()}})
			return false
		}
	}
	return true
}

func (h *Handler) walletForBuild(c *gin.Context, walletID string) (storage.Wallet, bool) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "transaction_intents_unavailable", "message": "Transaction intent store is unavailable"}})
		return storage.Wallet{}, false
	}
	var wallet storage.Wallet
	if err := h.db.WithContext(c.Request.Context()).
		Where("display_wallet_id = ? AND user_id = ? AND deleted_at IS NULL", strings.TrimSpace(walletID), auth.GetUserID(c.Request.Context())).
		First(&wallet).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "wallet_not_found", "message": "Wallet not found"}})
		return storage.Wallet{}, false
	}
	return wallet, true
}

func (h *Handler) chainID(chain string) int {
	if h.cfg != nil {
		if ch := h.cfg.ChainByName(chain); ch != nil {
			return ch.ChainID
		}
	}
	switch strings.ToLower(strings.TrimSpace(chain)) {
	case "ethereum":
		return 1
	case "bsc":
		return 56
	case "polygon":
		return 137
	case "base":
		return 8453
	default:
		return 0
	}
}

func mustJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func walletDisplayID(wallet storage.Wallet) string {
	if strings.TrimSpace(wallet.DisplayWalletID) != "" {
		return strings.TrimSpace(wallet.DisplayWalletID)
	}
	return strings.TrimSpace(wallet.WalletID)
}
