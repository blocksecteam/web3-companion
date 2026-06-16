// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestVersionCommandUsesInjectedBuildInfo(t *testing.T) {
	cli.SetBuildInfo("test-build")
	t.Cleanup(func() { cli.SetBuildInfo("") })

	for _, args := range [][]string{{"version"}, {"--version"}} {
		root := cli.NewRootCommand()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs(args)

		if err := root.Execute(); err != nil {
			t.Fatalf("web3 %v: %v", args, err)
		}
		var got map[string]string
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("web3 %v must output JSON, got %q: %v", args, out.String(), err)
		}
		if got["version"] != "test-build" {
			t.Fatalf("web3 %v version = %q, want test-build", args, got["version"])
		}
	}
}

func TestSwapCmdHasQuoteAndBuild(t *testing.T) {
	cmd := cli.SwapCmd()
	for _, name := range []string{"quote", "build"} {
		sub, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatalf("missing swap %s command: %v", name, err)
		}
		// Shared route flags both subcommands must carry.
		flags := []string{"chain", "from", "to", "amount", "protocol"}
		for _, flag := range flags {
			if sub.Flags().Lookup(flag) == nil {
				t.Fatalf("swap %s missing --%s flag", name, flag)
			}
		}
	}
	quote, _, _ := cmd.Find([]string{"quote"})
	if quote.Flags().Lookup("from-address") == nil {
		t.Fatal("swap quote missing --from-address flag")
	}
	if quote.Flags().Lookup("wallet") != nil {
		t.Fatal("swap quote should not register --wallet (only build needs it)")
	}
	build, _, _ := cmd.Find([]string{"build"})
	if build.Flags().Lookup("wallet") == nil {
		t.Fatal("swap build missing --wallet flag")
	}
	if build.Flags().Lookup("from-address") != nil {
		t.Fatal("swap build must not accept --from-address — the wallet's address is resolved from --wallet")
	}
}

