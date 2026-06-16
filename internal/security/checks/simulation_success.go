// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// SimulationSuccess fails the check when the simulator returned a non-zero
// status code, returned code=0 but no SimulationData, or returned data with
// Status=false. It is the gate-keeper for any downstream check that wants to
// inspect Sim.Data — those checks rely on this one having declared the sim
// usable.
type SimulationSuccess struct{}

func (SimulationSuccess) Rule() string        { return "simulation_success" }
func (SimulationSuccess) AppliesTo() []string { return nil }
func (SimulationSuccess) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Simulation Success",
		Description: "Gates downstream simulation-phase checks: confirms the simulator returned code=0 with usable transaction data and a non-failing EVM status. Any subsequent check that reads Sim.Data assumes this one passed.",
		Statuses: map[string]string{
			"pass": "Simulation completed successfully and produced usable transaction data.",
			"fail": "Simulation returned a non-zero code, missing data, or a reverted EVM execution.",
		},
	}
}
func (SimulationSuccess) Run(c security.SimulationContext) security.CheckResult {
	sim := c.Sim
	if sim == nil {
		return security.CheckResult{Rule: "simulation_success", Status: "fail", Detail: "simulation result is missing."}
	}
	if sim.Code != 0 {
		return security.CheckResult{
			Rule:     "simulation_success",
			Status:   "fail",
			Detail:   primitives.FirstString(sim.Message, fmt.Sprintf("simulation error %d", sim.Code)),
			Observed: map[string]interface{}{"code": sim.Code},
		}
	}
	if sim.Data == nil {
		return security.CheckResult{Rule: "simulation_success", Status: "fail", Detail: "simulator returned code=0 without transaction data."}
	}
	if !sim.Data.Status {
		return security.CheckResult{
			Rule:   "simulation_success",
			Status: "fail",
			Detail: primitives.FirstString(sim.Data.ErrorInfo, "simulated EVM execution failed."),
		}
	}
	return security.CheckResult{Rule: "simulation_success", Status: "pass", Detail: "simulation completed successfully."}
}
