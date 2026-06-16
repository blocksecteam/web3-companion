// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"sort"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
	securitychecks "github.com/blocksecteam/web3-companion/internal/security/checks"
)

// TestSecurity_Registry_AllChecksRegistered guards the registry against a
// missing or typo'd RegisterDefaultChecks line. Existing handler tests run
// through HTTP and look up specific rules by name, so a forgotten check
// would only surface as a confusing "rule not found" message in some
// unrelated test. This test fails loudly and lists what's missing.
//
// Mirrors swap's TestSwap_Registry_AllAdaptersRegistered (tests/swap/
// adapters_test.go) and bridge's adapters_test.go.
func TestSecurity_Registry_AllChecksRegistered(t *testing.T) {
	reg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(reg, nil)

	got := reg.Rules()
	want := []string{
		"transaction_shape",
		"burn_address_check",
		"calldata_decode",
		"unlimited_approval",
		"nft_approval_for_all",
		"erc20_approve_sanity",
		"address_label_risk",
		"simulation_success",
		"simulation_non_empty",
		"gas_cost_sanity",
		"bridge_deposit_match",
		"transfer_intent_match",
		"usd_value_conservation",
		"asset_intent_match",
	}

	if len(got) != len(want) {
		t.Errorf("registered check count = %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for _, expected := range want {
		found := false
		for _, name := range got {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rule %q missing from registry", expected)
		}
	}

	// Detect stray rule names that snuck in via a copy-paste or merge mishap.
	sortedGot := append([]string(nil), got...)
	sort.Strings(sortedGot)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	wantSet := make(map[string]struct{}, len(sortedWant))
	for _, name := range sortedWant {
		wantSet[name] = struct{}{}
	}
	for _, name := range sortedGot {
		if _, ok := wantSet[name]; !ok {
			t.Errorf("unexpected rule %q in registry (update this test if intentional)", name)
		}
	}
}
