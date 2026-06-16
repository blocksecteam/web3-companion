// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"fmt"
)

// Simulator runs a transaction's calldata against a forked RPC and reports
// the resulting trace, balance changes, and fund-flow address set. The
// orchestrator (checkTransaction) feeds the result into the Simulation /
// Effect phases of the check pipeline.
//
// Returning a non-nil SimulationResult with a non-zero Code is allowed: it
// signals "simulator reached the RPC but the EVM reverted / ran out of gas
// / etc.". Returning an error means the simulator itself failed (network
// error, malformed response). SimulationCheck implementations must inspect
// Sim.Code before reading Sim.Data; see internal/security/check.go.
type Simulator interface {
	Simulate(ctx context.Context, req CheckRequest) (*SimulationResult, []string, error)
}

// unavailableSimulator is the default Simulator wired in by NewHandler when
// the caller does not inject one. Every call returns an error, so the HTTP
// /security/check endpoint surfaces a 503 service-unavailable rather than
// silently producing a misleading "pass" verdict on uninspected calldata.
type unavailableSimulator struct{}

func (unavailableSimulator) Simulate(ctx context.Context, req CheckRequest) (*SimulationResult, []string, error) {
	return nil, nil, fmt.Errorf("transaction simulator is not configured")
}
