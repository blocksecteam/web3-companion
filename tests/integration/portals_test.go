// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

func portalsGet(ctx context.Context, path string) ([]byte, error) {
	apiKey := os.Getenv("PORTALS_API_KEY")
	url := "https://api.portals.fi/v2" + path
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func TestPortals_SearchToken_USDC_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	if os.Getenv("PORTALS_API_KEY") == "" {
		t.Skip("PORTALS_API_KEY not set")
	}

	data, err := portalsGet(integrationCtx(t), "/tokens?search=USDC&platforms=basic&networks=ethereum&networks=base&limit=10")
	if err != nil {
		t.Fatalf("portals API: %v", err)
	}

	var wrapper struct {
		Tokens []map[string]interface{} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, string(data))
	}
	if len(wrapper.Tokens) == 0 {
		t.Fatalf("expected at least one result for USDC search\nbody: %s", string(data))
	}

	foundUSDC := false
	for _, r := range wrapper.Tokens {
		if symbol, ok := r["symbol"].(string); ok && symbol == "USDC" {
			foundUSDC = true
			decimals, _ := r["decimals"].(float64)
			address, _ := r["address"].(string)
			t.Logf("USDC found: decimals=%.0f, address=%s", decimals, address)
			if price, ok := r["price"].(float64); ok {
				t.Logf("USDC price: %.4f", price)
			}
			break
		}
	}
	if !foundUSDC {
		t.Error("USDC not found in search results")
	}
}

func TestPortals_TokenInfo_Ethereum_USDC_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	if os.Getenv("PORTALS_API_KEY") == "" {
		t.Skip("PORTALS_API_KEY not set")
	}

	url := fmt.Sprintf("/tokens?ids=ethereum:%s&limit=1", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	data, err := portalsGet(integrationCtx(t), url)
	if err != nil {
		t.Fatalf("portals API: %v", err)
	}

	var wrapper struct {
		Tokens []map[string]interface{} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, string(data))
	}
	if len(wrapper.Tokens) == 0 {
		t.Fatalf("expected token info for USDC on ethereum\nbody: %s", string(data))
	}
	token := wrapper.Tokens[0]

	symbol, _ := token["symbol"].(string)
	if symbol != "USDC" {
		t.Errorf("symbol = %q, want USDC", symbol)
	}
	decimals, _ := token["decimals"].(float64)
	if decimals != 6 {
		t.Errorf("decimals = %f, want 6", decimals)
	}
	if price, ok := token["price"].(float64); ok {
		t.Logf("USDC price: %.4f", price)
		if price < 0.5 || price > 2.0 {
			t.Errorf("USDC price %.4f out of range [0.5, 2.0]", price)
		}
	}
}

func TestPortals_TokenInfo_Base_USDC_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	if os.Getenv("PORTALS_API_KEY") == "" {
		t.Skip("PORTALS_API_KEY not set")
	}

	url := fmt.Sprintf("/tokens?ids=base:%s&limit=1", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
	data, err := portalsGet(integrationCtx(t), url)
	if err != nil {
		t.Fatalf("portals API: %v", err)
	}

	var wrapper struct {
		Tokens []map[string]interface{} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, string(data))
	}
	if len(wrapper.Tokens) == 0 {
		t.Fatalf("expected token info for USDC on base\nbody: %s", string(data))
	}
	token := wrapper.Tokens[0]

	symbol, _ := token["symbol"].(string)
	if symbol != "USDC" {
		t.Errorf("symbol = %q, want USDC", symbol)
	}
	t.Logf("Base USDC: symbol=%s", symbol)
}

func TestPortals_SearchToken_NoAPIKey_StillWorks(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	data, err := portalsGet(integrationCtx(t), "/tokens?platforms=basic&networks=ethereum&limit=3")
	if err != nil {
		t.Skipf("portals requires network: %v", err)
		return
	}
	var wrapper struct {
		Tokens []map[string]interface{} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, string(data))
	}
	t.Logf("search results (no API key): %d tokens", len(wrapper.Tokens))
}

func TestPortals_Account_Balance_Live(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	if os.Getenv("PORTALS_API_KEY") == "" {
		t.Skip("PORTALS_API_KEY not set")
	}

	url := fmt.Sprintf("/account?owner=%s&networks=ethereum", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	data, err := portalsGet(integrationCtx(t), url)
	if err != nil {
		t.Fatalf("portals API: %v", err)
	}

	var account map[string]interface{}
	if err := json.Unmarshal(data, &account); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if _, ok := account["totalNetWorth"]; !ok {
		if _, ok := account["balances"]; !ok {
			t.Error("missing totalNetWorth or balances in account response")
		} else {
			t.Logf("account response has balances field")
		}
	}
	if tokens, ok := account["tokens"]; ok {
		if tokensArr, ok := tokens.([]interface{}); ok {
			t.Logf("account has %d tokens", len(tokensArr))
		}
	}
	if balances, ok := account["balances"]; ok {
		if balancesArr, ok := balances.([]interface{}); ok {
			t.Logf("account has %d balance entries", len(balancesArr))
		}
	}
}

func TestPortals_MissingAPIKey_ReturnsError(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}

	t.Setenv("PORTALS_API_KEY", "")
	data, err := portalsGet(integrationCtx(t), "/account?owner=0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045&networks=ethereum")
	if err != nil {
		t.Fatalf("portals API: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, string(data))
	}
	if msg, _ := resp["message"].(string); msg == "" {
		t.Fatalf("expected unauthorized error message without API key, got: %s", string(data))
	}
}
