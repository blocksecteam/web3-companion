// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"github.com/blocksecteam/web3-companion/internal/security"
)

// CalldataDecode parses the request's calldata into a list of operations the
// CalldataChecks downstream consume, and reports a check result describing
// whether decoding succeeded.
//
// It implements security.DataProducingCheck — runs between the Shape phase
// and the Calldata phase. The decoder logic itself lives in
// security.DecodeOperations; this struct is the thin Check-interface
// wrapper that lets the orchestrator iterate the registry uniformly and
// pick up Name / Description / per-status meaning via the standard
// decorateWithDescription path.
type CalldataDecode struct{}

func (CalldataDecode) Rule() string        { return "calldata_decode" }
func (CalldataDecode) AppliesTo() []string { return nil }
func (CalldataDecode) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Calldata Decode",
		Description: "Parses the transaction's calldata into individual operations — a single direct call, or the per-call list of an ERC-7821 execute batch — so downstream calldata checks have something to inspect.",
		Statuses: map[string]string{
			"pass": "Calldata parsed cleanly into one or more operations.",
			"fail": "Calldata is malformed or the ERC-7821 batch could not be decoded; downstream calldata checks cannot run meaningfully.",
		},
	}
}
func (CalldataDecode) Run(c security.ShapeContext) (security.CheckResult, []security.Operation) {
	ops, res := security.DecodeOperations(c.Req)
	return res, ops
}
