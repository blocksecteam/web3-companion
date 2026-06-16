// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/token"
)

func (h *Handler) Quote(c *gin.Context) {
	var req QuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if !h.validateChains(c, req.FromChain, req.ToChain) {
		return
	}
	if !validateBridgeTokens(c, req.FromToken, req.ToToken) {
		return
	}

	if h.resolver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "no_resolver", "message": "decimals resolver not configured"},
		})
		return
	}
	decimals, err := h.resolver.Resolve(c.Request.Context(), req.FromChain, req.FromToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "decimals_unresolvable", "message": err.Error()},
		})
		return
	}
	rawAmount, err := token.ReadableToRaw(req.Amount, decimals)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_amount", "message": err.Error()},
		})
		return
	}
	req.Amount = rawAmount

	ctx := c.Request.Context()
	type bridgeOutcome struct {
		Bridge string       `json:"bridge"`
		Quote  *QuoteResult `json:"quote,omitempty"`
		Error  string       `json:"error,omitempty"`
	}
	isNativeOut := req.ToToken == "" || token.IsNativeToken(req.ToToken)

	var candidates []Adapter
	for _, a := range h.registry.All() {
		if req.Bridge != "" && a.Name() != req.Bridge {
			continue
		}
		if !pairSupported(a.SupportedPairs(), req.FromChain, req.ToChain) {
			continue
		}
		candidates = append(candidates, a)
	}
	if req.Bridge != "" && len(candidates) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "unknown_bridge", "message": "Bridge not found or pair unsupported: " + req.Bridge},
		})
		return
	}

	outcomesCh := make(chan bridgeOutcome, len(candidates))
	var wg sync.WaitGroup
	for _, a := range candidates {
		wg.Add(1)
		go func(a Adapter) {
			defer wg.Done()
			if err := CheckOutputCompatible(isNativeOut, a.Capabilities().Native); err != nil {
				outcomesCh <- bridgeOutcome{Bridge: a.Name(), Error: err.Error()}
				return
			}
			quote, err := a.Quote(ctx, req)
			if err != nil {
				outcomesCh <- bridgeOutcome{Bridge: a.Name(), Error: err.Error()}
				return
			}
			if amount, ok := parsePositiveBridgeReceiveAmount(quote.EstimatedReceive); !ok {
				if strings.TrimSpace(quote.EstimatedReceive) == "" || (amount != nil && amount.Sign() == 0) {
					outcomesCh <- bridgeOutcome{Bridge: a.Name(), Error: "no route"}
					return
				}
				outcomesCh <- bridgeOutcome{Bridge: a.Name(), Error: "invalid estimated_receive"}
				return
			}
			outcomesCh <- bridgeOutcome{Bridge: a.Name(), Quote: quote}
		}(a)
	}
	go func() {
		wg.Wait()
		close(outcomesCh)
	}()
	var allResults []bridgeOutcome
	var successful []bridgeOutcome

	for outcome := range outcomesCh {
		allResults = append(allResults, outcome)
		if outcome.Quote != nil {
			successful = append(successful, outcome)
		}
	}

	if len(successful) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":       gin.H{"code": "no_quote", "message": "No bridge route available"},
			"all_results": allResults,
		})
		return
	}

	entries := make([]bridgeQuoteEntry, 0, len(successful))
	for _, r := range successful {
		entries = append(entries, bridgeQuoteEntry{
			Bridge:    r.Bridge,
			InAmount:  r.Quote.InputAmount,
			OutAmount: r.Quote.EstimatedReceive,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		cmp := compareBridgeReceiveAmount(entries[i].OutAmount, entries[j].OutAmount)
		if cmp == 0 {
			return entries[i].Bridge < entries[j].Bridge
		}
		return cmp > 0
	})
	c.JSON(http.StatusOK, bridgeQuoteResponse{
		FromChain: req.FromChain,
		ToChain:   req.ToChain,
		FromToken: req.FromToken,
		ToToken:   req.ToToken,
		Bridges:   entries,
	})
}

func parsePositiveBridgeReceiveAmount(raw string) (*big.Float, bool) {
	value, ok := new(big.Float).SetPrec(256).SetString(strings.TrimSpace(raw))
	if !ok {
		return nil, false
	}
	if value.Sign() <= 0 {
		return value, false
	}
	return value, true
}

func compareBridgeReceiveAmount(left, right string) int {
	a, okA := parsePositiveBridgeReceiveAmount(left)
	b, okB := parsePositiveBridgeReceiveAmount(right)
	switch {
	case okA && okB:
		return a.Cmp(b)
	case okA:
		return 1
	case okB:
		return -1
	default:
		return 0
	}
}

type bridgeQuoteEntry struct {
	Bridge    string `json:"bridge"`
	InAmount  string `json:"in_amount"`
	OutAmount string `json:"out_amount"`
}

type bridgeQuoteResponse struct {
	FromChain string             `json:"from_chain"`
	ToChain   string             `json:"to_chain"`
	FromToken string             `json:"from_token"`
	ToToken   string             `json:"to_token"`
	Bridges   []bridgeQuoteEntry `json:"bridges"`
}
