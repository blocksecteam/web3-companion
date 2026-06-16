// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package checks

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/security"
)

func runBurnAddressCheck(t *testing.T, summaryJSON string) security.CheckResult {
	t.Helper()
	return BurnAddressCheck{}.Run(security.ShapeContext{
		Req: security.CheckRequest{SummaryJSON: summaryJSON},
	})
}

func TestBurnAddressCheck_HitsCanonicalDeadAddress(t *testing.T) {
	// Mixed-case input must still match (the hardcoded list is lowercase;
	// extractTransferRecipient lowercases the input before lookup). EIP-55
	// checksum addresses are how most UI surfaces present this address —
	// rejecting the checksummed form would let the most common case slip
	// through.
	result := runBurnAddressCheck(t, `{"to":"0x000000000000000000000000000000000000dEaD","amount":"1","token":"native"}`)
	if result.Status != "fail" {
		t.Fatalf("expected fail for canonical dEaD address, got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_HitsZeroAddress(t *testing.T) {
	result := runBurnAddressCheck(t, `{"to":"0x0000000000000000000000000000000000000000","amount":"1","token":"native"}`)
	if result.Status != "fail" {
		t.Fatalf("expected fail for zero address, got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_HitsVitalikBurnAddress(t *testing.T) {
	// 0xdEAD000000000000000042069420694206942069 — the address Vitalik
	// publicly used to burn SHIB. In production-traded tokens, this is
	// the third-most-common burn target after 0x0 and 0x...dEaD.
	result := runBurnAddressCheck(t, `{"to":"0xdEAD000000000000000042069420694206942069","amount":"100","token":"0xabc"}`)
	if result.Status != "fail" {
		t.Fatalf("expected fail for Vitalik's burn address, got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_PassesForNormalAddress(t *testing.T) {
	result := runBurnAddressCheck(t, `{"to":"0xd8da6bf26964af9d7eed9e03e53415d37aa96045","amount":"1","token":"native"}`)
	if result.Status != "pass" {
		t.Fatalf("expected pass for normal EOA, got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_PassesForVanityLookalike(t *testing.T) {
	// Intentionally a lookalike that's NOT in the hardcoded list (note
	// the trailing bytes differ from any registered burn address). This
	// rule must NOT hard-fail on fuzzy resemblance — that judgement
	// belongs to the LLM Security Agent in companion/security.md, which
	// can warn and ask the user. A static hard-fail on resemblance would
	// false-positive every vanity address with "dead" in it.
	result := runBurnAddressCheck(t, `{"to":"0xdeadbeef12345678901234567890123456789012","amount":"1","token":"native"}`)
	if result.Status != "pass" {
		t.Fatalf("expected pass for non-registered vanity lookalike (pattern detection belongs to the Security Agent layer), got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_WarnsOnMissingSummary(t *testing.T) {
	result := runBurnAddressCheck(t, "")
	if result.Status != "warn" {
		t.Fatalf("expected warn for missing summary, got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_WarnsOnUnparseableSummary(t *testing.T) {
	result := runBurnAddressCheck(t, `{not json`)
	if result.Status != "warn" {
		t.Fatalf("expected warn for unparseable summary, got status=%q detail=%q", result.Status, result.Detail)
	}
}

func TestBurnAddressCheck_WarnsWhenToFieldEmpty(t *testing.T) {
	// Well-formed summary but `to` missing/empty — degrade to warn
	// rather than treating empty as "not a burn address" pass.
	result := runBurnAddressCheck(t, `{"amount":"1","token":"native"}`)
	if result.Status != "warn" {
		t.Fatalf("expected warn for empty `to` field, got status=%q detail=%q", result.Status, result.Detail)
	}
}
