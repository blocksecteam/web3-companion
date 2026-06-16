// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"context"
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/portals"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
	"strings"

	"time"
)

type Handler struct {
	portalsURL string
	apiKey     string
	apiKeyFunc func() string
	client     *http.Client
	portals    *portals.Client
}

type PortalsValuator struct {
	handler *Handler
	portals *portals.Client
}

type HandlerOption func(*Handler)

func WithAPIKey(apiKey string) HandlerOption {
	return func(h *Handler) {
		h.apiKey = apiKey
	}
}

func WithAPIKeyFunc(f func() string) HandlerOption {
	return func(h *Handler) {
		h.apiKeyFunc = f
	}
}

func WithPortalsURL(url string) HandlerOption {
	return func(h *Handler) {
		if url != "" {
			h.portalsURL = url
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

func NewHandler(opts ...HandlerOption) *Handler {
	h := &Handler{
		portalsURL: portals.DefaultBaseURL,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(h)
	}
	h.portals = portals.NewClient(
		portals.WithBaseURL(h.portalsURL),
		portals.WithAPIKey(h.apiKey),
		portals.WithHTTPClient(h.client),
	)
	return h
}

func (h *Handler) currentPortals() *portals.Client {
	if h.apiKeyFunc == nil {
		return h.portals
	}
	current := h.apiKeyFunc()
	if current == h.apiKey {
		return h.portals
	}
	h.apiKey = current
	h.portals = portals.NewClient(
		portals.WithBaseURL(h.portalsURL),
		portals.WithAPIKey(current),
		portals.WithHTTPClient(h.client),
	)
	return h.portals
}

func NewPortalsValuator(opts ...HandlerOption) *PortalsValuator {
	h := NewHandler(opts...)
	return &PortalsValuator{handler: h, portals: h.portals}
}

func (v *PortalsValuator) currentClient() *portals.Client {
	if v.handler != nil {
		return v.handler.currentPortals()
	}
	return v.portals
}

func (v *PortalsValuator) TokenAmount(ctx context.Context, chain, tokenAddress, rawAmount string) (amount string, usdValue string, ok bool) {
	if v == nil || (v.portals == nil && v.handler == nil) {
		return "", "", false
	}
	return v.currentClient().TokenAmount(ctx, chain, tokenAddress, rawAmount)
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	t := r.Group("/token")
	{
		t.GET("/search", h.SearchTokens)
		t.GET("/info/:chain/:address", h.GetTokenInfo)
		t.POST("/prices", h.GetPrices)
	}
}

func (h *Handler) SearchTokens(c *gin.Context) {
	query := c.Query("query")
	networks := c.Query("networks")
	if networks == "" {
		networks = "ethereum,base"
	}

	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "query is required"}})
		return
	}
	values := url.Values{}
	values.Set("search", query)
	values.Set("networks", networks)
	values.Set("platforms", "basic")
	values.Set("limit", "20")

	data, err := h.currentPortals().RawGet(c.Request.Context(), "/tokens", values)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "portals_error", "message": err.Error()},
		})
		return
	}

	c.Header("Content-Type", "application/json")
	c.String(http.StatusOK, string(data))
}

func (h *Handler) GetTokenInfo(c *gin.Context) {
	chain := c.Param("chain")
	address := c.Param("address")
	if IsNativeToken(address) {
		address = ZeroAddress
	}

	values := url.Values{}
	values.Set("ids", fmt.Sprintf("%s:%s", chain, address))
	values.Set("limit", "1")
	data, err := h.currentPortals().RawGet(c.Request.Context(), "/tokens", values)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "portals_error", "message": err.Error()},
		})
		return
	}

	c.Header("Content-Type", "application/json")
	c.String(http.StatusOK, string(data))
}

type PriceRequest struct {
	Tokens []PriceQuery `json:"tokens"`
}

type PriceQuery struct {
	Chain   string `json:"chain"`
	Address string `json:"address"`
}

func (h *Handler) GetPrices(c *gin.Context) {
	var req PriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}

	type priceResult struct {
		Chain   string `json:"chain"`
		Address string `json:"address"`
		Price   string `json:"price"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]priceResult, 0, len(req.Tokens))
	for _, t := range req.Tokens {
		tokenInfo, found, err := h.currentPortals().Token(c.Request.Context(), t.Chain, t.Address)
		if err != nil {
			results = append(results, priceResult{Chain: t.Chain, Address: t.Address, Error: err.Error()})
			continue
		}
		if !found {
			results = append(results, priceResult{Chain: t.Chain, Address: t.Address, Error: "token not found"})
			continue
		}
		price, _ := tokenInfo.Price.Float64()
		results = append(results, priceResult{
			Chain:   t.Chain,
			Address: t.Address,
			Price:   fmt.Sprintf("%.6f", price),
		})
	}

	c.JSON(http.StatusOK, gin.H{"prices": results})
}

const (
	NativeToken = "native"

	ZeroAddress = "0x0000000000000000000000000000000000000000"

	EIP155NativeAddress = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

func IsNativeToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case NativeToken, ZeroAddress, EIP155NativeAddress,
		"0x0":
		return true
	}
	return false
}

// IsNativeHexAddress reports whether the input is a hex form of the native
// token marker — the zero address (with any short/long form) or the EIP-155
// 0xEee… placeholder. Unlike IsNativeToken it does NOT match the bare
// "native" keyword; callers that need both should test IsNativeToken first.
// Used by bridge.validateBridgeTokens to accept hex native sentinels in token
// fields while still rejecting bare symbol strings.
func IsNativeHexAddress(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0x0", ZeroAddress, EIP155NativeAddress:
		return true
	}
	return false
}

var portalsBaseURL = portals.DefaultBaseURL

func SetPortalsBaseURL(u string) {
	portalsBaseURL = normalizeLegacyPortalsBaseURL(u)
}

func FetchUSDPrice(ctx context.Context, chain, tokenAddr string) (float64, bool) {
	apiKey := ""
	if cfg, err := config.Load(""); err == nil {
		apiKey = cfg.Providers.PortalsAPIKey
	}
	client := portals.NewClient(
		portals.WithBaseURL(portalsBaseURL),
		portals.WithAPIKey(apiKey),
		portals.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	)
	return client.USDPrice(ctx, chain, tokenAddr)
}

func ComputeAmountUSD(ctx context.Context, chain, tokenAddr, readableAmount string) (money.Amount, bool) {
	price, ok := FetchUSDPrice(ctx, chain, tokenAddr)
	if !ok {
		return money.Amount{}, false
	}
	amount, err := money.Parse(readableAmount)
	if err != nil {
		return money.Amount{}, false
	}
	priceAmount, err := money.FromFloat64(price)
	if err != nil {
		return money.Amount{}, false
	}
	return money.Multiply(priceAmount, amount), true
}

func normalizeLegacyPortalsBaseURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" {
		return portals.DefaultBaseURL
	}
	if strings.HasSuffix(u, "api.portals.fi") {
		return u + "/v2"
	}
	return u
}
