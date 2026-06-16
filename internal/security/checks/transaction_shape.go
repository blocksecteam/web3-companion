// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// TransactionShape verifies that the incoming CheckRequest's mandatory address
// and integer fields are well-formed before any decoding or simulation runs.
// It is the first check the orchestrator dispatches and applies to every
// tx_type.
type TransactionShape struct{}

func (TransactionShape) Rule() string        { return "transaction_shape" }
func (TransactionShape) AppliesTo() []string { return nil }
func (TransactionShape) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Transaction Shape",
		Description: "Validates that the request's owner / to addresses are well-formed and that value parses as an integer before any decoding or simulation runs.",
		Statuses: map[string]string{
			"pass": "Mandatory request fields are well-formed.",
			"warn": "Request looks structurally valid but gas_limit is zero, so the simulator may estimate with incomplete metadata.",
			"fail": "Owner, to, or value is malformed — downstream checks cannot run safely.",
		},
	}
}
func (TransactionShape) Run(c security.ShapeContext) security.CheckResult {
	req := c.Req
	if !primitives.CommonAddress(req.Owner) {
		return security.CheckResult{Rule: "transaction_shape", Status: "fail", Detail: "owner is not a valid EVM address."}
	}
	if !primitives.CommonAddress(req.To) {
		return security.CheckResult{Rule: "transaction_shape", Status: "fail", Detail: "to is not a valid EVM address."}
	}
	if _, ok := primitives.ParseInteger(req.Value); !ok {
		return security.CheckResult{Rule: "transaction_shape", Status: "fail", Detail: "value is not a valid integer."}
	}
	if req.GasLimit == 0 {
		return security.CheckResult{Rule: "transaction_shape", Status: "warn", Detail: "gas_limit is zero; simulator may estimate with incomplete transaction metadata."}
	}
	return security.CheckResult{Rule: "transaction_shape", Status: "pass", Detail: "transaction fields are well-formed."}
}
