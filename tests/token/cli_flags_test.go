// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token_test

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestTokenSearch_RegistersChainFlag_NotNetworks(t *testing.T) {
	cmd := cli.TokenCmd()
	sub, _, err := cmd.Find([]string{"search"})
	if err != nil {
		t.Fatalf("token search command not found: %v", err)
	}
	if sub.Flags().Lookup("networks") != nil {
		t.Error("token search must register --chain flag (not --networks) for consistency with other commands")
	}
	if sub.Flags().Lookup("chain") == nil {
		t.Error("token search must register --chain flag")
	}
}

func TestTokenInfo_UsesChainFlag_NotPositionalArg(t *testing.T) {
	cmd := cli.TokenCmd()
	sub, _, err := cmd.Find([]string{"info"})
	if err != nil {
		t.Fatalf("token info command not found: %v", err)
	}
	if sub.Flags().Lookup("chain") == nil {
		t.Error("token info must use --chain flag")
	}
	if sub.Use == "info <chain> <address>" {
		t.Error("token info must use --chain flag, not positional <chain> arg")
	}
}
