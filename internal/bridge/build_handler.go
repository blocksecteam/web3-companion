// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"fmt"
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

func (h *Handler) Build(c *gin.Context) {
	var req struct {
		Bridge      string  `json:"bridge"       binding:"required"`
		FromChain   string  `json:"from_chain"   binding:"required"`
		ToChain     string  `json:"to_chain"     binding:"required"`
		FromToken   string  `json:"from_token"   binding:"required"`
		ToToken     string  `json:"to_token"`
		Amount      string  `json:"amount"       binding:"required"`
		WalletID    string  `json:"wallet_id"    binding:"required"`
		FromAddress string  `json:"from_address"`
		Slippage    float64 `json:"slippage"`
	}
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
	wallet, ok := h.walletForBuild(c, req.WalletID)
	if !ok {
		return
	}
	req.FromAddress = wallet.Address

	var fromChainRPC string
	if h.cfg != nil {
		if fromCh := h.cfg.ChainByName(req.FromChain); fromCh != nil {
			fromChainRPC = fromCh.RPCURL
		}
	}
	if checkResult := delegation.CheckBeforeBuild(
		c.Request.Context(), h.db, fromChainRPC, wallet, req.FromChain,
	); checkResult.Status != delegation.OK {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code":            delegation.ErrorCode(checkResult.Status),
			"message":         checkResult.Message,
			"delegation_hash": checkResult.DelegationHash,
		}})
		return
	}

	adapter, err := h.registry.Get(req.Bridge)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "unknown_bridge", "message": "Bridge not found: " + req.Bridge},
		})
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
	readableAmount := req.Amount

	if !pairSupported(adapter.SupportedPairs(), req.FromChain, req.ToChain) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "unknown_bridge", "message": "Bridge not found or pair unsupported: " + req.Bridge},
		})
		return
	}

	quoteReq := QuoteRequest{
		Bridge:      req.Bridge,
		FromChain:   req.FromChain,
		ToChain:     req.ToChain,
		FromToken:   req.FromToken,
		ToToken:     req.ToToken,
		Amount:      rawAmount,
		FromAddress: req.FromAddress,
		Slippage:    req.Slippage,
	}
	nativeCap := adapter.Capabilities().Native
	isNativeIn := token.IsNativeToken(req.FromToken)
	isNativeOut := req.ToToken == "" || token.IsNativeToken(req.ToToken)
	if err := CheckOutputCompatible(isNativeOut, nativeCap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_output", "message": err.Error()}})
		return
	}
	wrapIn := DeriveNativeWrapIn(isNativeIn, nativeCap)

	quoted, err := adapter.Quote(c.Request.Context(), quoteReq)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "quote_failed", "message": err.Error()},
		})
		return
	}

	result, err := adapter.Build(c.Request.Context(), BuildRequest{
		Quote: quoted.RawQuote,
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "build_failed", "message": err.Error()},
		})
		return
	}
	if result.To == "" || result.Data == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "build_failed", "message": req.Bridge + " did not return executable calldata"},
		})
		return
	}

	wrapCalls, err := BuildWrapInCalls(req.FromChain, wrapIn, rawAmount)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "wrap_failed", "message": err.Error()},
		})
		return
	}

	// Adjust the bridge call's native value to reflect any wrap action we just inserted:
	//   wrap   (native → WETH): wrap step consumed the ETH, bridge call carries no value.
	//   unwrap (WETH → native): wrap step produced native, bridge call must forward it.
	bridgeCall := Call{To: result.To, Value: result.Value, Data: result.Data}
	switch {
	case wrapIn.Wrap:
		bridgeCall.Value = "0"
	case wrapIn.Unwrap:
		bridgeCall.Value = rawAmount
	}

	// Derive the token the bridge actually receives after any wrap step the
	// handler inserted. Three symmetric cases:
	//   wrapIn.Wrap   (native → WETH): bridge pulls WETH       → approve WETH
	//   wrapIn.Unwrap (WETH → native): bridge pulls native     → no approve
	//   no wrap                       : bridge pulls FromToken → approve FromToken (if ERC20)
	// Missing the Unwrap branch would leak an approve(WETH, spender) into the
	// batch even though the bridge is being paid via msg.value.
	effectiveInput := req.FromToken
	switch {
	case wrapIn.Wrap:
		wethAddr, ok := token.WrappedNativeAddress(req.FromChain)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"code": "wrap_failed", "message": "no WETH address for chain " + req.FromChain},
			})
			return
		}
		effectiveInput = wethAddr
	case wrapIn.Unwrap:
		effectiveInput = "native"
	}

	var approveCalls []Call
	var approveSpender string
	if ac := adapter.Capabilities().Approve; ac.Mode == ApproveModeApprove && !token.IsNativeToken(effectiveInput) {
		approveSpender = strings.TrimSpace(quoted.ApproveTarget)
		if approveSpender == "" {
			approveSpender = bridgeCall.To
		}
		// rawAmount is the output of token.ReadableToRaw which already validated
		// parseability — only the positive guard remains meaningful here.
		amountInt, _ := new(big.Int).SetString(rawAmount, 10)
		if amountInt.Sign() <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_amount", "message": "approve amount must be a positive integer"}})
			return
		}
		approveCalls = ComputeApproveSequence(
			c.Request.Context(),
			fromChainRPC,
			wallet.Address,
			effectiveInput,
			approveSpender,
			bridgeCall.To, // router = bridge call target; only consulted when style == "permit2"
			ac.Style,
			amountInt,
		)
	}

	// Assemble the final ERC-7821 batch in execution order: wrap → approve → bridge.
	calls := make([]Call, 0, len(wrapCalls)+len(approveCalls)+1)
	calls = append(calls, wrapCalls...)
	calls = append(calls, approveCalls...)
	calls = append(calls, bridgeCall)

	wrapped, err := wrapBridgeBuildResult(wallet.Address, calls, result.GasLimit)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "build_failed", "message": fmt.Sprintf("ERC-7821 wrap failed: %v", err)},
		})
		return
	}
	chainID := h.chainID(req.FromChain)
	if chainID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "unsupported_chain", "message": "missing chain_id for chain: " + req.FromChain}})
		return
	}
	summary := gin.H{
		"type":           "bridge",
		"wallet_address": wallet.Address,
		"bridge":         adapter.Name(),
		"from_chain":     req.FromChain,
		"to_chain":       req.ToChain,
		"from_token":     req.FromToken,
		"to_token":       req.ToToken,
		"amount":         readableAmount,
	}
	if quoted.DepositAddress != "" {
		summary["deposit_address"] = quoted.DepositAddress
	}
	// approve_target lets the security agent cross-check that any ERC-20 approval
	// the trace emits points at the expected spender. Set only when an approve
	// sequence is actually possible (Mode=approve and the bridge will pull an
	// ERC-20, i.e. effectiveInput is not native). Mirrors swap build_handler.
	if approveSpender != "" {
		summary["approve_target"] = approveSpender
	}
	gasPrice, gasPriceSource := gasfee.ResolveForIntent(c.Request.Context(), fromChainRPC)
	intent, err := txintent.NewService(h.db, nil).Create(c.Request.Context(), txintent.CreateRequest{
		UserID:         auth.GetUserID(c.Request.Context()),
		WalletID:       walletDisplayID(wallet),
		WalletUID:      wallet.WalletUID,
		Chain:          req.FromChain,
		ChainID:        int64(chainID),
		TxType:         "bridge",
		Owner:          wallet.Address,
		To:             wrapped.To,
		Value:          wrapped.Value,
		Data:           wrapped.Data,
		GasLimit:       wrapped.GasLimit,
		GasPrice:       gasPrice,
		GasPriceSource: gasPriceSource,
		Recipient:      wrapped.To,
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

// wrapBridgeBuildResult encodes the assembled call list into a single ERC-7821 execute
// transaction targeting the user's smart account. Inputs are the source-chain wallet
// (smart-account owner), the ordered call list (wrap → approve → bridge, as produced
// by the handler), and the gas estimate from the bridge adapter.
func wrapBridgeBuildResult(owner string, calls []Call, providerGas uint64) (*BuildResult, error) {
	ercCalls := make([]erc7821.HexCall, 0, len(calls))
	for _, call := range calls {
		value, ok := new(big.Int).SetString(strings.TrimSpace(call.Value), 10)
		if !ok {
			value = big.NewInt(0)
		}
		ercCalls = append(ercCalls, erc7821.HexCall{
			To:    call.To,
			Value: value,
			Data:  call.Data,
		})
	}
	bundle, err := erc7821.BuildExecuteBundle(
		owner,
		ercCalls,
		providerGas,
		erc7821.HeuristicGas(51_000, 60_000),
	)
	if err != nil {
		return nil, err
	}
	return &BuildResult{
		To:       bundle.To,
		Value:    bundle.ValueDecimal(),
		Data:     bundle.Data,
		GasLimit: bundle.GasLimit,
	}, nil
}
