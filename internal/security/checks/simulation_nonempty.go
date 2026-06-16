// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// SimulationNonEmpty fails when a transaction that should produce on-chain
// effects (non-zero value, non-trivial calldata) simulated to zero inner
// calls and zero balance changes — a strong signal that the simulator
// short-circuited or the transaction is a no-op.
type SimulationNonEmpty struct{}

func (SimulationNonEmpty) Rule() string        { return "simulation_non_empty" }
func (SimulationNonEmpty) AppliesTo() []string { return nil }
func (SimulationNonEmpty) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Simulation Non-Empty",
		Description: "When a transaction looks like it should produce on-chain effects (non-zero value or non-trivial calldata), guards against the simulator returning zero inner calls and zero balance changes — a strong signal of a short-circuit or no-op transaction.",
		Statuses: map[string]string{
			"pass": "Simulation produced inner calls or balance changes consistent with a value-moving transaction.",
			"fail": "Simulation data is unavailable, or a value-moving transaction produced an empty trace and no balance changes.",
		},
	}
}
func (SimulationNonEmpty) Run(c security.SimulationContext) security.CheckResult {
	req, sim := c.Req, c.Sim
	if sim == nil || sim.Data == nil || !sim.Data.Status {
		return security.CheckResult{Rule: "simulation_non_empty", Status: "fail", Detail: "successful simulation data is unavailable."}
	}
	value, _ := primitives.ParseInteger(req.Value)
	valueMoving := value.Sign() > 0 || hasActionCall(req.Data)
	traceCalls := 0
	if sim.Data.Trace != nil {
		traceCalls = len(sim.Data.Trace.Calls)
	}
	balanceChanges := len(sim.Data.BalanceChanges)
	if valueMoving && traceCalls == 0 && balanceChanges == 0 {
		return security.CheckResult{
			Rule:   "simulation_non_empty",
			Status: "fail",
			Detail: "simulation produced no inner calls and no balance changes for a value-moving transaction.",
		}
	}
	return security.CheckResult{
		Rule:   "simulation_non_empty",
		Status: "pass",
		Detail: fmt.Sprintf("simulation has %d inner call(s), %d balance-change account(s).", traceCalls, balanceChanges),
	}
}
