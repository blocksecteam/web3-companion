// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/token"
)

type SmartQuoteRequest struct {
	Protocol    string  `json:"protocol"`
	Chain       string  `json:"chain" binding:"required"`
	FromAddress string  `json:"from_address" binding:"required"`
	SourceAsset string  `json:"source_asset" binding:"required"`
	TargetAsset string  `json:"target_asset" binding:"required"`
	Amount      string  `json:"amount" binding:"required"`
	Slippage    float64 `json:"slippage"`
}

func (h *Handler) Quote(c *gin.Context) {
	var req SmartQuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if !h.validateChain(c, req.Chain) {
		return
	}

	ctx := c.Request.Context()

	if h.resolver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "no_resolver", "message": "decimals resolver not configured"},
		})
		return
	}
	decimals, err := h.resolver.Resolve(ctx, req.Chain, req.SourceAsset)
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

	qr := QuoteRequest{
		Chain:         req.Chain,
		FromAddress:   req.FromAddress,
		SourceAsset:   req.SourceAsset,
		TargetAsset:   req.TargetAsset,
		Amount:        rawAmount,
		DisplayAmount: req.Amount,
		Slippage:      req.Slippage,
	}

	if req.Protocol != "" {
		adapter, err := h.registry.Get(req.Protocol)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "unknown_protocol", "message": err.Error()},
			})
			return
		}
		quote, err := adapter.Quote(ctx, qr)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"code": "quote_failed", "message": err.Error()},
			})
			return
		}
		c.JSON(http.StatusOK, swapQuoteResponse{
			Chain:     req.Chain,
			FromToken: req.SourceAsset,
			ToToken:   req.TargetAsset,
			Protocols: []swapQuoteEntry{
				{Protocol: quote.Protocol, InAmount: quote.InputAmount, OutAmount: quote.OutputAmount},
			},
		})
		return
	}

	adapters := h.registry.All()
	type quoteOutcome struct {
		Protocol string       `json:"protocol"`
		Quote    *QuoteResult `json:"quote,omitempty"`
		Error    string       `json:"error,omitempty"`
	}
	outcomes := make(chan quoteOutcome, len(adapters))
	var wg sync.WaitGroup
	compatible := 0

	for _, adapter := range adapters {
		if !chainSupported(adapter.SupportedChains(), req.Chain) {
			continue
		}
		compatible++
		wg.Add(1)
		go func(adapter ProtocolAdapter) {
			defer wg.Done()
			quote, err := adapter.Quote(ctx, qr)
			if err != nil {
				slog.Debug("swap quote failed", "protocol", adapter.Name(), "error", err)
				outcomes <- quoteOutcome{Protocol: adapter.Name(), Error: err.Error()}
				return
			}
			if quote.OutputAmount == "" || quote.OutputAmount == "0" {
				outcomes <- quoteOutcome{Protocol: adapter.Name(), Error: "no liquidity"}
				return
			}
			outcomes <- quoteOutcome{Protocol: adapter.Name(), Quote: quote}
		}(adapter)
	}
	go func() {
		wg.Wait()
		close(outcomes)
	}()

	results := make([]quoteOutcome, 0, compatible)
	var best *QuoteResult
	for outcome := range outcomes {
		results = append(results, outcome)
		if outcome.Quote != nil && (best == nil || compareQuotes(outcome.Quote, best) > 0) {
			best = outcome.Quote
		}
	}

	if best == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":       gin.H{"code": "no_quote", "message": "No protocol returned a valid quote"},
			"all_results": results,
		})
		return
	}

	entries := make([]swapQuoteEntry, 0, len(results))
	for _, r := range results {
		if r.Quote != nil {
			entries = append(entries, swapQuoteEntry{
				Protocol:  r.Protocol,
				InAmount:  r.Quote.InputAmount,
				OutAmount: r.Quote.OutputAmount,
			})
		}
	}
	c.JSON(http.StatusOK, swapQuoteResponse{
		Chain:     req.Chain,
		FromToken: req.SourceAsset,
		ToToken:   req.TargetAsset,
		Protocols: entries,
	})
}

type swapQuoteEntry struct {
	Protocol  string `json:"protocol"`
	InAmount  string `json:"in_amount"`
	OutAmount string `json:"out_amount"`
}

type swapQuoteResponse struct {
	Chain     string           `json:"chain"`
	FromToken string           `json:"from_token"`
	ToToken   string           `json:"to_token"`
	Protocols []swapQuoteEntry `json:"protocols"`
}

func (h *Handler) bestAdapter(ctx context.Context, qr QuoteRequest) (ProtocolAdapter, error) {
	adapters := h.registry.All()
	type outcome struct {
		adapter ProtocolAdapter
		quote   *QuoteResult
	}
	ch := make(chan outcome, len(adapters))
	var wg sync.WaitGroup
	for _, a := range adapters {
		if !chainSupported(a.SupportedChains(), qr.Chain) {
			continue
		}
		wg.Add(1)
		go func(a ProtocolAdapter) {
			defer wg.Done()
			q, err := a.Quote(ctx, qr)
			if err != nil || q.OutputAmount == "" || q.OutputAmount == "0" {
				return
			}
			ch <- outcome{adapter: a, quote: q}
		}(a)
	}
	go func() { wg.Wait(); close(ch) }()

	var best *outcome
	for o := range ch {
		o := o
		if best == nil || compareQuotes(o.quote, best.quote) > 0 {
			best = &o
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no protocol returned a valid quote for chain %s", qr.Chain)
	}
	return best.adapter, nil
}

func compareQuotes(a, b *QuoteResult) int {
	amountA, okA := new(big.Int).SetString(a.OutputAmount, 10)
	amountB, okB := new(big.Int).SetString(b.OutputAmount, 10)
	if !okA || !okB {
		if a.OutputAmount > b.OutputAmount {
			return 1
		}
		if a.OutputAmount < b.OutputAmount {
			return -1
		}
		return 0
	}
	return amountA.Cmp(amountB)
}
