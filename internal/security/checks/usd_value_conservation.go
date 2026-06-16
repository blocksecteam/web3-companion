// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"fmt"
	"math/big"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// USDValueConservation compares the USD value of assets the owner spent
// against the USD value of assets they received in the simulated trace; a
// loss exceeding cfg.MaxUSDValueLossBPS fails the check.
//
// Scope: same-chain swaps only. Bridge transactions are cross-chain by
// construction (bridgePairs in internal/bridge/adapters/common.go skips
// from==to), so the destination-chain leg can never be present in the
// source-chain simulation — the conservation question is structurally
// unanswerable. Transfers and other one-way operations are not value-
// conserving by nature. AppliesTo() filters those out at the registry level.
type USDValueConservation struct{}

func (USDValueConservation) Rule() string        { return "usd_value_conservation" }
func (USDValueConservation) AppliesTo() []string { return []string{"swap"} }
func (USDValueConservation) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "USD Value Conservation",
		Description: "For same-chain swaps, compares the USD value of assets the owner spent against the USD value they received in the simulated trace. A loss exceeding cfg.MaxUSDValueLossBPS suggests slippage abuse, MEV sandwiching, or a misconfigured route.",
		Statuses: map[string]string{
			"pass": "USD outflow ≈ USD inflow within the configured loss cap, or no priced outflow was found for the owner.",
			"warn": "Some moved asset lacks USD price data so conservation cannot be fully verified.",
			"fail": "USD value loss exceeds the configured cap (cfg.MaxUSDValueLossBPS) — caller likely lost value to slippage or a hostile route.",
		},
	}
}
func (USDValueConservation) Run(c security.EffectContext) security.CheckResult {
	effect, cfg := *c.Effect, c.Cfg
	if effect.TotalSpendingUSD == nil || effect.TotalReceivingUSD == nil {
		return security.CheckResult{
			Rule:   "usd_value_conservation",
			Status: "warn",
			Detail: "some moved assets lack USD price data; cannot fully verify USD conservation.",
		}
	}
	outTotal, ok := primitives.DecimalRat(*effect.TotalSpendingUSD)
	if !ok {
		return security.CheckResult{Rule: "usd_value_conservation", Status: "warn", Detail: "total_spending_usd is not parseable."}
	}
	inTotal, ok := primitives.DecimalRat(*effect.TotalReceivingUSD)
	if !ok {
		return security.CheckResult{Rule: "usd_value_conservation", Status: "warn", Detail: "total_receiving_usd is not parseable."}
	}
	if outTotal.Sign() == 0 {
		return security.CheckResult{Rule: "usd_value_conservation", Status: "pass", Detail: "no priced outflow found for owner."}
	}
	loss := new(big.Rat).Sub(outTotal, inTotal)
	if loss.Sign() <= 0 {
		return security.CheckResult{Rule: "usd_value_conservation", Status: "pass", Detail: "owner receiving value is not below spending value."}
	}
	lossBPS := new(big.Rat).Mul(loss, big.NewRat(10000, 1))
	lossBPS.Quo(lossBPS, outTotal)
	limit := big.NewRat(cfg.MaxUSDValueLossBPS, 1)
	if lossBPS.Cmp(limit) > 0 {
		return security.CheckResult{
			Rule:   "usd_value_conservation",
			Status: "fail",
			Detail: fmt.Sprintf("USD value loss $%s exceeds cap %d bps.", primitives.FormatRat(loss), cfg.MaxUSDValueLossBPS),
			Observed: map[string]string{
				"outflow_usd": *effect.TotalSpendingUSD,
				"inflow_usd":  *effect.TotalReceivingUSD,
				"loss_usd":    primitives.FormatRat(loss),
				"loss_bps":    primitives.FormatRat(lossBPS),
			},
		}
	}
	return security.CheckResult{
		Rule:   "usd_value_conservation",
		Status: "pass",
		Detail: fmt.Sprintf("USD loss $%s is within cap %d bps.", primitives.FormatRat(loss), cfg.MaxUSDValueLossBPS),
	}
}
