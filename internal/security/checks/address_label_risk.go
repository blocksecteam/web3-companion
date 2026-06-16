// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/blocksecteam/web3-companion/internal/security"
)

// AddressLabelRisk queries the configured AddressLabelProvider for risk tags
// on every address surfaced by the request, decoded operations, and the
// simulator's fund-flow output. High-risk labels (sanctioned, exploited
// contracts, etc.) produce a fail result; the labels themselves are returned
// so the orchestrator can attach them to the simulation trace for downstream
// SimulationChecks to inspect.
//
// Provider is set at registration time. A nil provider degrades to a pass
// result (no labels queried).
type AddressLabelRisk struct {
	Provider security.AddressLabelProvider
}

func (AddressLabelRisk) Rule() string        { return "address_label_risk" }
func (AddressLabelRisk) AppliesTo() []string { return nil }
func (AddressLabelRisk) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Address Label Risk",
		Description: "Queries the configured AddressLabelProvider for risk tags on every address surfaced by the request, decoded calldata, and the simulator's fund-flow output. High-risk labels include sanctions, known exploiters, mixers, and scam contracts.",
		Statuses: map[string]string{
			"pass":    "No high-risk labels were returned for any address involved in the transaction.",
			"fail":    "At least one involved address carries a high-risk label.",
			"warn":    "Address label check could not complete due to provider error. User may bypass with Passkey.",
			"skipped": "Skipped by user. Address label check is disabled in Settings.",
		},
	}
}
func (a AddressLabelRisk) Run(c security.SimulationContext) (security.CheckResult, []security.AddressLabel) {
	if a.Provider == nil {
		return security.CheckResult{Rule: "address_label_risk", Status: "skipped", Detail: "Skipped by user — address label check is disabled in Settings."}, nil
	}
	addresses := security.CollectLabelAddresses(c.Req, c.Ops, c.FundFlow)
	slog.Info("security label check addresses", "chain_id", c.Req.ChainID, "owner", c.Req.Owner, "to", c.Req.To, "addresses", addresses)
	labels, err := a.Provider.Labels(c.Ctx, c.Req.ChainID, addresses)
	if err != nil {
		if errors.Is(err, security.ErrLabelCheckDisabled) {
			return security.CheckResult{Rule: "address_label_risk", Status: "skipped", Detail: "Skipped by user — address label check is disabled in Settings."}, nil
		}
		if errors.Is(err, security.ErrLabelPaymentFailed) {
			return security.CheckResult{Rule: "address_label_risk", Status: "warn", Detail: fmt.Sprintf("address label check skipped: %v. You can bypass this check or configure a different provider in Settings.", err)}, nil
		}
		return security.CheckResult{Rule: "address_label_risk", Status: "warn", Detail: fmt.Sprintf("address label lookup failed: %v. You can bypass this check or reconfigure the provider in Settings.", err)}, nil
	}
	findings := make([]map[string]interface{}, 0)
	for _, label := range labels {
		reasons := security.HighRiskLabelReasons(label)
		if len(reasons) == 0 {
			continue
		}
		findings = append(findings, map[string]interface{}{
			"address":      label.Address,
			"name_tag":     label.NameTag,
			"main_entity":  label.MainEntity,
			"comp_entity":  label.CompEntity,
			"risk_reasons": reasons,
		})
	}
	if len(findings) > 0 {
		return security.CheckResult{
			Rule:     "address_label_risk",
			Status:   "fail",
			Detail:   fmt.Sprintf("%d high-risk address label finding(s).", len(findings)),
			Observed: findings,
		}, labels
	}
	return security.CheckResult{
		Rule:     "address_label_risk",
		Status:   "pass",
		Detail:   fmt.Sprintf("checked %d address label(s); no high-risk labels found.", len(labels)),
		Observed: security.CompactLabels(labels),
	}, labels
}
