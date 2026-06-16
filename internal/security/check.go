// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import "context"

// Phase order: the orchestrator runs the six phases in fixed sequence,
// Shape → DataProducing → Calldata → LabelEmitting → Simulation → Effect.
// Each phase's Context type names the fields the orchestrator guarantees
// are populated by the time that phase's Run() is called.
//
// Phase transitions worth knowing about:
//   - DataProducing produces []Operation which the orchestrator stores in
//     CalldataContext.Ops before the Calldata phase runs.
//   - LabelEmitting emits []AddressLabel which the orchestrator attaches
//     to Sim.Data.Trace before regular SimulationChecks read the trace.
//   - Simulation → Effect is gated: the orchestrator only enters Effect
//     when Sim.Code == 0 and ExtractTxEffect produced a usable value.
//     EffectChecks that would have run are reported as skipped (see
//     StatusSkipped) so they still appear in the response.
//
// A check declares its phase implicitly via which interface it implements
// (ShapeCheck, DataProducingCheck, CalldataCheck, LabelEmittingCheck,
// SimulationCheck, EffectCheck). There is no Phase enum on the interface
// — the type system already encodes it.

// ShapeContext is what every check sees in the Shape phase: the raw request
// plus the security config. No decoded calldata yet, no simulation, no effect.
type ShapeContext struct {
	Ctx context.Context
	Req CheckRequest
	Cfg SecurityConfig
}

// CalldataContext extends ShapeContext with the decoded calldata (Ops). It is
// what calldata-phase checks see. Ops may be empty if the request had no
// executable batch, but it is not nil.
type CalldataContext struct {
	ShapeContext
	Ops []Operation
}

// SimulationContext extends CalldataContext with the simulator output.
// Implementations MUST guard against Sim == nil before reading any field: in
// production RPCSimulator returns non-nil on success, but test stubs and
// future simulators may return nil even when err is nil, and the
// orchestrator threads that nil through to here without unwrapping. When
// Sim is non-nil, Sim.Code may still be non-zero (simulation failure); guard
// against that before reading Sim.Data too.
//
// FundFlow is the secondary output of the simulator — the addresses involved
// in value movement during the simulated trace. LabelEmittingChecks feed it
// into label-provider lookups; orthodox SimulationChecks generally ignore it.
//
// Labels is gradually populated by LabelEmittingChecks before regular
// SimulationChecks run. After the label phase completes the orchestrator
// attaches Labels to Sim.Data.Trace so SimulationChecks reading the trace
// see them — Labels itself is mostly a record for the response payload, not
// a hot field most checks need to read directly.
type SimulationContext struct {
	CalldataContext
	Sim      *SimulationResult
	FundFlow []string
	Labels   []AddressLabel
}

// EffectContext extends SimulationContext with the extracted TxEffect. The
// orchestrator only enters this phase when Sim.Code == 0 and ExtractTxEffect
// produced a value, so Effect is guaranteed non-nil here.
type EffectContext struct {
	SimulationContext
	Effect *TxEffect
}

// ShapeCheck is implemented by rules that need only the raw request + config.
// They run first, before any decoding or simulation. Example: transaction
// shape validation.
type ShapeCheck interface {
	Rule() string
	AppliesTo() []string
	Description() RuleDescription
	Run(ShapeContext) CheckResult
}

// CalldataCheck is implemented by rules that inspect the decoded operations.
// They run after the DataProducing phase has filled CalldataContext.Ops.
// Example: unlimited approval detection.
type CalldataCheck interface {
	Rule() string
	AppliesTo() []string
	Description() RuleDescription
	Run(CalldataContext) CheckResult
}

// DataProducingCheck is a shape-phase check that also produces decoded
// operations for downstream calldata-phase checks to consume. It is the
// orthogonal cousin of LabelEmittingCheck — both run inside an established
// phase but emit side data the orchestrator threads into the next phase's
// context.
//
// Concretely: a DataProducingCheck reads ShapeContext (raw request + cfg),
// returns its own CheckResult, and produces a []Operation slice that the
// orchestrator stores in CalldataContext.Ops before invoking
// CalldataChecks. The canonical implementation is the calldata decoder
// (CalldataDecode in the checks subpackage), which parses ERC-7821 batches
// and direct calls into the executable Operation list.
//
// The interface is allowed but not encouraged for end-user-authored rules.
// Most authors should stick to one of the five "leaf" Check interfaces
// (Shape, Calldata, LabelEmitting, Simulation, Effect). Data-producing
// checks have outsized blast radius: if their Run() emits no ops, every
// CalldataCheck downstream operates on an empty slice. Register only one
// data-producing check unless you fully understand the consequences of
// multiple decoders; the orchestrator uses the first non-nil ops slice it
// sees.
type DataProducingCheck interface {
	Rule() string
	AppliesTo() []string
	Description() RuleDescription
	Run(ShapeContext) (CheckResult, []Operation)
}

