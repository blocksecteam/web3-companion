// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"

	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/token"
	"gorm.io/gorm"
	"io"
	"math/big"
	"net/http"

	"net/url"
	"strconv"
	"strings"
)

type Resolver interface {
	Resolve(ctx context.Context, chain, asset string) (uint8, error)
}

type Handler struct {
	registry    *Registry
	resolver    Resolver
	rpcURLs     map[string]string
	rpcProvider func() map[string]string
	cfg         *config.Config
	db          *gorm.DB
}

type HandlerOption func(*Handler)

func WithResolver(r Resolver) HandlerOption {
	return func(h *Handler) { h.resolver = r }
}

func WithRPCURLs(rpcURLs map[string]string) HandlerOption {
	return func(h *Handler) { h.rpcURLs = rpcURLs }
}

func WithRPCURLProvider(provider func() map[string]string) HandlerOption {
	return func(h *Handler) { h.rpcProvider = provider }
}

func WithConfig(cfg *config.Config) HandlerOption {
	return func(h *Handler) { h.cfg = cfg }
}

func WithDB(db *gorm.DB) HandlerOption {
	return func(h *Handler) { h.db = db }
}

func NewHandler(reg *Registry, opts ...HandlerOption) *Handler {
	h := &Handler{registry: reg}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	s := r.Group("/swap")
	{
		s.POST("/quote", h.Quote)
		s.POST("/build", h.Build)
		s.POST("/check-allowance", h.CheckAllowance)
	}
}

func (h *Handler) RegisterPublicRoutes(r *gin.RouterGroup) {
	s := r.Group("/swap")
	{
		s.GET("/protocols", h.ListProtocols)
	}
}

// protocolInfo is the per-protocol shape returned by GET /swap/protocols.
// `supports_slippage` is true for AMM-style aggregators (current 7 adapters);
// future RFQ / limit-order / Dutch-auction style adapters would declare false.
type protocolInfo struct {
	Name             string `json:"name"`
	SupportsSlippage bool   `json:"supports_slippage"`
}

func (h *Handler) ListProtocols(c *gin.Context) {
	all := h.registry.All()
	out := make([]protocolInfo, 0, len(all))
	for _, a := range all {
		caps := a.Capabilities()
		out = append(out, protocolInfo{
			Name:             a.Name(),
			SupportsSlippage: caps.SupportsSlippage,
		})
	}
	c.JSON(200, gin.H{"protocols": out})
}

// GetApproveCalls routes the approve flow based on style:
//
//	"erc20"   → standard ERC-20 approve
//	"permit2" → standard Permit2 4-parameter ABI (permit2 address = spender)
//
// Adapters must declare style explicitly. An unknown or empty style falls
// back to "erc20" to avoid silently returning empty calls.
func GetApproveCalls(style, tokenAddr, spender, router string, amount, currentAllowance *big.Int) []ApproveCall {
	switch style {
	case "permit2":
		return token.BuildPermit2ApproveSequence(spender, tokenAddr, router, amount, currentAllowance)
	default:
		// "erc20", "" (legacy unspecified), or any unknown value → ERC-20 path.
		return BuildApproveSequence(tokenAddr, spender, amount, currentAllowance)
	}
}
func (h *Handler) validateChain(c *gin.Context, chain string) bool {
	if h.cfg == nil {
		return true
	}
	if err := h.cfg.ValidateChain(chain); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": err.Error()}})
		return false
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

type PortalsDecimalsSource struct {
	baseURL    string
	apiKey     string
	apiKeyFunc func() string
	client     *http.Client
}

func NewPortalsDecimalsSource(baseURL, apiKey string, client *http.Client) *PortalsDecimalsSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &PortalsDecimalsSource{baseURL: baseURL, apiKey: apiKey, client: client}
}

func NewPortalsDecimalsSourceWithKeyFunc(baseURL string, keyFunc func() string, client *http.Client) *PortalsDecimalsSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &PortalsDecimalsSource{baseURL: baseURL, apiKeyFunc: keyFunc, client: client}
}

func (s *PortalsDecimalsSource) Decimals(ctx context.Context, chain, asset string) (uint8, error) {
	id := chain + ":" + asset
	values := url.Values{}
	values.Set("ids", id)
	values.Set("limit", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/tokens?"+values.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("portals: build request: %w", err)
	}
	key := s.apiKey
	if s.apiKeyFunc != nil {
		key = s.apiKeyFunc()
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("portals: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("portals: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Tokens []struct {
			Decimals uint8  `json:"decimals"`
			Symbol   string `json:"symbol"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("portals: decode response: %w", err)
	}
	if len(payload.Tokens) == 0 {
		return 0, fmt.Errorf("portals: token %s not found", id)
	}
	return payload.Tokens[0].Decimals, nil
}

const decimalsSelector = "0x313ce567"

type RPCDecimalsSource struct {
	rpcURLs     map[string]string
	rpcProvider func() map[string]string
	client      *http.Client
}

func NewRPCDecimalsSource(rpcURLs map[string]string, client *http.Client) *RPCDecimalsSource {
	return NewRPCDecimalsSourceWithProvider(func() map[string]string {
		return rpcURLs
	}, client)
}

func NewRPCDecimalsSourceWithProvider(provider func() map[string]string, client *http.Client) *RPCDecimalsSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &RPCDecimalsSource{rpcProvider: provider, client: client}
}

func (s *RPCDecimalsSource) Decimals(ctx context.Context, chain, asset string) (uint8, error) {
	rpcURL, ok := s.currentRPCURLs()[strings.ToLower(strings.TrimSpace(chain))]
	if !ok {
		return 0, fmt.Errorf("rpc: no RPC URL configured for chain %q", chain)
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []interface{}{
			map[string]string{"to": asset, "data": decimalsSelector},
			"latest",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("rpc: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("rpc: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("rpc: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("rpc: HTTP %d", resp.StatusCode)
	}

	var rpcResp struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return 0, fmt.Errorf("rpc: decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("rpc: %s", rpcResp.Error.Message)
	}
	return parseDecimalsHexResult(rpcResp.Result)
}

func (s *RPCDecimalsSource) currentRPCURLs() map[string]string {
	if s == nil {
		return nil
	}
	if s.rpcProvider != nil {
		return s.rpcProvider()
	}
	return s.rpcURLs
}

func parseDecimalsHexResult(s string) (uint8, error) {
	hex := strings.TrimPrefix(strings.TrimSpace(s), "0x")
	if len(hex) != 64 {
		return 0, fmt.Errorf("rpc: expected 64-hex-char result, got %q", s)
	}
	v, err := strconv.ParseUint(hex[62:], 16, 8)
	if err != nil {
		return 0, fmt.Errorf("rpc: parse decimals byte: %w", err)
	}
	return uint8(v), nil
}
