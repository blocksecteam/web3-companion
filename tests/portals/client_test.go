// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package portals_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/portals"
)

func TestClientTokenAmountUsesTokenMetadataAndAuthorization(t *testing.T) {
	var gotAuth string
	var gotIDs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotIDs = r.URL.Query().Get("ids")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tokens":[{"price":2.5,"decimals":6}]}`))
	}))
	defer srv.Close()

	client := portals.NewClient(
		portals.WithBaseURL(srv.URL),
		portals.WithHTTPClient(srv.Client()),
		portals.WithAPIKey("secret"),
	)
	amount, usd, ok := client.TokenAmount(context.Background(), "ethereum", portals.NativeAddress, "1234567")
	if !ok {
		t.Fatal("TokenAmount ok = false, want true")
	}
	if amount != "1.234567" {
		t.Fatalf("amount = %q, want 1.234567", amount)
	}
	if usd != "3.0864175" {
		t.Fatalf("usd = %q, want 3.0864175", usd)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", gotAuth)
	}
	if decoded, _ := url.QueryUnescape(gotIDs); decoded != "ethereum:"+portals.ZeroAddress {
		t.Fatalf("ids = %q, want native zero address", decoded)
	}
}
