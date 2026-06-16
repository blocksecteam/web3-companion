// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/token"
)

type chainInfo struct {
	Name            string                           `json:"name"`
	ChainID         int                              `json:"chain_id"`
	NativeSymbol    string                           `json:"native_symbol"`
	CanonicalTokens map[string]config.CanonicalToken `json:"canonical_tokens"`
}

func (h *Handler) ListChains(c *gin.Context) {
	active := h.cfg.ActiveChains()
	chains := make([]chainInfo, len(active))
	for i, ch := range active {
		chains[i] = chainInfo{
			Name:            ch.Name,
			ChainID:         ch.ChainID,
			NativeSymbol:    ch.NativeSymbol,
			CanonicalTokens: CanonicalTokens(ch.Name),
		}
	}
	c.JSON(http.StatusOK, chains)
}

var canonicalTokens = map[string]map[string]config.CanonicalToken{
	"ethereum": {
		"USDC": {Address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Decimals: 6},
		"USDT": {Address: "0xdAC17F958D2ee523a2206206994597C13D831ec7", Decimals: 6},
		"DAI":  {Address: "0x6B175474E89094C44Da98b954EedeAC495271d0F", Decimals: 18},
		"WETH": {Address: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", Decimals: 18},
		"WBTC": {Address: "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", Decimals: 8},
		"LINK": {Address: "0x514910771AF9Ca656af840dff83E8264EcF986CA", Decimals: 18},
		"UNI":  {Address: "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984", Decimals: 18},
		"AAVE": {Address: "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9", Decimals: 18},
	},
	"base": {
		"USDC":  {Address: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", Decimals: 6},
		"USDbC": {Address: "0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA", Decimals: 6},
		"USDT":  {Address: "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2", Decimals: 6},
		"DAI":   {Address: "0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb", Decimals: 18},
		"WETH":  {Address: "0x4200000000000000000000000000000000000006", Decimals: 18},
		"cbBTC": {Address: "0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf", Decimals: 8},
		"EURC":  {Address: "0x60a3e35cc302bfa44cb288bc5a4f316fdb1adb42", Decimals: 6},
		"LINK":  {Address: "0x88Fb150BDc53A65fe94De4c9BA0a6dAf8C6e196e", Decimals: 18},
		"AERO":  {Address: "0x940181a94A35A4569E4529A3CDfB74e38FD98631", Decimals: 18},
	},
	"polygon": {
		"USDC":   {Address: "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359", Decimals: 6},
		"USDC.e": {Address: "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", Decimals: 6},
		"USDT":   {Address: "0xc2132D05D31c914a87C6611C10748AEb04B58e8F", Decimals: 6},
		"DAI":    {Address: "0x8f3Cf7ad23Cd3CaDbD9735AFf958023239c6A063", Decimals: 18},
		"WPOL":   {Address: "0x0d500B1d8E8eF31E21C99d1Db9A6444d3ADf1270", Decimals: 18},
		"WETH":   {Address: "0x7ceB23fD6bC0adD59E62ac25578270cFf1b9f619", Decimals: 18},
		"WBTC":   {Address: "0x1BFD67037B42Cf73acF2047067bd4F2C47D9BfD6", Decimals: 8},
		"LINK":   {Address: "0x53E0bca35eC356BD5ddDFebbd1Fc0fD03FaBad39", Decimals: 18},
		"AAVE":   {Address: "0xD6DF932A45C0f255f85145f286eA0b292B21C90B", Decimals: 18},
	},
	"bsc": {
		"USDC": {Address: "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d", Decimals: 18},
		"USDT": {Address: "0x55d398326f99059fF775485246999027B3197955", Decimals: 18},
		"BUSD": {Address: "0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56", Decimals: 18},
		"DAI":  {Address: "0x1AF3F329e8BE154074D8769D1FFa4eE058B1DBc3", Decimals: 18},
		"WBNB": {Address: "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", Decimals: 18},
		"BTCB": {Address: "0x7130d2A12B9BCbFAe4f2634d864A1Ee1Ce3Ead9c", Decimals: 18},
		"ETH":  {Address: "0x2170Ed0880ac9A755fd29B2688956BD959F933F8", Decimals: 18},
		"CAKE": {Address: "0x0E09FaBB73Bd3Ade0a17ECC321fD13a19e81cE82", Decimals: 18},
		"LINK": {Address: "0xF8A0BF9cF54Bb92F17374d9e9A321E6a111a51bD", Decimals: 18},
	},
}

func init() {
	config.SetCanonicalTokenProvider(CanonicalToken)
}

func CanonicalTokens(chainName string) map[string]config.CanonicalToken {
	tokens := canonicalTokens[strings.ToLower(strings.TrimSpace(chainName))]
	if len(tokens) == 0 {
		return map[string]config.CanonicalToken{}
	}
	out := make(map[string]config.CanonicalToken, len(tokens))
	for symbol, token := range tokens {
		out[symbol] = token
	}
	return out
}

func CanonicalToken(chainName, symbol string) (config.CanonicalToken, bool) {
	tokens := canonicalTokens[strings.ToLower(strings.TrimSpace(chainName))]
	if len(tokens) == 0 {
		return config.CanonicalToken{}, false
	}
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	for candidate, token := range tokens {
		if strings.ToUpper(candidate) == upper {
			return token, true
		}
	}
	return config.CanonicalToken{}, false
}

func IsValidERC20Address(value string) bool {
	return common.IsHexAddress(strings.TrimSpace(value)) && !token.IsNativeToken(value)
}
