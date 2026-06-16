// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

func TestMetaSleuthProviderBatchLabels(t *testing.T) {
	var gotPath, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("API-KEY")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200000,
			"message": "Success",
			"data": [{
				"address": "0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad",
				"chain_id": 1,
				"name_tag": "Uniswap Universal Router",
				"main_entity": "Uniswap",
				"main_entity_info": {
					"categories": [{"code": 3008, "name": "DEX"}],
					"attributes": [{"code": 4009, "name": "HOT WALLET"}]
				},
				"attributes": [{"code": 4009, "name": "HOT WALLET"}]
			}]
		}`))
	}))
	t.Cleanup(server.Close)

	provider := security.NewMetaSleuthLabelProvider(server.URL, "test-key", server.Client())
	labels, err := provider.Labels(t.Context(), 1, []string{"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad"})
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if gotPath != "/batch-labels" {
		t.Fatalf("path = %q, want /batch-labels", gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Fatalf("API-KEY = %q, want test-key", gotAPIKey)
	}
	if len(labels) != 1 {
		t.Fatalf("labels = %d, want 1", len(labels))
	}
	if labels[0].NameTag != "Uniswap Universal Router" || labels[0].MainEntity != "Uniswap" {
		t.Fatalf("unexpected label: %#v", labels[0])
	}
	if len(labels[0].Categories) != 1 || labels[0].Categories[0].Name != "DEX" {
		t.Fatalf("categories not normalized: %#v", labels[0].Categories)
	}
}

func TestMetaSleuthProviderFailsOnBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":400001,"message":"invalid api key"}`))
	}))
	t.Cleanup(server.Close)

	provider := security.NewMetaSleuthLabelProvider(server.URL, "bad-key", server.Client())
	if _, err := provider.Labels(t.Context(), 1, []string{"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad"}); err == nil {
		t.Fatal("Labels error = nil, want business error")
	}
}

func TestMetaSleuthProviderQueriesAllAddressesInOneBatch(t *testing.T) {
	addresses := []string{
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002",
		"0x0000000000000000000000000000000000000003",
	}
	var gotAddresses []string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			ChainID   int      `json:"chain_id"`
			Addresses []string `json:"addresses"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotAddresses = body.Addresses
		labels := ""
		for i, address := range body.Addresses {
			if i > 0 {
				labels += ","
			}
			labels += `{"address":"` + address + `","chain_id":1,"name_tag":"label"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200000,"message":"Success","data":[` + labels + `]}`))
	}))
	t.Cleanup(server.Close)

	provider := security.NewMetaSleuthLabelProvider(server.URL, "test-key", server.Client())
	labels, err := provider.Labels(t.Context(), 1, addresses)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if len(labels) != len(addresses) {
		t.Fatalf("labels = %d, want %d", len(labels), len(addresses))
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one batch request", requests)
	}
	if len(gotAddresses) != len(addresses) {
		t.Fatalf("request addresses = %v, want %v", gotAddresses, addresses)
	}
}