func TestBuildSwapParamsSendsUserAmount(t *testing.T) {
	cmd := &cobra.Command{Use: "quote"}
	cli.AddSwapRouteFlags(cmd)
	// `from-address` is registered only by quote/build at the SwapCmd level, not
	// by AddSwapRouteFlags. Add it here so the test cmd mirrors the real quote
	// command shape.
	cmd.Flags().String("from-address", "", "Wallet/source address")
	if err := cmd.ParseFlags([]string{
		"--chain", "ethereum",
		"--from-address", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		"--from", "ETH",
		"--to", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"--amount", "0.001",
	}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	body := cli.BuildSwapParams(cmd)

	if v, ok := body["amount"]; !ok || v != "0.001" {
		t.Errorf("body[amount] = %v, want %q (user-supplied readable)", v, "0.001")
	}
}

func TestWriteCommandsRequireExplicitChain(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "ssm delegate", args: []string{"ssm", "delegate", "--wallet", "main"}, want: `required flag(s) "chain" not set`},
		{name: "chain transfer", args: []string{"chain", "transfer", "--wallet", "main", "--token", "native", "--to", "0x1234567890123456789012345678901234567890", "--amount", "1"}, want: `required flag(s) "chain" not set`},
		{name: "swap quote", args: []string{"swap", "quote", "--from-address", "0x1234567890123456789012345678901234567890", "--from", "native", "--to", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "--amount", "1"}, want: `required flag(s) "chain" not set`},
		{name: "bridge quote", args: []string{"bridge", "quote", "--to-chain", "base", "--from", "native", "--to", "native", "--amount", "1", "--from-address", "0x1234567890123456789012345678901234567890"}, want: `required flag(s) "from-chain" not set`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := cli.NewRootCommand()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("web3 %v error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestBridgeCmdHasBuild(t *testing.T) {
	cmd := cli.BridgeCmd()
	sub, _, err := cmd.Find([]string{"build"})
	if err != nil {
		t.Fatalf("missing bridge build command: %v", err)
	}
	for _, flag := range []string{"wallet", "from-chain", "to-chain", "from", "to", "amount", "bridge"} {
		if sub.Flags().Lookup(flag) == nil {
			t.Fatalf("bridge build missing --%s flag", flag)
		}
	}
	if sub.Flags().Lookup("from-address") != nil {
		t.Fatal("bridge build must not accept --from-address — the wallet's address is resolved from --wallet")
	}
	quote, _, _ := cmd.Find([]string{"quote"})
	if quote.Flags().Lookup("from-address") == nil {
		t.Fatal("bridge quote missing --from-address flag")
	}
}

func TestChainCmdHasBroadcast(t *testing.T) {
	cmd := cli.ChainCmd()
	sub, _, err := cmd.Find([]string{"broadcast"})
	if err != nil {
		t.Fatalf("missing chain broadcast command: %v", err)
	}
	if sub.Flags().Lookup("transaction-id") == nil {
		t.Fatal("chain broadcast missing --transaction-id flag")
	}
}

func TestSecurityCmdUsesTransactionID(t *testing.T) {
	cmd := cli.SecurityCmd()
	sub, _, err := cmd.Find([]string{"check"})
	if err != nil {
		t.Fatalf("missing security check command: %v", err)
	}
	for _, flag := range []string{"transaction-id"} {
		if sub.Flags().Lookup(flag) == nil {
			t.Fatalf("security check missing --%s flag", flag)
		}
	}
}

func TestSecurityCmdHasOverride(t *testing.T) {
	cmd := cli.SecurityCmd()
	sub, _, err := cmd.Find([]string{"override"})
	if err != nil {
		t.Fatalf("missing security override command: %v", err)
	}
	for _, flag := range []string{"transaction-id", "reason", "challenge-id"} {
		if sub.Flags().Lookup(flag) == nil {
			t.Fatalf("security override missing --%s flag", flag)
		}
	}
}

func TestBuildSwapParams_IncludesProtocol(t *testing.T) {
	cmd := &cobra.Command{}
	cli.AddSwapRouteFlags(cmd)
	cmd.Flags().String("wallet", "", "Wallet ID")
	_ = cmd.Flags().Set("chain", "ethereum")
	_ = cmd.Flags().Set("from-address", "0xabc")
	_ = cmd.Flags().Set("wallet", "wallet_1")
	_ = cmd.Flags().Set("from", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	_ = cmd.Flags().Set("to", "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE")
	_ = cmd.Flags().Set("amount", "100")
	_ = cmd.Flags().Set("protocol", "odos")
	got := cli.BuildSwapParams(cmd)
	if got["protocol"] != "odos" {
		t.Fatalf("expected protocol=odos, got %v", got["protocol"])
	}
	if got["chain"] != "ethereum" {
		t.Fatalf("expected chain=ethereum, got %v", got["chain"])
	}
	if got["wallet_id"] != "wallet_1" {
		t.Fatalf("expected wallet_id=wallet_1, got %v", got["wallet_id"])
	}
}

func TestBuildSecurityCheckParamsUsesTransactionID(t *testing.T) {
	got := cli.BuildSecurityCheckParams("txi_123")
	if got["transaction_id"] != "txi_123" {
		t.Fatalf("unexpected security payload: %#v", got)
	}
}

func TestBuildSecurityOverrideParamsIncludesChallengeID(t *testing.T) {
	got := cli.BuildSecurityOverrideParams(" txi_123 ", " user approved ", " ch_123 ")
	if got["transaction_id"] != "txi_123" || got["reason"] != "user approved" || got["challenge_id"] != "ch_123" {
		t.Fatalf("unexpected security override payload: %#v", got)
	}
}

func TestCompanionWriteGuardRejectsMissingContainerCredential(t *testing.T) {
	client := &cli.APIClient{}
	if err := cli.RequireContainerClient(client); err == nil {
		t.Fatal("RequireContainerClient should reject missing container credential")
	}
}

func TestCompanionWriteGuardAllowsContainerCredential(t *testing.T) {
	client := &cli.APIClient{ContainerCredential: &cli.ContainerCredential{
		CredentialID: "cred_123",
		Secret:       "secret",
	}}
	if err := cli.RequireContainerClient(client); err != nil {
		t.Fatalf("RequireContainerClient rejected container credential: %v", err)
	}
}

func TestBridgeCLI_RouteCommandsExposeAmountAndWalletFlags(t *testing.T) {
	cmd := cli.BridgeCmd()
	for _, name := range []string{"quote", "build"} {
		sub, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatalf("missing bridge %s command: %v", name, err)
		}
		if sub.Flags().Lookup("amount") == nil {
			t.Errorf("bridge %s: --amount flag not found", name)
		}
		if name != "quote" && sub.Flags().Lookup("wallet") == nil {
			t.Errorf("bridge %s: --wallet flag not found", name)
		}
	}
}
