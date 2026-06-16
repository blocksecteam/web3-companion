// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"math/big"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetGasPrice(c *gin.Context) {
	chainName := c.Param("chain")
	ch := resolveChain(h.cfg, chainName)
	if ch == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "unsupported_chain", "message": "Unsupported chain: " + chainName},
		})
		return
	}

	ctx := c.Request.Context()
	rpcClient := getRPCClient(ch.RPCURL)
	gasPrice, err := rpcClient.SuggestGasPrice(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "rpc_error", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"chain":          ch.Name,
		"chain_id":       ch.ChainID,
		"gas_price_wei":  gasPrice.String(),
		"gas_price_gwei": weiToGwei(gasPrice),
	})
}

func (h *Handler) EstimateGas(c *gin.Context) {
	var req GasEstimateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}

	ch := resolveChain(h.cfg, req.Chain)
	if ch == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "unsupported_chain", "message": "Unsupported chain: " + req.Chain},
		})
		return
	}

	ctx := c.Request.Context()
	rpcClient := getRPCClient(ch.RPCURL)

	msg := map[string]interface{}{
		"from": req.From,
	}
	if req.To != "" {
		msg["to"] = req.To
	}
	if req.Data != "" && req.Data != "0x" {
		msg["data"] = req.Data
	}
	if req.Value != "" && req.Value != "0" {
		msg["value"] = req.Value
	}

	estimated, err := rpcClient.EstimateGas(ctx, msg)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"code": "rpc_error", "message": err.Error()},
		})
		return
	}

	gasPrice, _ := rpcClient.SuggestGasPrice(ctx)
	if gasPrice == nil {
		gasPrice = big.NewInt(50_000_000_000)
	}
	gasCostWei := new(big.Int).Mul(big.NewInt(int64(estimated)), gasPrice)

	c.JSON(http.StatusOK, gin.H{
		"chain":              ch.Name,
		"chain_id":           ch.ChainID,
		"gas_limit":          estimated,
		"gas_price_wei":      gasPrice.String(),
		"estimated_cost_wei": gasCostWei.String(),
		"estimated_cost_eth": weiToEther(gasCostWei),
	})
}

type GasEstimateRequest struct {
	Chain string `json:"chain" binding:"required"`
	From  string `json:"from" binding:"required"`
	To    string `json:"to"`
	Data  string `json:"data"`
	Value string `json:"value"`
}
