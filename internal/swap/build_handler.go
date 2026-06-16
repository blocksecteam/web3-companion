// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/delegation"
	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/gasfee"
	"github.com/blocksecteam/web3-companion/internal/token"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

type BuildTxRequest struct {
	Protocol    string  `json:"protocol"`
	Chain       string  `json:"chain"        binding:"required"`
	WalletID    string  `json:"wallet_id"    binding:"required"`
	FromAddress string  `json:"from_address"`
	SourceAsset string  `json:"source_asset" binding:"required"`
	TargetAsset string  `json:"target_asset" binding:"required"`
	Amount      string  `json:"amount"       binding:"required"`
	Slippage    float64 `json:"slippage"`
}

func (h *Handler) Build(c *gin.Context) {
	var req BuildTxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	if !h.validateChain(c, req.Chain) {
		return
	}
	wallet, ok := h.walletForBuild(c, req.WalletID)
	if !ok {
		return
	}
	req.FromAddress = wallet.Address
	if checkResult := delegation.CheckBeforeBuild(
		c.Request.Context(), h.db, h.rpcURLForChain(req.Chain), wallet, req.Chain,
	); checkResult.Status != delegation.OK {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code":            delegation.ErrorCode(checkResult.Status),
			"message":         checkResult.Message,
			"delegation_hash": checkResult.DelegationHash,
		}})
		return
	}
	chainID := h.chainID(req.Chain)
	if chainID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": "missing chain_id for chain: " + req.Chain}})
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

	var adapter ProtocolAdapter
	if req.Protocol != "" {
		adapter, err = h.registry.Get(req.Protocol)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "unknown_protocol", "message": err.Error()},
			})
			return
		}
	} else {
		adapter, err = h.bestAdapter(ctx, qr)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"code": "no_quote", "message": err.Error()},
			})
			return
		}
	}

	quoted, err := adapter.Quote(ctx, qr)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "quote_failed", "message": err.Error()},
		})
		return
	}

	// The handler derives NativeWrap as a local variable from the adapter's declared
	// protocol capabilities and the request shape.
	// The adapter's Quote() does not need to compute or set any NativeWrap fields.
	isNativeIn := token.IsNativeToken(req.SourceAsset)
	isNativeOut := token.IsNativeToken(req.TargetAsset)
	nw := DeriveNativeWrap(isNativeIn, isNativeOut, adapter.Capabilities().Native)

	slippageBPS := int(math.Round(req.Slippage * 100))
	result, err := adapter.Build(ctx, BuildRequest{
		Chain:       req.Chain,
		FromAddress: req.FromAddress,
		Quote:       quoted.RawQuote,
		SlippageBPS: slippageBPS,
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "build_failed", "message": err.Error()},
		})
		return
	}
	if result.To == "" || result.Data == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "build_failed", "message": fmt.Sprintf("%s did not return executable calldata", adapter.Name())},
		})
		return
	}

	// When the handler will prepend WETH.deposit (WrapIn), the protocol pulls
	// WETH rather than the user's "native" — so the approve must target WETH.
	// Using req.SourceAsset here would skip the approve entirely (isNativeAsset
	// returns true for "native") and the protocol's transferFrom would fail.
	approveAsset := req.SourceAsset
	if nw.WrapIn {
		wethAddr, ok := token.WrappedNativeAddress(req.Chain)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "wrap_resolution_failed", "message": "no WETH address for chain " + req.Chain}})
			return
		}
		approveAsset = wethAddr
	}
	approveCalls, err := h.approveCallsForBuild(ctx, req.Chain, approveAsset, wallet.Address, result.To, quoted)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "allowance_check_failed", "message": err.Error()}})
		return
	}
	wrapPre, wrapPost, err := ResolveWrapCalls(req.Chain, nw, quoted.InputAmount, quoted.OutputAmount, slippageBPS)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "wrap_resolution_failed", "message": err.Error()}})
		return
	}
	swapValue := new(big.Int)
	if result.Value != "" && result.Value != "0x" && result.Value != "0x0" {
		if _, ok := swapValue.SetString(strings.TrimPrefix(result.Value, "0x"), 16); !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"code": "build_failed", "message": fmt.Sprintf("%s returned malformed Value %q", adapter.Name(), result.Value)},
			})
			return
		}
	}
	calls := ApproveCallsToHex(approveCalls)
	calls = append(calls, wrapPre...)
	calls = append(calls, erc7821.HexCall{To: result.To, Value: swapValue, Data: result.Data})
	calls = append(calls, wrapPost...)
	bundle := BuildExecuteBundle(wallet.Address, calls, result.GasLimit)

	// summary["approve_target"] must match the actual on-chain approve spender
	// or the security agent flags the trace as a spender mismatch. Mirror the
	// fallback approveCallsForBuild uses: when the adapter leaves ApproveTarget
	// empty (odos, openocean), the handler approves to result.To.
	approveTarget := strings.TrimSpace(quoted.ApproveTarget)
	if approveTarget == "" {
		approveTarget = result.To
	}
	summary := gin.H{
		"type":           "swap",
		"chain":          req.Chain,
		"wallet_address": wallet.Address,
		"protocol":       adapter.Name(),
		"source_asset":   req.SourceAsset,
		"target_asset":   req.TargetAsset,
		"input_amount":   quoted.InputAmount,
		"output_amount":  quoted.OutputAmount,
		"approve_target": approveTarget,
	}
	gasPrice, gasPriceSource := gasfee.ResolveForIntent(ctx, h.rpcURLForChain(req.Chain))
	intent, err := txintent.NewService(h.db, nil).Create(ctx, txintent.CreateRequest{
		UserID:         auth.GetUserID(ctx),
		WalletID:       walletDisplayID(wallet),
		WalletUID:      wallet.WalletUID,
		Chain:          req.Chain,
		ChainID:        int64(chainID),
		TxType:         "swap",
		Owner:          wallet.Address,
		To:             bundle.To,
		Value:          bundle.Value,
		Data:           bundle.Data,
		GasLimit:       bundle.GasLimit,
		GasPrice:       gasPrice,
		GasPriceSource: gasPriceSource,
		Recipient:      result.To,
		SummaryJSON:    mustJSON(summary),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "transaction_intent_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"transaction_id": intent.TransactionID,
		"status":         intent.Status,
		"summary":        summary,
		"expires_at":     intent.ExpiresAt,
	})
}