// LabelEmittingCheck is a simulation-phase check that also produces labels.
//
// It runs after the simulator returns but before regular SimulationChecks,
// because the labels it emits are attached to Sim.Data.Trace by the
// orchestrator — SimulationChecks that read the decorated trace depend on
// that attach having happened.
//
// The check needs both decoded calldata (Ops) and simulator output
// (Sim, FundFlow) — see internal/security/address_labels.go's
// collectLabelAddresses for why both are required. SimulationContext exposes
// all three.
//
// This is intentionally a separate interface from SimulationCheck rather than
// an optional method on it: keeping each Run signature uniform makes the
// registry's dispatch straightforward (no runtime type assertions) and
// signals at the type level that this check writes back into context.
type LabelEmittingCheck interface {
	Rule() string
	AppliesTo() []string
	Description() RuleDescription
	Run(SimulationContext) (CheckResult, []AddressLabel)
}

// SimulationCheck is implemented by rules that need the simulator output.
// They run after the simulator returns, regardless of whether it succeeded;
// implementations must inspect Sim.Code before relying on Sim.Data.
type SimulationCheck interface {
	Rule() string
	AppliesTo() []string
	Description() RuleDescription
	Run(SimulationContext) CheckResult
}

// EffectCheck is implemented by rules that need the extracted TxEffect. They
// run only when Sim.Code == 0 and effect extraction succeeded — Effect is
// guaranteed non-nil inside Run.
type EffectCheck interface {
	Rule() string
	AppliesTo() []string
	Description() RuleDescription
	Run(EffectContext) CheckResult
}

// RuleDescription is the self-describing metadata a check exposes via its
// Description() method. The orchestrator reads it after Run() so the
// CheckResult shipped to the LLM agent and end users carries each rule's
// human-readable name + intent + per-status meaning — no need for the agent
// (or a hand-maintained companion/security.md table) to know every rule
// name in advance.
//
//	Name        Human-readable name in the user's language (e.g. "无限授权检测").
//	Description One sentence on what the rule looks at; what makes its
//	            judgement meaningful.
//	Statuses    Each status string this rule's Run() can emit ("pass" / "warn"
//	            / "fail") mapped to a human-readable explanation of what
//	            that status means *for this rule*. The Status of any
//	            CheckResult emitted by Run() should appear as a key here.
type RuleDescription struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Statuses    map[string]string `json:"statuses"`
}

// StatusSkipped is the status the orchestrator stamps onto a CheckResult
// when a registered rule did not run. Two reasons trigger this:
//
//   - The rule's AppliesTo declaration excluded the current request's
//     tx_type. Example: usd_value_conservation has AppliesTo=["swap"]; on
//     a transfer it is reported as skipped, not pass.
//   - The phase itself was gated out. Currently only Effect-phase rules
//     can be phase-gated: when Sim.Code != 0 the orchestrator never enters
//     the effect phase and every EffectCheck is reported as skipped.
//
// "skipped" is intentionally distinct from "pass" — a passing check
// actively approved the transaction; a skipped one was simply not
// consulted. Both contribute the benign verdict, but only "pass" is a
// positive judgement worth crediting.
const StatusSkipped = "skipped"

// AppliesToTxType reports whether a check's AppliesTo declaration matches the
// current request's tx_type. Semantics:
//
//	nil           — applies to every tx_type (use this for foundational rules
//	                that should never be filtered out).
//	non-empty []  — whitelist; the check runs only for the listed tx_types.
//	empty []      — explicitly disabled (useful to silence a check temporarily
//	                without deleting its registration).
func AppliesToTxType(applies []string, txType string) bool {
	if applies == nil {
		return true
	}
	for _, t := range applies {
		if t == txType {
			return true
		}
	}
	return false
}
