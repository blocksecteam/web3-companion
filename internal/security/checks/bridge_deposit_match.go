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

// BridgeDepositMatch verifies that the simulated trace actually delivers
// funds to the per-quote deposit address the bridge adapter committed to in
// tx_summary.deposit_address. Catches deposit-redirection attacks: a
// tampered calldata or compromised adapter could ship the user's funds to
// an attacker-controlled address while keeping the rest of the trace
// looking superficially normal.
//
// What "delivered" means here, in priority order:
//
//  1. Any call inside the trace whose To equals deposit_address and whose
//     Value is non-zero — i.e. native asset moved into deposit_address.
//     Strongest signal; works for ETH/BNB/POL deposit-mode bridges.
//
//  2. deposit_address present in SimulationContext.FundFlow — the
//     simulator's aggregated set of addresses that exchanged value with
//     anyone in the trace (covers ERC-20 transfers via Transfer event
//     logs without needing to parse calldata args). Weaker than (1) on
//     its own but sufficient given that a deposit_address only ever
//     surfaces because the bridge adapter generated it for this quote.
//
// The check applies only to bridge transactions whose summary actually
// carries a deposit_address — approve-mode bridges (summary.deposit_address
// absent, summary.approve_target present) are out of scope and pass
// silently.
type BridgeDepositMatch struct{}

func (BridgeDepositMatch) Rule() string        { return "bridge_deposit_match" }
func (BridgeDepositMatch) AppliesTo() []string { return []string{"bridge"} }
func (BridgeDepositMatch) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Bridge Deposit Match",
		Description: "Verifies that the simulated trace transfers funds to the per-quote deposit_address the bridge adapter committed to in tx_summary, catching deposit-redirection where a tampered calldata or compromised adapter would silently change the recipient.",
		Statuses: map[string]string{
			"pass": "Simulated trace delivers value to the declared deposit_address, or the bridge is not in deposit mode (summary parsed cleanly and no deposit_address declared).",
			"warn": "Cannot evaluate the deposit target: tx_summary is missing or unparseable, simulation data is unavailable, simulation did not succeed, or deposit_address resolves to the native sentinel.",
			"fail": "tx_summary.deposit_address is present but no observable transfer to that address appears in the simulated trace — the bridge build's declared deposit target is not where funds actually went.",
		},
	}
}
func (BridgeDepositMatch) Run(c security.SimulationContext) security.CheckResult {
	rawAddr, present, parseable := extractDepositAddress(c.Req.SummaryJSON)
	if !parseable {
		// summary missing or JSON unparseable — distinct from "approve-mode
		// bridge with no deposit_address". This rule exists to catch
		// deposit-redirection; silently passing when the summary cannot be
		// read would mask field-drop, serialization loss, or tampering, all
		// of which look identical to "no field" under a string-empty check.
		return security.CheckResult{
			Rule:   "bridge_deposit_match",
			Status: "warn",
			Detail: "tx_summary missing or unparseable; cannot verify deposit target.",
		}
	}
	if !present {
		// Legitimate approve-mode bridge: summary parsed cleanly, deposit_address
		// simply absent because this bridge uses approve+pull rather than
		// deposit-to-address.
		return security.CheckResult{
			Rule:   "bridge_deposit_match",
			Status: "pass",
			Detail: "no deposit_address declared (approve-mode bridge); rule does not apply to this transaction.",
		}
	}
	depositAddr := normaliseTokenAddress(rawAddr)
	if depositAddr == "native" {
		// Pathological: summary's deposit_address normalises to the native
		// sentinel. There's no real on-chain recipient to match against;
		// treat as warn rather than silently pass.
		return security.CheckResult{
			Rule:   "bridge_deposit_match",
			Status: "warn",
			Detail: "tx_summary.deposit_address normalises to the native sentinel; no concrete on-chain target to match against.",
		}
	}
	if c.Sim == nil || c.Sim.Data == nil {
		return security.CheckResult{
			Rule:   "bridge_deposit_match",
			Status: "warn",
			Detail: "simulation data unavailable; cannot verify whether trace delivers to declared deposit_address.",
		}
	}
	// Failed simulation: the trace may exist but is a half-recorded
	// call chain from before the revert. Running the deposit-target
	// match against it produces meaningless fails (revert often
	// short-circuited before the deposit call). SimulationSuccess
	// already reports the failure on its own rule; short-circuit to
	// skipped so the same root cause is not double-counted.
	if c.Sim.Code != 0 {
		return security.CheckResult{
			Rule:          "bridge_deposit_match",
			Status:        security.StatusSkipped,
			StatusMeaning: "Rule was not consulted for this transaction.",
			Detail:        "simulation did not succeed; trace is incomplete so deposit-target match cannot be evaluated.",
		}
	}

	// Signal 1: native-value transfer to deposit_address anywhere in the
	// trace. Strongest evidence; sufficient on its own.
	if traceCallDeliversNativeTo(c.Sim.Data.Trace, depositAddr) {
		return security.CheckResult{
			Rule:   "bridge_deposit_match",
			Status: "pass",
			Detail: fmt.Sprintf("simulated trace delivers native value to declared deposit_address %s.", depositAddr),
		}
	}

	// Signal 2: deposit_address appears in the simulator's aggregated
	// fund-flow set (covers ERC-20 transfers via Transfer event logs).
	for _, addr := range c.FundFlow {
		if primitives.AddrEq(addr, depositAddr) {
			return security.CheckResult{
				Rule:   "bridge_deposit_match",
				Status: "pass",
				Detail: fmt.Sprintf("simulated trace's fund-flow set includes declared deposit_address %s (ERC-20 Transfer-event match).", depositAddr),
			}
		}
	}

	return security.CheckResult{
		Rule:   "bridge_deposit_match",
		Status: "fail",
		Detail: fmt.Sprintf("declared deposit_address %s does not appear in the simulated trace's value movement — bridge calldata is shipping funds somewhere other than the declared deposit target.", depositAddr),
		Observed: map[string]string{
			"expected_deposit_address": depositAddr,
			"fund_flow_count":          fmt.Sprintf("%d", len(c.FundFlow)),
		},
	}
}

