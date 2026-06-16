// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

const portalsResp_USDC = `{
  "tokens": [
    {"network":"ethereum","address":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48","symbol":"USDC","decimals":6}
  ]
}`

const portalsResp_Empty = `{"tokens": []}`

func fakePortals(t *testing.T, response string, status int, expectedID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/tokens") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("ids"); got != expectedID {
			t.Errorf("ids query = %q, want %q", got, expectedID)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
}

func TestPortalsDecimals_USDC(t *testing.T) {
	ts := fakePortals(t, portalsResp_USDC, 200, "ethereum:0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	defer ts.Close()

	src := swap.NewPortalsDecimalsSource(ts.URL, "test-api-key", http.DefaultClient)
	d, err := src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d != 6 {
		t.Errorf("got %d, want 6", d)
	}
}

func TestPortalsDecimals_NotFound(t *testing.T) {
	ts := fakePortals(t, portalsResp_Empty, 200, "ethereum:0xunknown000000000000000000000000000000000")
	defer ts.Close()

	src := swap.NewPortalsDecimalsSource(ts.URL, "k", http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xunknown000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error when Portals returns empty list")
	}
}

func TestPortalsDecimals_HTTPError(t *testing.T) {
	ts := fakePortals(t, `unauthorized`, 401, "ethereum:0xanything")
	defer ts.Close()

	src := swap.NewPortalsDecimalsSource(ts.URL, "k", http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xanything")
	if err == nil {
		t.Fatal("expected error on HTTP 401")
	}
}

func TestPortalsDecimals_BearerHeaderSent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing Bearer auth header; got %q", auth)
		}
		_, _ = w.Write([]byte(portalsResp_USDC))
	}))
	defer ts.Close()

	src := swap.NewPortalsDecimalsSource(ts.URL, "my-secret-key", http.DefaultClient)
	_, _ = src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
}

func TestPortalsDecimals_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()

	src := swap.NewPortalsDecimalsSource(ts.URL, "k", http.DefaultClient)
	_, err := src.Decimals(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}
