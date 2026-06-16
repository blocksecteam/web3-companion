// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// checkTransaction is the security orchestrator: it runs the simulator,
// then walks the six check phases (Shape → DataProducing → Calldata →
// LabelEmitting → Simulation → Effect) in fixed order, collecting one
// CheckResult per registered rule (decorated with the rule's static
// description metadata or marked skipped when AppliesTo / phase gates
// excluded it), and finally aggregates the verdict.
//
// Lives on *Handler so HTTP routes can call it as h.checkTransaction(...),
// but the implementation has no HTTP awareness — it could be lifted into a
// standalone Orchestrator struct in a later refactor if it ever grows
// callers outside the HTTP layer.
func (h *Handler) checkTransaction(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	sim, fundFlow, err := h.simulator.Simulate(ctx, req)
	if err != nil {
		return nil, err
	}

	cc := ShapeContext{Ctx: ctx, Req: req, Cfg: h.cfg}
	results := make([]CheckResult, 0, 16)

	// Shape phase
	for _, check := range h.checks.ShapeChecks() {
		if !AppliesToTxType(check.AppliesTo(), req.TxType) {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, ""))
			continue
		}
		results = append(results, decorateWithDescription(check.Run(cc), check.Description()))
	}

	// Data-producing phase: decoders run on ShapeContext, emit []Operation
	// for downstream phases AND a CheckResult on the `calldata_decode` rule.
	// If multiple are registered, the first non-nil ops slice wins; their
	// CheckResults are all appended so each appears in the All Checks
	// table. In the default registry only CalldataDecode is registered.
	var ops []Operation
	for _, check := range h.checks.DataProducingChecks() {
		if !AppliesToTxType(check.AppliesTo(), req.TxType) {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, ""))
			continue
		}
		res, produced := check.Run(cc)
		results = append(results, decorateWithDescription(res, check.Description()))
		if ops == nil && produced != nil {
			ops = produced
		}
	}

	calldataCtx := CalldataContext{ShapeContext: cc, Ops: ops}
	simCtx := SimulationContext{
		CalldataContext: calldataCtx,
		Sim:             sim,
		FundFlow:        fundFlow,
	}

	for _, check := range h.checks.CalldataChecks() {
		if !AppliesToTxType(check.AppliesTo(), req.TxType) {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, ""))
			continue
		}
		results = append(results, decorateWithDescription(check.Run(calldataCtx), check.Description()))
	}

	// LabelEmittingChecks run after simulate (so they see FundFlow + Sim) but
	// before regular SimulationChecks (so the labels they emit can be attached
	// to the trace before SimulationChecks read it).
	for _, check := range h.checks.LabelEmittingChecks() {
		if !AppliesToTxType(check.AppliesTo(), req.TxType) {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, ""))
			continue
		}
		res, ls := check.Run(simCtx)
		simCtx.Labels = append(simCtx.Labels, ls...)
		results = append(results, decorateWithDescription(res, check.Description()))
	}
	if sim != nil && sim.Data != nil {
		attachAddressLabelsToTrace(sim.Data.Trace, simCtx.Labels)
	}

	// Simulation phase
	for _, check := range h.checks.SimulationChecks() {
		if !AppliesToTxType(check.AppliesTo(), req.TxType) {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, ""))
			continue
		}
		results = append(results, decorateWithDescription(check.Run(simCtx), check.Description()))
	}

	// Effect phase — gated by sim.Code == 0 && sim.Data != nil
	var effect *TxEffect
	effectPhaseGated := !(sim != nil && sim.Code == 0 && sim.Data != nil)
	var effectCtx EffectContext
	if !effectPhaseGated {
		e := ExtractTxEffect(req, sim.Data)
		effect = &e
		effectCtx = EffectContext{SimulationContext: simCtx, Effect: effect}
	}
	for _, check := range h.checks.EffectChecks() {
		if !AppliesToTxType(check.AppliesTo(), req.TxType) {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, ""))
			continue
		}
		if effectPhaseGated {
			results = append(results, skippedResult(check.Rule(), check.Description(), req.TxType, "Effect phase did not run because the simulation did not succeed."))
			continue
		}
		results = append(results, decorateWithDescription(check.Run(effectCtx), check.Description()))
	}

	verdict := aggregateVerdict(results)
	resp := &CheckResponse{
		TxType:     req.TxType,
		Verdict:    verdict,
		Summary:    summarizeVerdict(verdict, results),
		Checks:     results,
		Effect:     effect,
		Simulation: h.simplifiedSimulationPayload(ctx, sim),
	}
	if req.SummaryJSON != "" {
		resp.TxSummary = json.RawMessage(req.SummaryJSON)
	}
	return resp, nil
}

