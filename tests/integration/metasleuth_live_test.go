// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

const addrUniswapRouter = "0x3fC91A3afd70395cd496c647D5a6CC9D4B2b7FAD"

func TestMetaSleuth_AddressLabels_Live(t *testing.T) {
	if !checkIntegrationEnabledFromDotenv(t) {
		return
	}

	apiKey := strings.TrimSpace(os.Getenv("METASLEUTH_API_KEY"))
	if apiKey == "" {
		t.Skip("METASLEUTH_API_KEY not set")
	}

	ctx := integrationCtx(t)
	provider := security.NewMetaSleuthLabelProvider("", apiKey, nil)
	labels, err := provider.Labels(ctx, 1, []string{addrUniswapRouter})
	if err != nil {
		t.Fatalf("MetaSleuth labels: %v", err)
	}
	if len(labels) == 0 {
		t.Fatal("expected at least one label for Uniswap Universal Router")
	}

	label := labels[0]
	if !strings.EqualFold(label.Address, addrUniswapRouter) {
		t.Fatalf("label address = %q, want %q", label.Address, addrUniswapRouter)
	}
	if label.ChainID != 1 {
		t.Fatalf("label chain_id = %d, want 1", label.ChainID)
	}
	if label.NameTag == "" && label.MainEntity == "" {
		t.Errorf("expected NameTag or MainEntity to be non-empty, got %#v", label)
	}
}
