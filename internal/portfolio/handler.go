// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/portals"
)

type Snapshot struct {
	Chain    string         `json:"chain"`
	Address  string         `json:"address"`
	TotalUSD money.Amount   `json:"total_usd"`
	Balances []TokenBalance `json:"balances"`
}

type TokenBalance struct {
	Address    string  `json:"address"`
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	Balance    float64 `json:"balance"`
	BalanceUSD float64 `json:"balanceUSD"`
	Price      float64 `json:"price"`
	Decimals   int     `json:"decimals"`
	Network    string  `json:"network"`
}

type AssetInspector struct {
	chains  []string
	client  *portals.Client
	handler *Handler
}

type Handler struct {
	portalsURL string
	apiKey     string
	apiKeyFunc func() string
	client     *http.Client
	portals    *portals.Client
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

func NewAssetInspector(chains []string, opts ...HandlerOption) *AssetInspector {
	normalizedChains := make([]string, 0, len(chains))
	for _, chain := range chains {
		if chain == "" {
			continue
		}
		normalizedChains = append(normalizedChains, chain)
	}
	handler := NewHandler(opts...)
	return &AssetInspector{chains: normalizedChains, client: handler.portals, handler: handler}
}

func (i *AssetInspector) currentClient() *portals.Client {
	if i.handler != nil {
		return i.handler.currentPortals()
	}
	return i.client
}

func (i *AssetInspector) TotalUSD(ctx context.Context, address string) (money.Amount, error) {
	if i == nil || (i.client == nil && i.handler == nil) {
		return money.Amount{}, fmt.Errorf("portfolio asset inspector is not configured")
	}
	if len(i.chains) == 0 {
		return money.Amount{}, fmt.Errorf("portfolio asset inspector has no configured chains")
	}
	total := money.Amount{}
	client := i.currentClient()
	for _, chain := range i.chains {
		snapshot, err := fetchSnapshot(ctx, client, chain, address)
		if err != nil {
			return money.Amount{}, err
		}
		total = money.Sum(total, snapshot.TotalUSD)
	}
	return total, nil
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	p := r.Group("/portfolio")
	p.GET("/:chain/:address", h.GetPortfolio)
}

// FetchSnapshot accepts either a single chain (e.g. "ethereum") or a
// comma-separated list (e.g. "ethereum,base,bsc"). Multi-chain is forwarded
// to Portals as a repeated `networks` query parameter — Portals merges the
// response and tags each balance with its source `network` field. The
// returned Snapshot.Chain echoes the input verbatim so callers can tell
// whether a multi-chain query was issued.
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

func (h *Handler) FetchSnapshot(ctx context.Context, chain, address string) (Snapshot, error) {
	return fetchSnapshot(ctx, h.currentPortals(), chain, address)
}

func fetchSnapshot(ctx context.Context, client *portals.Client, chain, address string) (Snapshot, error) {
	chains := splitChainParam(chain)
	if len(chains) == 0 {
		return Snapshot{}, fmt.Errorf("at least one chain required")
	}

	balances, err := client.AccountBalances(ctx, chains, address)
	if err != nil {
		return Snapshot{}, err
	}

	// Defensive: Portals tags each balance with `network` in our experience,
	// but for single-chain calls fall back to the requested chain if the
	// upstream omits it. Multi-chain queries depend on the tag being present.
	if len(chains) == 1 {
		for i := range balances {
			if balances[i].Network == "" {
				balances[i].Network = chains[0]
			}
		}
	}

	totalUSD := money.Amount{}
	for _, b := range balances {
		amount, err := money.FromFloat64(b.BalanceUSD)
		if err != nil {
			return Snapshot{}, fmt.Errorf("parse balance USD: %w", err)
		}
		totalUSD = money.Sum(totalUSD, amount)
	}

	return Snapshot{
		Chain:    chain,
		Address:  address,
		TotalUSD: totalUSD,
		Balances: tokenBalances(balances),
	}, nil
}

func splitChainParam(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func tokenBalances(raw []portals.TokenBalance) []TokenBalance {
	out := make([]TokenBalance, len(raw))
	for i, balance := range raw {
		out[i] = TokenBalance(balance)
	}
	return out
}

func (h *Handler) GetPortfolio(c *gin.Context) {
	chain := c.Param("chain")
	address := c.Param("address")
	snapshot, err := h.FetchSnapshot(c.Request.Context(), chain, address)
	if err != nil {
		slog.Error("portals account fetch failed", "chain", chain, "address", address, "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "portals_error", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"chain":     snapshot.Chain,
		"address":   snapshot.Address,
		"total_usd": snapshot.TotalUSD,
		"balances":  snapshot.Balances,
	})
}
