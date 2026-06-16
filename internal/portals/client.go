// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package portals

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.portals.fi/v2"
	ZeroAddress    = "0x0000000000000000000000000000000000000000"
	NativeAddress  = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if strings.TrimSpace(baseURL) != "" {
			c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
		}
	}
}

func WithAPIKey(apiKey string) Option {
	return func(c *Client) {
		c.apiKey = strings.TrimSpace(apiKey)
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.http = client
		}
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) RawGet(ctx context.Context, path string, values url.Values) ([]byte, error) {
	if c == nil {
		c = NewClient()
	}
	requestURL := c.baseURL + path
	if encoded := values.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("portals API error (HTTP %d) for %s: %s", resp.StatusCode, requestURL, string(data))
	}
	return data, nil
}

type TokenInfo struct {
	Price    json.Number `json:"price"`
	Decimals int         `json:"decimals"`
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

func (c *Client) Token(ctx context.Context, chain, tokenAddress string) (TokenInfo, bool, error) {
	values := url.Values{}
	values.Set("ids", fmt.Sprintf("%s:%s", strings.TrimSpace(chain), NormalizeTokenAddress(tokenAddress)))
	values.Set("limit", "1")
	data, err := c.RawGet(ctx, "/tokens", values)
	if err != nil {
		return TokenInfo{}, false, err
	}
	var resp struct {
		Tokens []TokenInfo `json:"tokens"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return TokenInfo{}, false, err
	}
	if len(resp.Tokens) == 0 {
		return TokenInfo{}, false, nil
	}
	return resp.Tokens[0], true, nil
}

func (c *Client) TokenAmount(ctx context.Context, chain, tokenAddress, rawAmount string) (amount string, usdValue string, ok bool) {
	token, found, err := c.Token(ctx, chain, tokenAddress)
	if err != nil || !found || token.Decimals < 0 {
		return "", "", false
	}
	raw, ok := parseRawAmount(rawAmount)
	if !ok {
		return "", "", false
	}
	priceRat, ok := decimalRat(token.Price.String())
	if !ok || priceRat.Sign() == 0 {
		return "", "", false
	}
	amountRat := new(big.Rat).SetInt(raw)
	amountRat.Quo(amountRat, new(big.Rat).SetInt(pow10(token.Decimals)))
	usdRat := new(big.Rat).Mul(new(big.Rat).Set(amountRat), priceRat)
	return formatRat(amountRat), formatRat(usdRat), true
}

func (c *Client) USDPrice(ctx context.Context, chain, tokenAddress string) (float64, bool) {
	token, found, err := c.Token(ctx, chain, tokenAddress)
	if err != nil || !found {
		return 0, false
	}
	price, ok := new(big.Rat).SetString(token.Price.String())
	if !ok || price.Sign() == 0 {
		return 0, false
	}
	value, _ := price.Float64()
	return value, true
}

func (c *Client) AccountBalances(ctx context.Context, chains []string, owner string) ([]TokenBalance, error) {
	values := url.Values{}
	values.Set("owner", owner)
	for _, chain := range chains {
		if strings.TrimSpace(chain) != "" {
			values.Add("networks", strings.TrimSpace(chain))
		}
	}
	data, err := c.RawGet(ctx, "/account", values)
	if err != nil {
		return nil, err
	}
	var account struct {
		Balances []TokenBalance `json:"balances"`
	}
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("parse account data: %w", err)
	}
	if account.Balances == nil {
		account.Balances = []TokenBalance{}
	}
	return account.Balances, nil
}

func NormalizeTokenAddress(tokenAddress string) string {
	switch strings.ToLower(strings.TrimSpace(tokenAddress)) {
	case "native", NativeAddress, ZeroAddress, "0x0":
		return ZeroAddress
	default:
		return strings.TrimSpace(tokenAddress)
	}
}

func parseRawAmount(rawAmount string) (*big.Int, bool) {
	rawAmount = strings.TrimSpace(rawAmount)
	if rawAmount == "" {
		return nil, false
	}
	base := 10
	if strings.HasPrefix(rawAmount, "0x") || strings.HasPrefix(rawAmount, "0X") {
		base = 0
	}
	n := new(big.Int)
	if _, ok := n.SetString(rawAmount, base); !ok {
		return nil, false
	}
	if n.Sign() < 0 {
		n.Abs(n)
	}
	return n, true
}

func pow10(decimals int) *big.Int {
	if decimals <= 0 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
}

func decimalRat(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, false
	}
	return r, true
}

func formatRat(value *big.Rat) string {
	if value == nil || value.Sign() == 0 {
		return "0"
	}
	return trimDecimal(value.FloatString(18))
}

func trimDecimal(value string) string {
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "" || value == "-0" {
		return "0"
	}
	return value
}
