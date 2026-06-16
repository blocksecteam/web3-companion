// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security"
	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

// TransferIntentMatch verifies that a transfer transaction does exactly what
// its tx_summary declared: the right token, the right amount, the right
// recipient, and nothing else (no approves, no delegatecalls). Deterministic
// encoding of these cross-validation checks keeps them stable across LLM
// prompt drift.
//
// Four sub-rules, all gated on tx_summary being present:
//
//	recipient_match     summary.to must appear as a transfer recipient in
//	                    the simulated trace (native value call to that
//	                    address, or ERC-20 Transfer event covered by
//	                    SimulationContext.FundFlow).
//	token_amount_match  the simulated effect must show summary.token
//	                    moving in summary.amount magnitude. Native and
//	                    ERC-20 use different ground-truth sources:
//	                    native compares trace call.Value against the
//	                    declared wei amount (BalanceChange for native
//	                    comes from a prestate diff and is gas-polluted,
//	                    so it cannot match an amount that excludes gas);
//	                    ERC-20 compares the owner's BalanceChange asset
//	                    outflow against the declared amount (ERC-20
//	                    BalanceChanges come from Transfer event logs and
//	                    gas does not pollute them).
//	calldata_extras     decoded operations must contain neither approve
//	                    (0x095ea7b3) nor setApprovalForAll (0xa22cb465);
//	                    a transfer should be a single transfer call.
//	trace_delegatecall  no DELEGATECALL to an UNTRUSTED target in the
//	                    simulated trace. A 7702-delegated wallet always
//	                    executes through a designator DELEGATECALL into its
//	                    own installed delegate (trustedDelegateImplementation),
//	                    so that frame is expected and ignored; a DELEGATECALL
//	                    to any other address hands execution to an unknown
//	                    contract and is a strong red flag.
//
// All four sub-rule failures are fail-level (this is high-stakes — the user
// is moving real value and any mismatch likely means tampered intent or a
// compromised build). Summary missing / unparseable → warn so the rule
// degrades gracefully rather than blocking legitimate traffic without
// evidence.
type TransferIntentMatch struct{}

