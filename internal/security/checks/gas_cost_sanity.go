// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// GasCostSanity computes the simulated transaction fee in USD using the
// owner's chain-native USD price (from sim.Data) and fails when it exceeds
// cfg.MaxGasUSD. It distinguishes live RPC-sourced gas prices from offline
// fallback estimates and downgrades pass→warn for the latter, so a verdict
// is never silently issued on an unverified fee estimate.
type GasCostSanity struct{}

func (GasCostSanity) Rule() string        { return "gas_cost_sanity" }
func (GasCostSanity) AppliesTo() []string { return nil }
func (GasCostSanity) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Gas Cost Sanity",
		Description: "Converts the simulated transaction fee to USD using the owner's chain-native USD price and fails when it exceeds cfg.MaxGasUSD. Catches fee-spike honeypots and accidental overpayment. Distinguishes live RPC-sourced gas prices from offline fallback estimates — the latter is surfaced as warn even when the converted fee is within the cap, so a verdict is never silently issued on an unverified estimate.",
		Statuses: map[string]string{
			"pass": "Gas cost is within the configured USD cap, evaluated from a live RPC-sourced gas price.",
			"warn": "Gas cost could not be reliably evaluated — gas price was unavailable or came from an offline fallback, the native USD price was missing, or the simulator returned unexpected data. See detail for the specific cause.",
			"fail": "Simulation data is missing, or the converted gas cost exceeds cfg.MaxGasUSD.",
		},
	}
}
func (GasCostSanity) Run(c security.SimulationContext) security.CheckResult {
	req, sim, cfg := c.Req, c.Sim, c.Cfg
	if sim == nil || sim.Data == nil {
		return security.CheckResult{Rule: "gas_cost_sanity", Status: "fail", Detail: "simulation data unavailable."}
	}
	// Failed simulation: sim.Data may carry a partial pre-revert prefix
	// whose GasPrice/TransactionFee fields reflect an incomplete run.
	// Continuing here produces a misleading "zero fee" warn that double-
	// narrates the same root cause as SimulationSuccess's fail row.
	// Mirror the skip pattern bridge_deposit_match and transfer_intent_match
	// already use.
	if c.Sim.Code != 0 {
		return security.CheckResult{
			Rule:          "gas_cost_sanity",
			Status:        security.StatusSkipped,
			StatusMeaning: "Rule was not consulted for this transaction.",
			Detail:        "simulation did not succeed; gas cost cannot be evaluated against the cap.",
		}
	}
	// Build-phase requests do not carry a gas price — fee is set at sign time
	// via FetchFeeParams. Without a gas price the simulator computes
	// TransactionFee as 0, which would otherwise silently bypass the
	// MaxGasUSD cap. Surface explicitly so the LLM does not narrate this
	// as "no gas spent". Read sim.Data.GasPrice (what the simulator
	// actually used to compute the fee) rather than req.GasPrice — both
	// are equivalent in production but only sim.Data is authoritative.
	if gp := strings.TrimSpace(sim.Data.GasPrice); gp == "" || gp == "0" {
		return security.CheckResult{
			Rule:   "gas_cost_sanity",
			Status: "warn",
			Detail: "gas price not provided at this phase; on-chain fee will be set when the transaction is signed and is not evaluated here.",
		}
	}
	feeWei, ok := primitives.ParseInteger(sim.Data.TransactionFee)
	if !ok {
		return security.CheckResult{Rule: "gas_cost_sanity", Status: "warn", Detail: "transactionFee is not parseable."}
	}
	if feeWei.Sign() == 0 {
		// Defensive: gas price was provided above but the simulator still
		// produced zero fee (e.g. trace returned gasUsed=0 after a failure).
		// Do not claim "pass" — we have no fee to evaluate against the cap.
		return security.CheckResult{Rule: "gas_cost_sanity", Status: "warn", Detail: "simulator returned zero fee despite a non-zero gas price; cannot evaluate against gas cap."}
	}
	price := security.NativeUSDPerWei(sim.Data, req.Owner)
	if price == nil {
		return security.CheckResult{
			Rule:   "gas_cost_sanity",
			Status: "warn",
			Detail: fmt.Sprintf("fee=%s wei but native USD price is unavailable.", feeWei.String()),
		}
	}
	feeUSD := new(big.Rat).Mul(new(big.Rat).SetInt(feeWei), price)
	limit := new(big.Rat).SetFloat64(cfg.MaxGasUSD)
	if feeUSD.Cmp(limit) > 0 {
		return security.CheckResult{
			Rule:   "gas_cost_sanity",
			Status: "fail",
			Detail: fmt.Sprintf("gas cost $%s exceeds max gas cap $%.2f.", primitives.FormatRat(feeUSD), cfg.MaxGasUSD),
			Observed: map[string]string{
				"gas_usd": primitives.FormatRat(feeUSD),
			},
		}
	}
	// Fee is within cap, but the price feeding the simulator came from an
	// offline fallback (RPC unreachable at build time). Downgrade to warn
	// so the LLM/UI surfaces the estimate as unverified — sign-time RPC
	// might still succeed and pay a different actual fee.
	if req.GasPriceSource == "fallback" {
		return security.CheckResult{
			Rule:   "gas_cost_sanity",
			Status: "warn",
			Detail: fmt.Sprintf("gas cost $%s estimated using offline fallback price (live RPC unreachable); within cap $%.2f but actual on-chain fee may differ.", primitives.FormatRat(feeUSD), cfg.MaxGasUSD),
		}
	}
	return security.CheckResult{
		Rule:   "gas_cost_sanity",
		Status: "pass",
		Detail: fmt.Sprintf("gas cost $%s is within cap $%.2f.", primitives.FormatRat(feeUSD), cfg.MaxGasUSD),
	}
}