// extractDepositAddress pulls summary.deposit_address out of req.SummaryJSON,
// returning three flags so the caller can distinguish:
//
//	parseable=false           — summary absent or JSON unparseable; the rule
//	                            should warn rather than silently pass, since
//	                            a missing/corrupt summary is the exact
//	                            condition deposit-redirection attacks exploit.
//	parseable=true, present=false — JSON parsed, deposit_address absent
//	                            (legitimate approve-mode bridge); pass.
//	parseable=true, present=true  — concrete address returned; run the
//	                            trace-match logic.
//
// Returning a bare string would conflate cases 1 and 2 — corrupted JSON
// would silently look like "approve-mode bridge".
func extractDepositAddress(rawJSON string) (address string, present bool, parseable bool) {
	if rawJSON == "" {
		return "", false, false
	}
	var summary struct {
		DepositAddress string `json:"deposit_address"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &summary); err != nil {
		return "", false, false
	}
	trimmed := strings.TrimSpace(summary.DepositAddress)
	return trimmed, trimmed != "", true
}

// traceCallDeliversNativeTo reports whether anywhere in the call tree there
// is a call that targets `target` with non-zero native value. Recursive on
// SimulationCall.Calls.
func traceCallDeliversNativeTo(call *security.SimulationCall, target string) bool {
	if call == nil {
		return false
	}
	if primitives.AddrEq(call.To, target) {
		if v, ok := primitives.ParseInteger(call.Value); ok && v.Sign() > 0 {
			return true
		}
	}
	for i := range call.Calls {
		if traceCallDeliversNativeTo(&call.Calls[i], target) {
			return true
		}
	}
	return false
}
