// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

func TestSourcifyFunctionSignatureResolverLooksUpSelectors(t *testing.T) {
	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"function": map[string]interface{}{
					"0xa9059cbb": []map[string]interface{}{{"name": "transfer(address,uint256)", "hasVerifiedContract": true}},
					"0x12345678": nil,
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	resolver := security.NewSourcifyFunctionSignatureResolver(server.URL, server.Client())
	got, err := resolver.ResolveFunctions(t.Context(), []string{"0xa9059cbb", "0x12345678", "0xa9059cbb", "0x"})
	if err != nil {
		t.Fatalf("ResolveFunctions: %v", err)
	}
	if gotPath != "/signature-database/v1/lookup" {
		t.Fatalf("path = %q, want lookup endpoint", gotPath)
	}
	if !strings.Contains(gotQuery, "function=0xa9059cbb%2C0x12345678") && !strings.Contains(gotQuery, "function=0xa9059cbb,0x12345678") {
		t.Fatalf("query = %q, want comma-delimited selectors", gotQuery)
	}
	if got["0xa9059cbb"] != "transfer(address,uint256)" {
		t.Fatalf("resolved selector = %#v", got)
	}
	if _, ok := got["0x12345678"]; ok {
		t.Fatalf("unmatched selector should be absent: %#v", got)
	}
	if _, ok := got["0x"]; ok {
		t.Fatalf("invalid selector should be absent: %#v", got)
	}
}
