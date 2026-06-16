// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestChainTransferCLICommand_Exists(t *testing.T) {
	cmd := cli.ChainCmd()
	if _, _, err := cmd.Find([]string{"transfer"}); err != nil {
		t.Fatalf("web3 chain transfer subcommand must exist in CLI: %v", err)
	}
}

func TestChainTransferCLICommand_HasRequiredFlags(t *testing.T) {
	cmd := cli.ChainCmd()
	sub, _, err := cmd.Find([]string{"transfer"})
	if err != nil {
		t.Fatal("transfer command not found")
	}
	for _, flag := range []string{"token", "to", "amount", "wallet"} {
		if sub.Flags().Lookup(flag) == nil {
			t.Errorf("transfer command must have --%s flag", flag)
		}
	}
}

func TestChainTransferCLICommand_HasChainFlag(t *testing.T) {
	cmd := cli.ChainCmd()
	sub, _, err := cmd.Find([]string{"transfer"})
	if err != nil {
		t.Fatal("transfer command not found")
	}
	if sub.Flags().Lookup("chain") == nil {
		t.Error("transfer command must have --chain flag")
	}
}

func TestChainTransferCLICommand_BuildOnly(t *testing.T) {
	cli.ResetAPIClientForTest()
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/chain/transfer":
			_, _ = w.Write([]byte(`{"status":"built","transaction_id":"txi_transfer"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("WEB3_API_URL", server.URL)
	t.Setenv("HOME", t.TempDir())
	writeContainerCredential(t)

	output := captureStdout(t, func() {
		cmd := cli.ChainCmd()
		cmd.SetArgs([]string{
			"transfer",
			"--chain", "ethereum",
			"--token", "native",
			"--to", "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510",
			"--amount", "0.001",
			"--wallet", "wallet_transfer",
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute transfer: %v", err)
		}
	})

	if !strings.Contains(output, `"status": "built"`) || !strings.Contains(output, `"transaction_id": "txi_transfer"`) {
		t.Fatalf("build output missing intent fields:\n%s", output)
	}
	if len(requested) != 1 || requested[0] != "POST /api/v1/chain/transfer" {
		t.Fatalf("transfer should only build intent, got requests: %v", requested)
	}
}

func writeContainerCredential(t *testing.T) {
	t.Helper()
	stateDir := os.Getenv("HOME") + "/.local/web3"
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(stateDir+"/container-credential.json", []byte(`{"credential_id":"cc_test","secret":"secret"}`), 0o600); err != nil {
		t.Fatalf("write container credential: %v", err)
	}
}
