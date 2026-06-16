// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/delegation"
	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/gasfee"
	"github.com/blocksecteam/web3-companion/internal/token"
	"github.com/blocksecteam/web3-companion/internal/txintent"
)

type TransferRequest struct {
	Chain    string `json:"chain"     binding:"required"`
	WalletID string `json:"wallet_id" binding:"required"`
	Token    string `json:"token"     binding:"required"`
	To       string `json:"to"        binding:"required"`
	Amount   string `json:"amount"    binding:"required"`
}

func (h *Handler) Transfer(c *gin.Context) {
	var req TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": err.Error()}})
		return
	}
	req.To = strings.TrimSpace(req.To)
	if !common.IsHexAddress(req.To) {
		// Without this guard a malformed recipient (short hex, long hex,
		// non-hex chars, empty) silently flows through padHexAddress, which
		// left-pads/truncates whatever it gets, and the resulting calldata
		// targets a real-but-unrelated address. Policy whitelist only catches
		// it when one is configured; default-permissive wallets have no
		// safety net downstream until burn_address_check, which only matches
		// known-burn addresses (zero, dead, etc.).
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code":    "invalid_recipient",
			"message": "Recipient address must be a valid 0x-prefixed 40-character hex address",
		}})
		return
	}
	ch := resolveChain(h.cfg, req.Chain)
	if ch == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "unsupported_chain", "message": "Unsupported chain: " + req.Chain},
		})
		return
	}
	wallet, ok := h.walletForIntent(c, req.WalletID)
	if !ok {
		return
	}
	if checkResult := delegation.CheckBeforeBuild(
		c.Request.Context(), h.db, ch.RPCURL, wallet, req.Chain,
	); checkResult.Status != delegation.OK {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code":            delegation.ErrorCode(checkResult.Status),
			"message":         checkResult.Message,
			"delegation_hash": checkResult.DelegationHash,
		}})
		return
	}
	isNative := token.IsNativeToken(req.Token)
	isERC20 := !isNative && IsValidERC20Address(req.Token)
	if !isNative && !isERC20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code":    "invalid_token",
			"message": "invalid token: use 'native' for native token or a contract address for ERC-20",
		}})
		return
	}

	if isNative {
		rawAmount, err := token.ReadableToRaw(req.Amount, 18)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_amount", "message": err.Error()}})
			return
		}
		amt := new(big.Int)
		amt.SetString(rawAmount, 10)
		// Route native transfers through the ERC-7821 batch executor so they
		// share swap's 1.2x buffer + 1M gas floor. A direct path with GasLimit
		// 21000 OOGs when the recipient has ERC-7702 delegation (e.g.
		// self-transfer): calling a delegated address — even with empty
		// calldata — triggers its fallback.
		calls := []erc7821.HexCall{{To: req.To, Value: amt, Data: "0x"}}
		bundle, err := buildTransferBundle(wallet.Address, calls, nativeTransferGas)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "build_failed", "message": err.Error()}})
			return
		}
		gasPrice, gasPriceSource := gasfee.ResolveForIntent(c.Request.Context(), ch.RPCURL)
		h.respondWithIntent(c, txintent.CreateRequest{
			UserID:         auth.GetUserID(c.Request.Context()),
			WalletID:       walletDisplayID(wallet),
			WalletUID:      wallet.WalletUID,
			Chain:          ch.Name,
			ChainID:        int64(ch.ChainID),
			TxType:         "transfer",
			Owner:          wallet.Address,
			To:             bundle.To,
			Value:          bundle.ValueHex(),
			Data:           bundle.Data,
			GasLimit:       bundle.GasLimit,
			GasPrice:       gasPrice,
			GasPriceSource: gasPriceSource,
			Recipient:      req.To,
			SummaryJSON:    mustSummaryJSON(gin.H{"type": "transfer", "chain": ch.Name, "wallet_address": wallet.Address, "token": "native", "amount": req.Amount, "to": req.To}),
		})
		return
	}

	if h.resolver == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "no_resolver", "message": "token resolver not configured"}})
		return
	}
	ctx := c.Request.Context()
	decimals, err := h.resolver.Resolve(ctx, req.Chain, req.Token)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "decimals_error", "message": err.Error()}})
		return
	}
	rawAmount, err := token.ReadableToRaw(req.Amount, decimals)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_amount", "message": err.Error()}})
		return
	}
	amt := new(big.Int)
	amt.SetString(rawAmount, 10)
	calldata := token.EncodeERC20Transfer(req.To, amt)
	calls := []erc7821.HexCall{{To: req.Token, Value: big.NewInt(0), Data: calldata}}
	bundle, err := buildTransferBundle(wallet.Address, calls, erc20TransferGas)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "build_failed", "message": err.Error()}})
		return
	}
	gasPrice, gasPriceSource := gasfee.ResolveForIntent(c.Request.Context(), ch.RPCURL)
	h.respondWithIntent(c, txintent.CreateRequest{
		UserID:         auth.GetUserID(c.Request.Context()),
		WalletID:       walletDisplayID(wallet),
		WalletUID:      wallet.WalletUID,
		Chain:          ch.Name,
		ChainID:        int64(ch.ChainID),
		TxType:         "transfer",
		Owner:          wallet.Address,
		To:             bundle.To,
		Value:          bundle.ValueHex(),
		Data:           bundle.Data,
		GasLimit:       bundle.GasLimit,
		GasPrice:       gasPrice,
		GasPriceSource: gasPriceSource,
		Recipient:      req.To,
		SummaryJSON:    mustSummaryJSON(gin.H{"type": "transfer", "chain": ch.Name, "wallet_address": wallet.Address, "token": req.Token, "amount": req.Amount, "to": req.To}),
	})
}

const (
	nativeTransferGas = uint64(70_000)
	erc20TransferGas  = uint64(120_000)
)

func buildTransferBundle(walletAddress string, calls []erc7821.HexCall, gasLimit uint64) (erc7821.Bundle, error) {
	return erc7821.BuildExecuteBundle(
		walletAddress,
		calls,
		gasLimit,
		nil,
	)
}
