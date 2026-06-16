// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli_test

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestContextCmdExposesStatus(t *testing.T) {
	cmd := cli.ContextCmd()

	sub, _, err := cmd.Find([]string{"status"})
	if err != nil {
		t.Fatalf("missing context status command: %v", err)
	}
	if sub.Name() != "status" {
		t.Fatalf("context status resolved to %s", sub.Name())
	}
}