func (TransferIntentMatch) Rule() string        { return "transfer_intent_match" }
func (TransferIntentMatch) AppliesTo() []string { return []string{"transfer"} }
func (TransferIntentMatch) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Transfer Intent Match",
		Description: "Verifies that a transfer transaction does exactly what tx_summary declared: the declared recipient, token, and amount all show up in the simulated trace, and the calldata/trace contain no unexpected approves or delegatecalls.",
		Statuses: map[string]string{
			"pass": "Simulated trace shows exactly the declared transfer (recipient + token + amount match) and nothing else.",
			"warn": "tx_summary is missing or unparseable, or simulation data is unavailable; cannot verify the transfer matches intent.",
			"fail": "One or more sub-rules failed: recipient mismatch, token/amount mismatch, unexpected approve in calldata, or a DELEGATECALL to an untrusted (non-delegate) target in the trace.",
		},
	}
}
func (TransferIntentMatch) Run(c security.SimulationContext) security.CheckResult {
	declared, ok := extractTransferDeclaration(c.Req.SummaryJSON)
	if !ok {
		return security.CheckResult{
			Rule:   "transfer_intent_match",
			Status: "warn",
			Detail: "tx_summary missing or unparseable; cannot verify the transfer matches intent.",
		}
	}
	if c.Sim == nil || c.Sim.Data == nil {
		return security.CheckResult{
			Rule:   "transfer_intent_match",
			Status: "warn",
			Detail: "simulation data unavailable; cannot verify trace matches declared transfer.",
		}
	}
	// Failed simulation: the trace and BalanceChanges only reflect
	// the pre-revert prefix, so the recipient/amount/extras checks
	// would mis-report a legitimate transfer as mismatched.
	// SimulationSuccess already surfaces the failure; short-circuit
	// to skipped to avoid double-counting the same root cause.
	if c.Sim.Code != 0 {
		return security.CheckResult{
			Rule:          "transfer_intent_match",
			Status:        security.StatusSkipped,
			StatusMeaning: "Rule was not consulted for this transaction.",
			Detail:        "simulation did not succeed; trace is incomplete so transfer-intent match cannot be evaluated.",
		}
	}

	findings := make([]map[string]string, 0)

	if declared.to != "" && !recipientInSimulation(c, declared.to) {
		findings = append(findings, map[string]string{
			"check":       "recipient_mismatch",
			"expected_to": declared.to,
			"detail":      fmt.Sprintf("declared recipient %s does not appear in the simulated trace's value movement.", declared.to),
		})
	}
	if amountFinding := verifyTransferAmount(c, declared); amountFinding != nil {
		findings = append(findings, amountFinding)
	}
	if extra := extraCalldataOps(c.Ops); extra != "" {
		findings = append(findings, map[string]string{
			"check":  "calldata_extras",
			"detail": fmt.Sprintf("calldata contains unexpected operation(s): %s. A transfer should be a single transfer call.", extra),
		})
	}
	if target, found := traceUnexpectedDelegateCall(c.Sim.Data.Trace, c.Req.Owner); found {
		findings = append(findings, map[string]string{
			"check":  "trace_delegatecall",
			"target": target,
			"detail": fmt.Sprintf("simulated trace contains a DELEGATECALL to %s, which is not the wallet's trusted EIP-7702 delegate — execution context was handed to an unknown contract.", target),
		})
	}

	if len(findings) == 0 {
		return security.CheckResult{
			Rule:   "transfer_intent_match",
			Status: "pass",
			Detail: fmt.Sprintf("simulated trace matches declared transfer: %s %s → %s.", declared.amount, displayToken(declared.token), declared.to),
		}
	}
	return security.CheckResult{
		Rule:     "transfer_intent_match",
		Status:   "fail",
		Detail:   fmt.Sprintf("%d transfer intent finding(s); calldata/trace does not match tx_summary.", len(findings)),
		Observed: findings,
	}
}

type transferDeclaration struct {
	token  string // normalised: "native" or lowercase hex address
	amount string // human-readable, e.g. "0.5"
	to     string // lowercase hex address
}

