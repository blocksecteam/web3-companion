// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security"
)

// BurnAddressCheck blocks transfers whose declared recipient is a hardcoded
// burn / dead address. Funds sent to these addresses are permanently
// destroyed and unrecoverable — almost always a user mistake or a malicious
// build substitution. The check fails hard so the aggregate verdict turns
// `fail`; the downstream Security Agent then reports unsafe with high
// confidence, which the review hook maps to a `stop` decision. The user can
// still override explicitly via the normal `web3 security override` flow —
// the rule's job is to refuse silent sign-through, not to forbid burning
// outright for the rare case the user genuinely intends it.
//
// Scope is intentionally narrow:
//
//   - AppliesTo is "transfer" only. Swap and bridge intents have their
//     own intent-match rules that catch a router substituting the
//     destination; they also involve multi-hop routing where a burn
//     address can legitimately appear in the trace (LP burns, etc.).
//     Mixing those into this rule would invite false positives.
//
//   - The hardcoded list is conservative on purpose. Only addresses with
//     near-universal recognition as burn destinations are listed. Less
//     conventional burn lookalikes (vanity patterns like 0xdead…0d06,
//     repeating-byte addresses, protocol-specific burn vaults) are left
//     to the LLM Security Agent's pattern detection in companion/
//     security.md, which has the latitude to "warn" rather than "fail"
//     and can ask the user for confirmation. A hardcoded hard-fail on a
//     fuzzy match would false-positive too often.
//
// Native and ERC-20 transfers share the same tx_summary schema (`to` is
// always the declared recipient), so the rule does not need to branch on
// token type.
type BurnAddressCheck struct{}

func (BurnAddressCheck) Rule() string        { return "burn_address_check" }
func (BurnAddressCheck) AppliesTo() []string { return []string{"transfer"} }

func (BurnAddressCheck) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Burn Address Check",
		Description: "Blocks transfers (native or ERC-20) whose declared recipient is a well-known burn / dead address. Burning is permanent and irreversible; the user must explicitly reconfirm before any override.",
		Statuses: map[string]string{
			"pass": "Declared recipient is not in the hardcoded burn-address list.",
			"warn": "tx_summary is missing or unparseable; cannot evaluate the recipient against the burn list.",
			"fail": "Declared recipient matches a known burn address. Transferring here permanently destroys the funds.",
		},
	}
}

// hardcodedBurnAddresses keys are lowercase 0x-prefixed 40-hex strings. Add
// new entries only when the address has broad community recognition as an
// intentional burn destination — the LLM Security Agent (companion/
// security.md) covers fuzzy / vanity lookalikes by pattern, and overlap
// between this hard list and the pattern layer is intentional.
var hardcodedBurnAddresses = map[string]bool{
	"0x0000000000000000000000000000000000000000": true, // zero address
	"0x000000000000000000000000000000000000dead": true, // canonical dEaD
	"0xdead000000000000000000000000000000000000": true, // leading-dead variant
	"0xdead000000000000000042069420694206942069": true, // Vitalik's public burn (used by SHIB and others)
	"0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead": true, // all-dead repeating
}

func (BurnAddressCheck) Run(c security.ShapeContext) security.CheckResult {
	to, ok := extractTransferRecipient(c.Req.SummaryJSON)
	if !ok {
		return security.CheckResult{
			Rule:   "burn_address_check",
			Status: "warn",
			Detail: "tx_summary missing or unparseable; cannot evaluate recipient against the burn-address list.",
		}
	}
	if hardcodedBurnAddresses[to] {
		return security.CheckResult{
			Rule:   "burn_address_check",
			Status: "fail",
			Detail: fmt.Sprintf("declared recipient %s matches a known burn address; transferring here permanently destroys the funds.", to),
			Observed: map[string]string{
				"recipient": to,
			},
		}
	}
	return security.CheckResult{
		Rule:   "burn_address_check",
		Status: "pass",
		Detail: fmt.Sprintf("declared recipient %s is not in the hardcoded burn-address list.", to),
	}
}

// extractTransferRecipient pulls the lowercased `to` field out of the raw
// tx_summary JSON. Returns ok=false when the summary is absent or
// unparseable or `to` is empty, so the caller can degrade to warn instead
// of false-failing.
func extractTransferRecipient(rawJSON string) (string, bool) {
	if rawJSON == "" {
		return "", false
	}
	var summary struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &summary); err != nil {
		return "", false
	}
	to := strings.ToLower(strings.TrimSpace(summary.To))
	if to == "" {
		return "", false
	}
	return to, true
}
