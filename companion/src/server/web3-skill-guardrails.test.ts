// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const repoRoot = join(import.meta.dir, "../../..");

describe("web3 skill guardrails", () => {
  test("SSM skill keeps the sign-vs-delegate resume paths distinct and points at the explicit `--wallet --chain --challenge-id` form", () => {
    const ssm = readFileSync(join(repoRoot, "skills/web3-ssm/SKILL.md"), "utf8");
    const sharedPasskey = readFileSync(join(repoRoot, "skills/_shared/passkey.md"), "utf8");

    // The delegate sub-flow must tell the agent explicitly not to use
    // sign-status for the resume — both flows emit `pending_user_auth` and
    // the agent will pick the wrong resume command without this guard.
    // Markdown emphasis on "not" is significant (the agent reads the rendered
    // skill); dropping the bold would weaken the signal.
    expect(ssm).toContain("**not** `sign-status`");
    // The resume command shape must use the `--wallet` flag form so it stays
    // aligned with every other write command — positional-vs-flag drift
    // causes agents to misfire their first invocation.
    expect(sharedPasskey).toContain("web3 ssm delegate --wallet <wallet_id> --chain <chain> --challenge-id <challenge_id>");
    // The shared passkey NEVER block keeps the wrong-resume-path prohibition
    // as a final guard even if a future SKILL edit dropped the inline hint.
    expect(sharedPasskey).toContain("Never use the wrong resume path");
  });

  test("bridge/swap/chain SKILLs name the trigger scenarios that require reading _shared/amount-resolution.md", () => {
    // Claude Agent SDK inlines a Skill's own SKILL.md when the skill
    // is invoked, but does NOT automatically inline files the SKILL.md
    // references (like _shared/amount-resolution.md). The agent has to
    // decide to Read them. A short "Follow ~/.claude/skills/_shared/X"
    // pointer in the SKILL.md gives the agent permission but no signal
    // about WHEN it's load-bearing — for the cases that matter, like
    // "全部 ETH" turning into a stale-balance simulation revert.
    //
    // The fix is to enumerate the triggering scenarios in each SKILL.md
    // (full-balance "all/max/全部", fiat amount, bare ambiguous number)
    // so the agent gets the signal at the point it's deciding what to
    // do, without duplicating the recipe itself. If any of these three
    // SKILLs lose that enumeration, agents drift back to inferring
    // amounts from the rendered portfolio float.
    for (const path of [
      "skills/web3-bridge/SKILL.md",
      "skills/web3-swap/SKILL.md",
      "skills/web3-chain/SKILL.md",
    ]) {
      const md = readFileSync(join(repoRoot, path), "utf8");
      expect(md, path).toContain("MUST read that file before issuing the CLI command");
      expect(md, path).toContain("\"全部\"");
      expect(md, path).toContain("fiat amount");
    }
  });

  test("multi-operation orchestration default is per-operation serial, not batched build", () => {
    const companion = readFileSync(join(repoRoot, "companion/web3_companion.md"), "utf8");

    // When the user requests multiple writes (e.g. "swap A, B, and C"),
    // agents had been observed batching all builds + checks first and
    // only reaching sign at the end. That left earlier signed
    // transactions stale across minutes of build/check time, which on
    // Base in particular blew past the (now 4x) MaxFeePerGas headroom
    // and forced silent rebuild loops.
    //
    // Three load-bearing pieces have to travel together:
    //   - the explicit pipeline order: build → security check → sign → broadcast
    //   - the "on top of, not instead of" callout so agents don't read
    //     this rule as a license to skip wait_user_confirm / passkey halt
    //   - the quote-only exception with explicit user gating ("compare
    //     options") — without that constraint agents tend to batch
    //     quotes by default and the rule loses force
    expect(companion).toContain("`build` → `security check` → `sign` → `broadcast` — before starting the next");
    expect(companion).toContain("on top of, not instead of");
    expect(companion).toContain("Quotes are the only allowed exception");
  });

  test("amount-resolution covers full-balance ops without silently sending the rendered float", () => {
    const amountRes = readFileSync(join(repoRoot, "skills/_shared/amount-resolution.md"), "utf8");

    // Real failure mode (USDT BSC, 18 decimals): the portfolio
    // balance string is rendered at ~16 significant digits from a
    // float, which is sometimes rounded UP from the actual on-chain
    // wei. Agent copying that verbatim into --amount produces an
    // approve + transferFrom batch where transferFrom reverts on
    // insufficient balance. The recipe trims the trailing digit by
    // 1, leaving sub-rendered-unit dust.
    //
    // Three load-bearing pieces have to stay together:
    //   - the 4-step recipe so agents that read partial context
    //     still get the algorithm
    //   - the 6-significant-digit pre-condition that gates small or
    //     round-number balances out (otherwise dust is user-visible)
    //   - the NEVER block as a final guard against any future skill
    //     edit that drops the inline rule
    expect(amountRes).toContain("Subtract 1 from the last digit");
    expect(amountRes).toContain("at least 6 significant digits");
    expect(amountRes).toContain("Never copy the portfolio balance string verbatim");

    // Native full-balance ops have an additional gas-reserve step the
    // trim-only recipe doesn't cover — without it, a "全部 bridge BNB"
    // request would still revert "insufficient balance for gas". The
    // Gas reserve uses dynamic estimation, not hardcoded limits.
    expect(amountRes).toContain("Extra step for NATIVE token full-balance operations");
    expect(amountRes).toContain("eth_estimateGas");
    expect(amountRes).toContain("`500_000`");
    expect(amountRes).toContain("ERC-20 full operations (token ≠ native) do NOT deduct gas");
  });

  test("override resume guidance forbids the no-challenge-id re-invocation that traps users in a Passkey-confirm loop", () => {
    const sharedPasskey = readFileSync(join(repoRoot, "skills/_shared/passkey.md"), "utf8");

    // Real user incident: after the user approved Passkey for an override,
    // the agent re-ran `web3 security override` WITHOUT `--challenge-id`,
    // believing it was a "check status" call. Backend treated it as a fresh
    // override request, issued a new challenge_id, and prompted the user
    // for Passkey again — repeating indefinitely. Two reinforcements need
    // to stay co-located in the same skill so the agent encounters both:
    //   - prose right after the resume table explains "the resume IS the
    //     status check; no separate endpoint"
    //   - a NEVER block entry forbids the specific failure mode
    // Either alone is insufficient: the prose without the NEVER reads as
    // background colour, and the NEVER without the prose looks like an
    // unmotivated prohibition. Keep both.
    expect(sharedPasskey).toContain("no separate status endpoint");
    expect(sharedPasskey).toContain("Never \"check status\" by re-invoking `web3 security override` without `--challenge-id`");
  });

  test("delegation_failed RPC recovery hint stays in the shared error-codes table", () => {
    const errorCodes = readFileSync(join(repoRoot, "skills/_shared/error-codes.md"), "utf8");

    // Surface the misconfigured-RPC clue when the underlying go-ethereum
    // error message bubbles up through `delegation_failed`. Centralising
    // this in error-codes.md (not the SKILL) is intentional — keeps the
    // SKILL flow-shaped and the error reference table message-shaped.
    expect(errorCodes).toContain("invalid chain ID");
    expect(errorCodes).toContain("RPC for the target chain looks misconfigured");
  });

  test("skills rely on passkey hook blocks instead of asking agents to show challenge links", () => {
    const ssm = readFileSync(join(repoRoot, "skills/web3-ssm/SKILL.md"), "utf8");
    const sharedPasskey = readFileSync(join(repoRoot, "skills/_shared/passkey.md"), "utf8");
    const companionGuide = readFileSync(join(repoRoot, "companion/web3_companion.md"), "utf8");
    const combined = [ssm, sharedPasskey, companionGuide].join("\n");

    // The user-facing rule is that the Passkey button appears alongside the
    // agent's message. Wording must not leak the implementation detail
    // "frontend creates ... from structured Bash tool output" — the
    // agent-facing invariant is what matters.
    expect(combined).toContain("Passkey confirmation button is rendered automatically");
    expect(combined).toContain("Passkey confirmation button is shown automatically");
    expect(combined).not.toContain("Please open the following link");
    expect(combined).not.toContain("If you include the confirmation URL");
    expect(companionGuide).not.toContain("include the provided `sign_url`");
    // `sign_url` must not be surfaced — agent should rely on the rendered
    // button only.
    expect(sharedPasskey).toContain("Never display, wrap, reconstruct, shorten, or retype `sign_url`");
  });
});
