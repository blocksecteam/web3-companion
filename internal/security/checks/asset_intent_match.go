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

// AssetIntentMatch verifies that the assets actually moved in the simulated
// trace match what the build phase declared in tx_summary — catches
// router-substitution attacks and backend bugs where the calldata trades a
// different token than was committed to.
//
// Scope per tx_type:
//
//	swap   — both sides checked
//	         effect.spending must contain summary.source_asset
//	         effect.receiving must contain summary.target_asset
//	bridge — source side only
//	         effect.spending must contain summary.from_token.
//	         The target side is intentionally out of scope: the destination
//	         chain runs a different EVM that the source-chain simulator
//	         cannot observe, so summary.to_token cannot be verified here. A
//	         separate guarantee is needed if the destination side ever
//	         needs end-to-end matching.
//
// Token addresses are normalised — "native" / 0x0 / 0xEee… are treated as
// the same identity so an EIP-155 placeholder in summary matches whatever
// representation the simulator emitted for the chain-native token.
//
// Amount comparison is intentionally NOT done here: swap stores input/output
// as raw wei strings while effect.spending.Amount is a human-readable
// decimal, and exact comparison would require resolving token decimals from
// an RPC call. Value-level mismatches are already caught by
// usd_value_conservation; this rule's job is the higher-leverage signal
// "did we move the declared TOKEN at all?"
type AssetIntentMatch struct{}

func (AssetIntentMatch) Rule() string        { return "asset_intent_match" }
func (AssetIntentMatch) AppliesTo() []string { return []string{"swap", "bridge"} }
func (AssetIntentMatch) Description() security.RuleDescription {
	return security.RuleDescription{
		Name:        "Asset Intent Match",
		Description: "Verifies that the assets actually moved in the simulated trace match what tx_summary declared. For swap, checks both the source asset (in owner's spending) and target asset (in owner's receiving). For bridge, only the source asset is checked — the destination chain is not observable from the source-chain simulator, so bridge target tokens are intentionally out of scope. Catches router-substitution attacks and backend bugs where the calldata moves a different source asset than declared.",
		Statuses: map[string]string{
			"pass": "For swap, simulated spending includes the declared source token and simulated receiving includes the declared target token. For bridge, simulated spending includes the declared source token (target is not verified on source chain).",
			"warn": "Summary is unavailable or unparseable; cannot verify the move matches intent.",
			"fail": "A declared token is absent from the simulated spending (swap or bridge) or the simulated receiving (swap only) — calldata is moving a different asset than the build phase committed to.",
		},
	}
}
func (AssetIntentMatch) Run(c security.EffectContext) security.CheckResult {
	declared, ok := extractAssetDeclaration(c.Req.TxType, c.Req.SummaryJSON)
	if !ok {
		return security.CheckResult{
			Rule:   "asset_intent_match",
			Status: "warn",
			Detail: "tx_summary missing or unparseable; cannot verify declared assets against simulated effect.",
		}
	}

	findings := make([]map[string]string, 0)
	if declared.source != "" && !effectContainsToken(c.Effect.Spending, declared.source) {
		findings = append(findings, map[string]string{
			"check":          "source_missing",
			"expected_token": declared.source,
			"expected_in":    "effect.spending",
			"detail":         fmt.Sprintf("declared source token %s does not appear in the owner's simulated spending.", declared.source),
		})
	}
	if declared.target != "" && !effectContainsToken(c.Effect.Receiving, declared.target) {
		findings = append(findings, map[string]string{
			"check":          "target_missing",
			"expected_token": declared.target,
			"expected_in":    "effect.receiving",
			"detail":         fmt.Sprintf("declared target token %s does not appear in the owner's simulated receiving.", declared.target),
		})
	}

	if len(findings) > 0 {
		return security.CheckResult{
			Rule:     "asset_intent_match",
			Status:   "fail",
			Detail:   fmt.Sprintf("%d declared token(s) missing from simulated effect.", len(findings)),
			Observed: findings,
		}
	}

	detail := fmt.Sprintf("simulated spending includes declared source token %s", displayToken(declared.source))
	if c.Req.TxType == "swap" {
		detail += fmt.Sprintf("; simulated receiving includes declared target token %s", displayToken(declared.target))
	} else {
		detail += " (bridge: target token on destination chain is out of scope for this rule)"
	}
	return security.CheckResult{
		Rule:   "asset_intent_match",
		Status: "pass",
		Detail: detail,
	}
}

// assetDeclaration is the (source, target) the build phase committed to,
// normalised. target is empty for bridge transactions (destination chain is
// not in scope here) or when the summary omits it.
type assetDeclaration struct {
	source string
	target string
}

// extractAssetDeclaration pulls source/target token addresses out of the
// raw tx_summary JSON. Returns ok=false when the summary is absent or
// unparseable so the rule can degrade to warn instead of false-failing.
func extractAssetDeclaration(txType, rawJSON string) (assetDeclaration, bool) {
	if rawJSON == "" {
		return assetDeclaration{}, false
	}
	var summary struct {
		SourceAsset string `json:"source_asset"`
		TargetAsset string `json:"target_asset"`
		FromToken   string `json:"from_token"`
		ToToken     string `json:"to_token"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &summary); err != nil {
		return assetDeclaration{}, false
	}
	switch txType {
	case "swap":
		return assetDeclaration{
			source: normaliseTokenAddress(summary.SourceAsset),
			target: normaliseTokenAddress(summary.TargetAsset),
		}, true
	case "bridge":
		return assetDeclaration{
			source: normaliseTokenAddress(summary.FromToken),
			// target intentionally empty: bridge destination chain is not
			// simulated by the source-chain simulator, so we cannot verify
			// it here even if summary.to_token is present.
		}, true
	}
	return assetDeclaration{}, false
}

// effectContainsToken reports whether `assets` includes an entry whose
// TokenAddress matches `expected` after native-normalisation.
func effectContainsToken(assets []security.AssetEffect, expected string) bool {
	for _, a := range assets {
		if primitives.AddrEq(normaliseTokenAddress(a.TokenAddress), expected) {
			return true
		}
	}
	return false
}

// normaliseTokenAddress collapses the three common native-token spellings —
// the bare keyword "native", the zero address, and the EIP-155 placeholder
// 0xEee… — into a single canonical form so cross-source comparison works
// regardless of which dialect either side used.
func normaliseTokenAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	switch strings.ToLower(addr) {
	case "native",
		"0x0",
		"0x0000000000000000000000000000000000000000",
		"0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee":
		return "native"
	}
	return strings.ToLower(addr)
}

// displayToken returns a short token identifier for inclusion in the pass
// detail line. Empty when not declared (e.g. bridge target is intentionally
// omitted from the rule).
func displayToken(addr string) string {
	if addr == "" {
		return "<none>"
	}
	return addr
}