// extractTransferDeclaration pulls token / amount / to out of req.SummaryJSON.
// Returns ok=false when summary is absent or unparseable; the caller maps
// that to a warn (rather than a false fail).
func extractTransferDeclaration(rawJSON string) (transferDeclaration, bool) {
	if rawJSON == "" {
		return transferDeclaration{}, false
	}
	var summary struct {
		Token  string `json:"token"`
		Amount string `json:"amount"`
		To     string `json:"to"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &summary); err != nil {
		return transferDeclaration{}, false
	}
	return transferDeclaration{
		token:  normaliseTokenAddress(summary.Token),
		amount: strings.TrimSpace(summary.Amount),
		to:     strings.ToLower(strings.TrimSpace(summary.To)),
	}, true
}

// recipientInSimulation reports whether the declared recipient shows up as
// a value-receiving address in the simulated trace. Two signals; either
// suffices:
//
//   - native value call: any SimulationCall whose To equals `to` and whose
//     Value is non-zero (reuses the same walker as bridge_deposit_match).
//   - SimulationContext.FundFlow: the simulator's aggregated set of
//     addresses that exchanged value with any other address in the trace,
//     covering ERC-20 Transfer events without needing to inspect raw logs.
func recipientInSimulation(c security.SimulationContext, to string) bool {
	if traceCallDeliversNativeTo(c.Sim.Data.Trace, to) {
		return true
	}
	for _, addr := range c.FundFlow {
		if primitives.AddrEq(addr, to) {
			return true
		}
	}
	return false
}

// verifyTransferAmount checks that the declared token + amount actually
// shows up in the simulated effect. Returns nil when the amount is verified,
// or a finding map describing the mismatch.
//
// Native and ERC-20 use different ground-truth sources:
//
//   - native: walk the simulated trace for a call delivering the declared
//     wei amount to declared.to. The rpc simulator builds native
//     BalanceChanges from a prestate balance diff which *includes the gas
//     cost paid in native*, so a magnitude-equal compare against the
//     BalanceChange would false-fail every non-self native transfer (and
//     produce no entry at all for self-transfer, where outflow nets to
//     zero). The trace's call.Value is the EVM-level transferred amount
//     — no gas pollution, no self-transfer net-zero blindspot. Recursive
//     walk handles ERC-7821 batch nesting (EOA → batch executor → inner
//     value call) transparently.
//
//   - ERC-20: compare the owner's BalanceChange asset outflow magnitude
//     against the declared amount. ERC-20 BalanceChanges come from
//     Transfer event logs and reflect the exact ERC-20 amount; gas is
//     paid in native and does not pollute ERC-20 outflows.
func verifyTransferAmount(c security.SimulationContext, declared transferDeclaration) map[string]string {
	matched := false
	if declared.token == "native" {
		matched = traceContainsNativeValueTransferOfAmount(c.Sim.Data.Trace, declared.to, declared.amount)
	} else {
		matched = ownerOutflowMatches(c.Sim.Data.BalanceChanges, c.Req.Owner, declared.token, declared.amount)
	}
	if matched {
		return nil
	}
	return map[string]string{
		"check":           "token_amount_mismatch",
		"expected_token":  declared.token,
		"expected_amount": declared.amount,
		"detail":          fmt.Sprintf("simulated effect does not show an outflow of %s %s from %s.", declared.amount, declared.token, c.Req.Owner),
	}
}

// ownerOutflowMatches reports whether the owner's BalanceChange shows an
// outflow asset matching the declared token+amount.
//
// Token comparison is native-normalised. Amount comparison is decimal-rat
// equality on the absolute value (BalanceChangeAsset.Amount is signed; a
// transfer outflow has the form "-1.5").
func ownerOutflowMatches(changes []security.BalanceChange, owner, expectedToken, expectedAmount string) bool {
	expectedAmountRat, ok := primitives.DecimalRat(expectedAmount)
	if !ok {
		return false
	}
	for _, bc := range changes {
		if !primitives.AddrEq(bc.Account, owner) {
			continue
		}
		for _, asset := range bc.Assets {
			if !primitives.AddrEq(normaliseTokenAddress(asset.Address), expectedToken) {
				continue
			}
			// Asset.Amount has the form "-1.5" for outflow; strip the
			// sign and compare magnitudes.
			magnitude := strings.TrimPrefix(strings.TrimSpace(asset.Amount), "-")
			gotRat, ok := primitives.DecimalRat(magnitude)
			if !ok {
				continue
			}
			if expectedAmountRat.Cmp(gotRat) == 0 {
				return true
			}
		}
	}
	return false
}

// traceContainsNativeValueTransferOfAmount walks the call tree looking for
// any call that delivers exactly `expectedHumanAmount` of native value to
// `to`. The human-readable amount is converted to wei using 18 decimals
// (every chain we support uses 18-decimal native units). Recursive on
// SimulationCall.Calls.
func traceContainsNativeValueTransferOfAmount(call *security.SimulationCall, to, expectedHumanAmount string) bool {
	expectedWei, ok := humanNativeToWei(expectedHumanAmount)
	if !ok {
		return false
	}
	return walkTraceForNativeValue(call, to, expectedWei)
}

func walkTraceForNativeValue(call *security.SimulationCall, to string, expectedWei *big.Int) bool {
	if call == nil {
		return false
	}
	if primitives.AddrEq(call.To, to) {
		if v, ok := primitives.ParseInteger(call.Value); ok && v.Cmp(expectedWei) == 0 {
			return true
		}
	}
	for i := range call.Calls {
		if walkTraceForNativeValue(&call.Calls[i], to, expectedWei) {
			return true
		}
	}
	return false
}

// humanNativeToWei converts a decimal native-unit string (e.g. "0.0000506")
// to a wei *big.Int using 18 decimals. Returns false when the input is not
// a parseable decimal or when the precision exceeds 18 fractional digits
// (which would silently lose information on conversion).
//
// The sub-wei refusal is load-bearing: the only caller compares the result
// to an on-chain call.Value via Cmp == 0. Silent truncation would produce
// both false-fails (declared 0.0...0001 ETH → 0 wei, never matches a
// real-value transfer) and false-passes (two distinct sub-wei amounts
// truncating to the same floor wei look equal). Surfacing the precision
// problem as ok=false lets the caller emit a meaningful finding instead
// of pretending the comparison was valid.
func humanNativeToWei(human string) (*big.Int, bool) {
	rat, ok := primitives.DecimalRat(human)
	if !ok {
		return nil, false
	}
	weiPerUnit := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	weiRat := new(big.Rat).Mul(rat, new(big.Rat).SetInt(weiPerUnit))
	num := weiRat.Num()
	denom := weiRat.Denom()
	if new(big.Int).Rem(num, denom).Sign() != 0 {
		return nil, false
	}
	return new(big.Int).Quo(num, denom), true
}

// extraCalldataOps scans the decoded operations for anything that is not a
// transfer call. Returns a human-readable summary of the first unexpected
// op encountered, or empty string when the calldata is clean.
//
// Allowed: ERC-20 transfer (0xa9059cbb), ERC-20 transferFrom (0x23b872dd),
// or an empty-calldata native transfer (selector "0x").
// Disallowed: approve (0x095ea7b3), setApprovalForAll (0xa22cb465), or any
// other selector.
func extraCalldataOps(ops []security.Operation) string {
	const (
		erc20Transfer     = "0xa9059cbb"
		erc20TransferFrom = "0x23b872dd"
		approve           = primitives.SelectorApprove // "0x095ea7b3"
		setApprovalForAll = "0xa22cb465"
		emptyMethod       = "0x"
	)
	for i, op := range ops {
		method := primitives.MethodID(op.Data)
		switch method {
		case erc20Transfer, erc20TransferFrom, emptyMethod:
			continue
		case approve:
			return fmt.Sprintf("op[%d] is approve(...) at token %s", i, op.To.Hex())
		case setApprovalForAll:
			return fmt.Sprintf("op[%d] is setApprovalForAll(...) at token %s", i, op.To.Hex())
		default:
			return fmt.Sprintf("op[%d] uses unexpected selector %s at %s", i, method, op.To.Hex())
		}
	}
	return ""
}

// trustedDelegateImplementation is the EIP-7702 delegate contract our managed
// wallets install (MetaMask Delegator). Because a 7702-delegated EOA executes
// EVERY transaction through an EVM-enforced designator DELEGATECALL into this
// implementation, that one frame is always present and is NOT a red flag — it
// is the wallet delegating to its own installed code. This mirrors
// vault.MMDelegatorAddress; kept as a local constant so the pure check logic
// need not pull in the vault package's gorm/storage dependency tree.
const trustedDelegateImplementation = "0x63c0c19a282a1b52b07dd5a65b58948a07dae32b"

// traceUnexpectedDelegateCall walks the call tree for a DELEGATECALL that
// originates from the wallet owner AND targets something other than the
// wallet's trusted delegate. Two categories are safe and ignored:
//
//   - From=owner, To=trustedDelegateImplementation — the EIP-7702 designator
//     delegation into the wallet's own installed code. Always present.
//   - From≠owner — contract-internal proxy delegation (e.g., USDC proxy →
//     USDC implementation). This is normal upgradeable-contract architecture
//     and does not affect the wallet's execution context.
//
// Only From=owner + To≠trusted is dangerous: it means the wallet handed its
// execution context to an unknown contract.
func traceUnexpectedDelegateCall(call *security.SimulationCall, owner string) (string, bool) {
	if call == nil {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(call.Type), "DELEGATECALL") &&
		primitives.AddrEq(call.From, owner) &&
		!primitives.AddrEq(call.To, trustedDelegateImplementation) {
		return call.To, true
	}
	for i := range call.Calls {
		if target, found := traceUnexpectedDelegateCall(&call.Calls[i], owner); found {
			return target, true
		}
	}
	return "", false
}
