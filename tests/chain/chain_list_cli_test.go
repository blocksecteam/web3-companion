// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/cli"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func chainListOutput(t *testing.T) string {
	t.Helper()
	r, _ := setupChainRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/supported", "", nil)
	return w.Body.String()
}

func TestChainListEndpoint_ContainsAllChainNames(t *testing.T) {
	body := chainListOutput(t)
	var chains []map[string]interface{}
	if err := json.Unmarshal([]byte(body), &chains); err != nil {
		t.Fatalf("parse: %v", err)
	}
	names := make([]string, len(chains))
	for i, c := range chains {
		names[i] = c["name"].(string)
	}
	for _, want := range []string{"ethereum", "base"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("chain %q not found in response, got: %v", want, names)
		}
	}
}

func TestChainListEndpoint_TokenDecimalsPresent(t *testing.T) {

	r, _ := setupChainRouterWithConfig(t)
	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/chain/supported", "", nil)
	var chains []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &chains)

	found := false
	for _, c := range chains {
		if c["name"] == "bsc" {
			found = true
			tokens := c["canonical_tokens"].(map[string]interface{})
			usdc := tokens["USDC"].(map[string]interface{})
			dec := usdc["decimals"].(float64)
			if dec != 18 {
				t.Errorf("BSC USDC decimals should be 18, got %v", dec)
			}
		}
	}
	if !found {
		t.Log("bsc chain not in setupChainRouterWithConfig config, skip decimal check")
	}
}

func TestChainListEndpoint_ResponseIsValidJSON(t *testing.T) {
	body := chainListOutput(t)
	if !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("expected JSON array response, got: %s", body[:min(50, len(body))])
	}
	var result []interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Errorf("response is not valid JSON: %v", err)
	}
}

func TestChainListCLIOutputsFormattedJSON(t *testing.T) {
	cli.ResetAPIClientForTest()
	r, _ := setupChainRouterWithConfig(t)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	t.Setenv("WEB3_API_URL", server.URL)
	t.Setenv("HOME", t.TempDir())

	cmd := cli.ChainCmd()
	cmd.SetArgs([]string{"list"})

	output := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute chain list: %v", err)
		}
	})
	if strings.Contains(output, "Supported Chains") {
		t.Fatalf("chain list should not output a markdown/text table:\n%s", output)
	}
	var chains []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &chains); err != nil {
		t.Fatalf("chain list must output JSON, got %q: %v", output, err)
	}
	if len(chains) == 0 {
		t.Fatalf("chain list returned no chains: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("\n  ")) {
		t.Fatalf("chain list should output formatted JSON, got: %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
