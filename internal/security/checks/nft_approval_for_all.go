// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// NFTApprovalForAll flags ERC-721/1155 setApprovalForAll(operator, true)
// grants in the decoded operations. Applies to every tx_type — granting
// operator-wide control is rarely intentional regardless of the surrounding
// transaction type.
type NFTApprovalForAll struct{}

func (NFTApprovalForAll) Rule() string        { return "nft_approval_for_all" }
func (NFTApprovalForAll) AppliesTo() []string { return nil }
func (NFTApprovalForAll) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "NFT Approval-For-All",
		Description: "Detects ERC-721 / ERC-1155 setApprovalForAll(operator, true) grants. Operator-wide approvals let the spender move every NFT the owner currently holds and any future NFTs in the same collection.",
		Statuses: map[string]string{
			"pass": "No setApprovalForAll grants in the calldata.",
			"fail": "At least one setApprovalForAll grants operator-wide control of an NFT collection.",
		},
	}
}
func (NFTApprovalForAll) Run(c security.CalldataContext) security.CheckResult {
	findings := make([]map[string]string, 0)
	for i, op := range c.Ops {
		operator, approved, ok := primitives.DecodeSetApprovalForAll(op.Data)
		if !ok || !approved {
			continue
		}
		findings = append(findings, map[string]string{
			"operation": fmt.Sprintf("%d", i),
			"token":     op.To.Hex(),
			"operator":  operator.Hex(),
		})
	}
	if len(findings) > 0 {
		return security.CheckResult{
			Rule:     "nft_approval_for_all",
			Status:   "fail",
			Detail:   fmt.Sprintf("%d setApprovalForAll grant(s) detected.", len(findings)),
			Observed: findings,
		}
	}
	return security.CheckResult{Rule: "nft_approval_for_all", Status: "pass", Detail: "no setApprovalForAll grants."}
}