// decorateWithDescription copies the check's static metadata (Name,
// Description) into the dynamic CheckResult and resolves the per-status
// meaning. The orchestrator calls this for every result produced by a
// registered Check so the agent and end user see a self-describing rule
// without consulting a separate doc.
func decorateWithDescription(res CheckResult, desc RuleDescription) CheckResult {
	res.Name = desc.Name
	res.Description = desc.Description
	if desc.Statuses != nil {
		res.StatusMeaning = desc.Statuses[res.Status]
	}
	return res
}

// skippedResult builds a CheckResult representing a rule that the orchestrator
// chose not to run. The reason is either an AppliesTo filter miss (default
// when reason is empty — explains tx_type incompatibility) or an explicit
// phase gate (caller passes the reason, e.g. "effect phase did not run...").
// Skipped results carry the rule's Name/Description so the All Checks table
// still shows what the rule would have done, but their Status is "skipped"
// to make clear they did not actively approve anything.
func skippedResult(rule string, desc RuleDescription, txType, reason string) CheckResult {
	detail := reason
	if detail == "" {
		detail = fmt.Sprintf("Rule does not apply to tx_type=%q.", txType)
	}
	return CheckResult{
		Rule:          rule,
		Name:          desc.Name,
		Description:   desc.Description,
		Status:        StatusSkipped,
		StatusMeaning: "Rule was not consulted for this transaction.",
		Detail:        detail,
	}
}

// aggregateVerdict reduces the per-rule statuses into a single response-level
// verdict. fail wins absolutely; otherwise warn wins over pass; skipped does
// not count. The aggregator does not look at rule severity or rule identity —
// individual rules express severity through their Status, not through a separate
// weight.
func aggregateVerdict(checks []CheckResult) string {
	verdict := "pass"
	for _, check := range checks {
		switch check.Status {
		case "fail":
			return "fail"
		case "warn":
			verdict = "warn"
		}
	}
	return verdict
}

// summarizeVerdict produces the short human-readable line that appears at the
// top of the security response — e.g. "verdict=warn; warn=[gas_cost_sanity]"
// — by collecting fail / warn rule ids and joining them. Skipped and pass
// rules are intentionally omitted: the line is meant to draw attention to
// what needs review, not enumerate everything that ran.
func summarizeVerdict(verdict string, checks []CheckResult) string {
	fails := make([]string, 0)
	warns := make([]string, 0)
	for _, check := range checks {
		switch check.Status {
		case "fail":
			fails = append(fails, check.Rule)
		case "warn":
			warns = append(warns, check.Rule)
		}
	}
	summary := "verdict=" + verdict
	if len(fails) > 0 {
		summary += "; fail=[" + joinRules(fails) + "]"
	}
	if len(warns) > 0 {
		summary += "; warn=[" + joinRules(warns) + "]"
	}
	return summary
}

func joinRules(rules []string) string {
	if len(rules) == 0 {
		return ""
	}
	out := rules[0]
	for _, rule := range rules[1:] {
		out += ", " + rule
	}
	return out
}

// securityAgentMustReview decides whether a verdict requires explicit
// Security Agent review before the transaction can proceed to signing.
// Driven by SecurityAgentReviewMode in SecurityConfig:
//
//	"always" (default) — every verdict requires agent review
//	"warn"             — only warn / fail verdicts require review
//	anything else      — treated as "always" for safety
//
// Lives on *Handler because it reads h.cfg, but the policy itself is
// orchestrator-level (verdict interpretation), not HTTP concern.
func (h *Handler) securityAgentMustReview(verdict string) bool {
	mode := strings.ToLower(strings.TrimSpace(h.cfg.SecurityAgentReviewMode))
	if mode == "" || mode == "always" {
		return true
	}
	if mode == "warn" {
		switch strings.ToLower(strings.TrimSpace(verdict)) {
		case "warn", "fail":
			return true
		default:
			return false
		}
	}
	return true
}
