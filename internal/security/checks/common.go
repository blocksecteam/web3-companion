// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

// Package checks holds the built-in static security check rules. Each file
// here defines one rule as a struct that implements one of the security
// package's typed Check interfaces (ShapeCheck, DataProducingCheck,
// CalldataCheck, LabelEmittingCheck, SimulationCheck, EffectCheck).
// RegisterDefaultChecks installs the whole built-in set into a
// security.CheckRegistry.
package checks

import (
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// RegisterDefaultChecks installs the built-in static security rules into reg.
// The labelProvider is needed by AddressLabelRisk; pass the same provider the
// Handler is configured with. nil is acceptable — the label check degrades
// gracefully to a pass result.
//
// Registration order follows the orchestrator's phase order: Shape,
// DataProducing, Calldata, LabelEmitting, Simulation, Effect. Within a
// phase the order determines the JSON response ordering surfaced to
// callers, so changes here are user-visible.
func RegisterDefaultChecks(reg *security.CheckRegistry, labelProvider security.AddressLabelProvider) {
	reg.RegisterShape(TransactionShape{})
	reg.RegisterShape(BurnAddressCheck{})

	reg.RegisterDataProducing(CalldataDecode{})

	reg.RegisterCalldata(UnlimitedApproval{})
	reg.RegisterCalldata(NFTApprovalForAll{})
	reg.RegisterCalldata(ERC20ApproveSanity{})

	reg.RegisterLabelEmitting(AddressLabelRisk{Provider: labelProvider})

	reg.RegisterSimulation(SimulationSuccess{})
	reg.RegisterSimulation(SimulationNonEmpty{})
	reg.RegisterSimulation(GasCostSanity{})
	reg.RegisterSimulation(BridgeDepositMatch{})
	reg.RegisterSimulation(TransferIntentMatch{})

	reg.RegisterEffect(USDValueConservation{})
	reg.RegisterEffect(AssetIntentMatch{})
}

// knownRouter is a check-private predicate used by ERC20ApproveSanity to
// classify a spender as an established router (versus an unknown contract).
// Lives here rather than in primitives because the lookup key set is part
// of the SecurityConfig surface — it would not be useful outside checks.
func knownRouter(chainID int, spender string, cfg security.SecurityConfig) bool {
	routers := cfg.KnownRoutersByChainID[chainID]
	if routers == nil {
		return false
	}
	return routers[strings.ToLower(spender)]
}

// hasActionCall reports whether the calldata represents a state-changing
// call other than a bare approve. SimulationNonEmpty uses it to distinguish
// "this transaction intends to move value" from "this transaction is a
// metadata-only no-op", which gates whether an empty simulation trace is a
// red flag.
func hasActionCall(data string) bool {
	parsed, err := primitives.ParseHexBytes(data)
	if err != nil {
		return false
	}
	if len(parsed) == 0 {
		return false
	}
	method := primitives.MethodID(parsed)
	return method != primitives.SelectorApprove && method != "0x"
}

// copyStringMap returns a shallow clone. ERC20ApproveSanity builds one base
// finding map per approve op and clones it for each sub-rule violation so
// the per-finding mutations don't bleed into each other.
func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
