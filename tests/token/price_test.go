// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestComputeAmountUSD_PriceAvailable_ReturnsTrueAndAmount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tokens":[{"price":2000.0}]}`))
	}))
	defer srv.Close()
	token.SetPortalsBaseURL(srv.URL)
	defer token.SetPortalsBaseURL("https://api.portals.fi")

	amount, known := token.ComputeAmountUSD(context.Background(), "ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "0.5")
	if !known {
		t.Error("expected known=true")
	}
	if amount.String() != "1000" {
		t.Errorf("expected 1000.0, got %v", amount)
	}
}

func TestComputeAmountUSD_UsesDecimalMultiplication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tokens":[{"price":0.2}]}`))
	}))
	defer srv.Close()
	token.SetPortalsBaseURL(srv.URL)
	defer token.SetPortalsBaseURL("https://api.portals.fi")

	amount, known := token.ComputeAmountUSD(context.Background(), "ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "0.1")
	if !known {
		t.Error("expected known=true")
	}
	if amount.String() != "0.02" {
		t.Errorf("expected decimal product 0.02, got %s", amount.String())
	}
}

func TestComputeAmountUSD_PriceUnavailable_ReturnsFalseAndZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tokens":[]}`))
	}))
	defer srv.Close()
	token.SetPortalsBaseURL(srv.URL)
	defer token.SetPortalsBaseURL("https://api.portals.fi")

	amount, known := token.ComputeAmountUSD(context.Background(), "ethereum", "0xunknown", "1.0")
	if known {
		t.Error("expected known=false for unknown token")
	}
	if amount.String() != "0" {
		t.Errorf("expected 0, got %v", amount)
	}
}

func TestPortalsValuatorTokenAmountUsesChainAddressDecimalsAndPrice(t *testing.T) {
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.URL.Query().Get("ids"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tokens":[{"price":1.0,"decimals":18}]}`))
	}))
	defer srv.Close()

	valuator := token.NewPortalsValuator(token.WithPortalsURL(srv.URL), token.WithHTTPClient(srv.Client()))
	amount, usdValue, ok := valuator.TokenAmount(context.Background(), "bsc", "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d", "1000000000000000000")
	if !ok {
		t.Fatal("TokenAmount ok = false, want true")
	}
	if amount != "1" {
		t.Fatalf("amount = %q, want 1", amount)
	}
	if usdValue != "1" {
		t.Fatalf("usdValue = %q, want 1", usdValue)
	}
	if len(ids) != 1 {
		t.Fatalf("requests = %d, want 1", len(ids))
	}
	wantID := "bsc:0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"
	if decoded, _ := url.QueryUnescape(ids[0]); decoded != wantID {
		t.Fatalf("ids = %q, want %q", decoded, wantID)
	}
}

func TestPortalsValuatorTokenAmountSupportsNativeAlias(t *testing.T) {
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.URL.Query().Get("ids"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tokens":[{"price":2500.0,"decimals":18}]}`))
	}))
	defer srv.Close()

	valuator := token.NewPortalsValuator(token.WithPortalsURL(srv.URL), token.WithHTTPClient(srv.Client()))
	amount, usdValue, ok := valuator.TokenAmount(context.Background(), "ethereum", token.EIP155NativeAddress, "100000000000000000")
	if !ok {
		t.Fatal("TokenAmount ok = false, want true")
	}
	if amount != "0.1" {
		t.Fatalf("amount = %q, want 0.1", amount)
	}
	if usdValue != "250" {
		t.Fatalf("usdValue = %q, want 250", usdValue)
	}
	if decoded, _ := url.QueryUnescape(ids[0]); decoded != "ethereum:"+token.ZeroAddress {
		t.Fatalf("ids = %q, want native zero address", decoded)
	}
}

func TestFetchUSDPrice_TimesOutSlowServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	token.SetPortalsBaseURL(srv.URL)
	defer token.SetPortalsBaseURL("https://api.portals.fi")

	start := time.Now()
	_, ok := token.FetchUSDPrice(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	elapsed := time.Since(start)

	if ok {
		t.Error("expected ok=false for slow server")
	}
	if elapsed > 8*time.Second {
		t.Errorf("expected timeout within 8s, took %v", elapsed)
	}
}
