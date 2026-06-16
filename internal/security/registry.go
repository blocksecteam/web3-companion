// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import "sync"

// CheckRegistry holds the static security check rules grouped by phase.
//
// The registry is split into one slot per phase (rather than a single []Check
// slice with a Phase() method) so the orchestrator can dispatch by direct
// slice iteration without runtime type assertions. The cost is six
// Register*() entry points (one per interface in check.go); the win is that
// "which fields can this Run read" is enforced at the type system level via
// the Context types in check.go.
//
// All Register*/getter operations are mutex-guarded for the rare case where
// tests register checks from multiple goroutines; the hot path
// (checkTransaction) only reads.
type CheckRegistry struct {
	mu sync.RWMutex

	shape         []ShapeCheck
	dataProducing []DataProducingCheck
	calldata      []CalldataCheck
	labelEmitting []LabelEmittingCheck
	simulation    []SimulationCheck
	effect        []EffectCheck
}

// NewCheckRegistry returns an empty registry. Callers populate it via
// Register* methods or by handing it to a package-level RegisterAll helper
// (the security/checks subpackage exposes one).
func NewCheckRegistry() *CheckRegistry {
	return &CheckRegistry{}
}

// RegisterShape appends a ShapeCheck. Duplicate rules are not detected here;
// the orchestrator runs whatever the registry holds in registration order.
func (r *CheckRegistry) RegisterShape(c ShapeCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shape = append(r.shape, c)
}

func (r *CheckRegistry) RegisterDataProducing(c DataProducingCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dataProducing = append(r.dataProducing, c)
}

func (r *CheckRegistry) RegisterCalldata(c CalldataCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calldata = append(r.calldata, c)
}

func (r *CheckRegistry) RegisterLabelEmitting(c LabelEmittingCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.labelEmitting = append(r.labelEmitting, c)
}

func (r *CheckRegistry) RegisterSimulation(c SimulationCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.simulation = append(r.simulation, c)
}

func (r *CheckRegistry) RegisterEffect(c EffectCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effect = append(r.effect, c)
}

// ShapeChecks returns every registered shape-phase check (no AppliesTo
// filtering). The returned slice is a fresh copy; callers may iterate
// without holding the registry lock.
//
// The orchestrator applies AppliesTo filtering itself and emits a "skipped"
// CheckResult for rules that do not match the current tx_type, so the
// response surface lists every registered rule and the user can see why
// it did not contribute to the verdict.
func (r *CheckRegistry) ShapeChecks() []ShapeCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]ShapeCheck(nil), r.shape...)
}

func (r *CheckRegistry) DataProducingChecks() []DataProducingCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]DataProducingCheck(nil), r.dataProducing...)
}

func (r *CheckRegistry) CalldataChecks() []CalldataCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]CalldataCheck(nil), r.calldata...)
}

func (r *CheckRegistry) LabelEmittingChecks() []LabelEmittingCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]LabelEmittingCheck(nil), r.labelEmitting...)
}

func (r *CheckRegistry) SimulationChecks() []SimulationCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]SimulationCheck(nil), r.simulation...)
}

func (r *CheckRegistry) EffectChecks() []EffectCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]EffectCheck(nil), r.effect...)
}

// Rules returns every registered rule name across all phases, in registration
// order within each phase, phase-order across phases. Intended for the
// registry-completeness test ("all expected checks registered") and for
// debug dumps.
func (r *CheckRegistry) Rules() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.shape)+len(r.dataProducing)+len(r.calldata)+len(r.labelEmitting)+len(r.simulation)+len(r.effect))
	for _, c := range r.shape {
		out = append(out, c.Rule())
	}
	for _, c := range r.dataProducing {
		out = append(out, c.Rule())
	}
	for _, c := range r.calldata {
		out = append(out, c.Rule())
	}
	for _, c := range r.labelEmitting {
		out = append(out, c.Rule())
	}
	for _, c := range r.simulation {
		out = append(out, c.Rule())
	}
	for _, c := range r.effect {
		out = append(out, c.Rule())
	}
	return out
}
