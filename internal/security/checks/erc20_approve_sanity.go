// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// ERC20ApproveSanity bundles four sub-rules over each approve call in the
// decoded operations:
//
//  1. known-router whitelist match (cfg.KnownRoutersByChainID)
//  2. amount is not MAX_UINT256
//  3. an immediately-following call actually consumes the approval ("dangling")
//  4. when tx_summary.approve_target is present, the approve spender must
//     equal it — catches calldata that approves a different spender than the
//     swap/bridge build phase committed to (typical router-swap attack)
//
// Intentionally NOT split into four separate checks: all sub-rules share the
// same approve-decoding loop and emit a unified finding list. Splitting would
// quadruple the work per approve op for no gain.
//
// Severity model: sub-rules 1–3 are advisory (warn). Sub-rule 4 is fail —
// a calldata/summary mismatch is direct evidence of intent tampering. The
// overall status escalates: any fail-level finding → fail; otherwise any
// finding → warn; otherwise pass.
type ERC20ApproveSanity struct{}

func (ERC20ApproveSanity) Rule() string        { return "erc20_approve_sanity" }
func (ERC20ApproveSanity) AppliesTo() []string { return nil }
func (ERC20ApproveSanity) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "ERC-20 Approve Sanity",
		Description: "Audits every ERC-20 approve in the calldata: spender is in the known-router whitelist, amount is not MAX_UINT256, an immediately-following call consumes the approval, and (when tx_summary.approve_target is present) the spender matches the value the swap/bridge build phase committed to.",
		Statuses: map[string]string{
			"pass": "All approve calls (or none at all) match known routers, have bounded amounts, are followed by a consuming call, and (when applicable) match the declared approve_target.",
			"warn": "One or more advisory sub-rules flagged: unknown spender, MAX_UINT256 amount, or no consuming call follows.",
			"fail": "An approve spender does not match tx_summary.approve_target (intent mismatch).",
		},
	}
}
func (ERC20ApproveSanity) Run(c security.CalldataContext) security.CheckResult {
	req, ops, cfg := c.Req, c.Ops, c.Cfg
	summaryApproveTarget := extractApproveTarget(req.SummaryJSON)

	findings := make([]map[string]string, 0)
	approveCount := 0
	hasFail := false
	for i, op := range ops {
		spender, amount, ok := primitives.DecodeApprove(op.Data)
		if !ok {
			continue
		}
		approveCount++
		loc := map[string]string{
			"operation": fmt.Sprintf("%d", i),
			"token":     op.To.Hex(),
			"spender":   spender.Hex(),
			"amount":    amount.String(),
		}
		if !knownRouter(req.ChainID, spender.Hex(), cfg) {
			finding := copyStringMap(loc)
			finding["check"] = "unknown_spender"
			finding["detail"] = fmt.Sprintf("spender %s is not in the known-router whitelist for chain %d.", spender.Hex(), req.ChainID)
			findings = append(findings, finding)
		}
		if amount.Cmp(primitives.MaxUint256()) == 0 {
			finding := copyStringMap(loc)
			finding["check"] = "unlimited_amount"
			finding["detail"] = "approve amount is MAX_UINT256."
			findings = append(findings, finding)
		}
		if i+1 >= len(ops) || !primitives.AddrEq(ops[i+1].To.Hex(), spender.Hex()) {
			finding := copyStringMap(loc)
			finding["check"] = "dangling_approve"
			if i+1 >= len(ops) {
				finding["detail"] = "approve is the last operation; no consuming call follows it."
			} else {
				finding["detail"] = fmt.Sprintf("approve is followed by %s, not spender %s.", ops[i+1].To.Hex(), spender.Hex())
			}
			findings = append(findings, finding)
		}
		if summaryApproveTarget != "" && !primitives.AddrEq(spender.Hex(), summaryApproveTarget) {
			finding := copyStringMap(loc)
			finding["check"] = "summary_mismatch"
			finding["expected_spender"] = summaryApproveTarget
			finding["detail"] = fmt.Sprintf("approve spender %s does not match tx_summary.approve_target %s — the calldata is approving a different contract than the swap/bridge build committed to.", spender.Hex(), summaryApproveTarget)
			findings = append(findings, finding)
			hasFail = true
		}
	}
	if len(findings) == 0 {
		if approveCount == 0 {
			return security.CheckResult{Rule: "erc20_approve_sanity", Status: "pass", Detail: "no ERC-20 approve calls."}
		}
		return security.CheckResult{Rule: "erc20_approve_sanity", Status: "pass", Detail: fmt.Sprintf("%d approve call(s) look sane.", approveCount)}
	}
	status := "warn"
	if hasFail {
		status = "fail"
	}
	return security.CheckResult{
		Rule:     "erc20_approve_sanity",
		Status:   status,
		Detail:   fmt.Sprintf("%d approve sanity finding(s) across %d approve call(s).", len(findings), approveCount),
		Observed: findings,
	}
}

// extractApproveTarget reads tx_summary.approve_target from the raw JSON
// blob the orchestrator attaches as req.SummaryJSON. Returns an empty
// string when the JSON is absent, malformed, or has no approve_target —
// in any of those cases the summary_mismatch sub-rule is silently inactive
// (the other three sub-rules still fire as before).
//
// Lowercased on the way out so address comparison against the (case-folding)
// AddrEq helper is unambiguous.
func extractApproveTarget(rawJSON string) string {
	if rawJSON == "" {
		return ""
	}
	var summary struct {
		ApproveTarget string `json:"approve_target"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &summary); err != nil {
		return ""
	}
	return strings.TrimSpace(summary.ApproveTarget)
}
