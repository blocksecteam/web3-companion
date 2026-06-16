// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// UnlimitedApproval flags ERC-20 approve calls whose amount equals
// MAX_UINT256. It looks only at decoded calldata; runs for every tx_type.
type UnlimitedApproval struct{}

func (UnlimitedApproval) Rule() string        { return "unlimited_approval" }
func (UnlimitedApproval) AppliesTo() []string { return nil }
func (UnlimitedApproval) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Unlimited Approval",
		Description: "Detects ERC-20 approve calls whose amount equals MAX_UINT256. Such approvals let the spender drain the token at any future point without re-approval, well beyond the current transaction's intent.",
		Statuses: map[string]string{
			"pass": "No unlimited ERC-20 approvals in the calldata.",
			"fail": "At least one approve call grants MAX_UINT256 — sign only if you trust the spender forever.",
		},
	}
}
func (UnlimitedApproval) Run(c security.CalldataContext) security.CheckResult {
	findings := make([]map[string]string, 0)
	for i, op := range c.Ops {
		spender, amount, ok := primitives.DecodeApprove(op.Data)
		if !ok {
			continue
		}
		if amount.Cmp(primitives.MaxUint256()) == 0 {
			findings = append(findings, map[string]string{
				"operation": fmt.Sprintf("%d", i),
				"token":     op.To.Hex(),
				"spender":   spender.Hex(),
			})
		}
	}
	if len(findings) > 0 {
		return security.CheckResult{
			Rule:     "unlimited_approval",
			Status:   "fail",
			Detail:   fmt.Sprintf("%d unlimited ERC-20 approval(s) detected.", len(findings)),
			Observed: findings,
		}
	}
	return security.CheckResult{Rule: "unlimited_approval", Status: "pass", Detail: "no unlimited ERC-20 approvals."}
}
