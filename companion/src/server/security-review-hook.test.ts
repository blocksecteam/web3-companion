// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { mkdtemp, rm, writeFile as writeFileFn } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import type { ChatMessage, ServerWsEvent } from "../types/chat";
import {
  createSecurityReviewHook,
  parseSecurityAgentConfig,
  parseSecurityCheckFromTool,
  resolvePersistedOutput,
  securityReviewSystemMessage,
  shouldReviewSecurityCheck,
} from "./security-review-hook";

describe("security review hook helpers", () => {
  test("parses security agent config from backend-managed JSON", () => {
    expect(
      parseSecurityAgentConfig(
        JSON.stringify({
          review_mode: "warn_only",
          allow_review_clear: false,
        }),
      ),
    ).toEqual({
      reviewMode: "warn",
      allowReviewClear: false,
    });
  });

  test("defaults incomplete security agent config conservatively", () => {
    expect(parseSecurityAgentConfig("{}")).toEqual({
      reviewMode: "always",
      allowReviewClear: true,
    });
  });

  test("parses web3 security check JSON from a Bash tool response", () => {
    const check = parseSecurityCheckFromTool(
      { command: "web3 security check --transaction-id txi_123" },
      {
        stdout: JSON.stringify({
          transaction_id: "txi_123",
          status: "security_review_required",
          verdict: "warn",
          summary: "verdict=warn; warn=[gas_cost_sanity]",
          checks: [
            { rule: "gas_cost_sanity", status: "warn", detail: "gas is high" },
          ],
          simulation: {
            provider: "rpc",
            trace: { to: "0xabc" },
            balanceChanges: [],
          },
        }),
      },
    );

    expect(check?.transactionId).toBe("txi_123");
    expect(check?.verdict).toBe("warn");
    expect(check?.checks[0]?.rule).toBe("gas_cost_sanity");
  });

  test("parses security check JSON from output merged with stderr", () => {
    const check = parseSecurityCheckFromTool(
      { command: "web3 security check --transaction-id txi_123 2>&1" },
      [
        "warning: retrying slow RPC endpoint",
        JSON.stringify({
          transaction_id: "txi_123",
          status: "security_review_required",
          verdict: "warn",
          checks: [{ rule: "gas_cost_sanity", status: "warn", detail: "gas is high" }],
        }),
      ].join("\n"),
    );

    expect(check?.transactionId).toBe("txi_123");
    expect(check?.verdict).toBe("warn");
    expect(check?.checks[0]?.rule).toBe("gas_cost_sanity");
  });

  test("parses security check JSON when stderr contains braces", () => {
    const check = parseSecurityCheckFromTool(
      { command: "web3 security check --transaction-id txi_123 2>&1" },
      [
        "warning: provider returned {temporary retry}",
        JSON.stringify({
          transaction_id: "txi_123",
          status: "security_review_required",
          verdict: "warn",
          checks: [{ rule: "rpc_trace", status: "warn", detail: "retry succeeded" }],
        }),
        "debug: cache={hit}",
      ].join("\n"),
    );

    expect(check?.transactionId).toBe("txi_123");
    expect(check?.checks[0]?.rule).toBe("rpc_trace");
  });

  test("review mode warn only reviews warn or worse transactions", () => {
    const config = parseSecurityAgentConfig(
      JSON.stringify({ review_mode: "warn" }),
    );

    expect(
      shouldReviewSecurityCheck({ verdict: "warn" } as never, config),
    ).toBe(true);
    expect(
      shouldReviewSecurityCheck({ verdict: "fail" } as never, config),
    ).toBe(true);
    expect(
      shouldReviewSecurityCheck({ verdict: "pass" } as never, config),
    ).toBe(false);
  });

  test("review mode always reviews pass, warn, and fail transactions", () => {
    const config = parseSecurityAgentConfig(
      JSON.stringify({ review_mode: "always" }),
    );

    expect(
      shouldReviewSecurityCheck({ verdict: "warn" } as never, config),
    ).toBe(true);
    expect(
      shouldReviewSecurityCheck({ verdict: "pass" } as never, config),
    ).toBe(true);
    expect(
      shouldReviewSecurityCheck({ verdict: "fail" } as never, config),
    ).toBe(true);
  });

  test("system message allows continuing to passkey-gated signing by default after safe review", () => {
    const message = securityReviewSystemMessage(
      { transactionId: "txi_123", verdict: "warn" } as never,
      {
        verdict: "safe",
        confidence: "medium",
        reason: "route matches expected flow",
      },
      parseSecurityAgentConfig(JSON.stringify({})),
    );

    expect(message).toContain("verdict=safe");
    expect(message).toContain("decision=passkey_required");
    expect(message).toContain("This is not automatic signing");
  });

  test("low confidence safe review waits for explicit confirmation", () => {
    const message = securityReviewSystemMessage(
      { transactionId: "txi_123", verdict: "warn" } as never,
      {
        verdict: "safe",
        confidence: "low",
        reason: "route may match expected flow",
      },
      parseSecurityAgentConfig(JSON.stringify({})),
    );

    expect(message).toContain("decision=wait_user_confirm");
    expect(message).toContain("Do not call web3 ssm sign yet");
  });

  test("safe review with missing confidence waits for explicit confirmation", () => {
    // Defensive: a safe review without an explicit high/medium confidence must
    // not auto-clear, because clearBackendSecurityReview fallbacks the missing
    // confidence to "low" when writing the Go clearance, and the Go gate
    // (securityClearanceAllowsSigning) then rejects a low-confidence safe.
    const message = securityReviewSystemMessage(
      { transactionId: "txi_123", verdict: "warn" } as never,
      { verdict: "safe", reason: "agent omitted confidence" } as never,
      parseSecurityAgentConfig(JSON.stringify({})),
    );

    expect(message).toContain("decision=wait_user_confirm");
    expect(message).not.toContain("decision=passkey_required");
  });

  test("medium and high confidence unsafe reviews directly stop signing", () => {
    for (const confidence of ["medium", "high"] as const) {
      const message = securityReviewSystemMessage(
        { transactionId: "txi_123", verdict: "pass" } as never,
        { verdict: "unsafe", confidence, reason: "unexpected spender" },
        parseSecurityAgentConfig(JSON.stringify({})),
      );

      expect(message).toContain("decision=stop");
      expect(message).toContain("Do not call web3 ssm sign");
    }
  });

  test("low confidence unsafe reviews wait for explicit confirmation instead of directly rejecting", () => {
    const message = securityReviewSystemMessage(
      { transactionId: "txi_123", verdict: "pass" } as never,
      {
        verdict: "unsafe",
        confidence: "low",
        reason: "review output was uncertain",
      },
      parseSecurityAgentConfig(JSON.stringify({})),
    );

    expect(message).toContain("decision=wait_user_confirm");
    expect(message).toContain("Do not call web3 ssm sign yet");
  });

  test("hook emits reviewing and final security review blocks", async () => {
    const store = new MemoryStore();
    const events: ServerWsEvent[] = [];
    const hook = createSecurityReviewHook({
      sessionId: "session-1",
      store: store as never,
      emit: (event) => events.push(event),
      config: parseSecurityAgentConfig(JSON.stringify({ review_mode: "warn" })),
      runSecurityAgent: async () => ({
        verdict: "unsafe",
        confidence: "high",
        reason: "unexpected outflow",
        markdown: "### Web3 Security Agent Review: Unsafe\n\nDo not sign.",
      }),
    });

    const response = await hook(
      {
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_use_id: "tool-1",
        tool_input: { command: "web3 security check --transaction-id txi_123" },
        tool_response: {
          stdout: JSON.stringify({
            transaction_id: "txi_123",
            verdict: "warn",
            checks: [
              {
                rule: "usd_value_conservation",
                status: "warn",
                detail: "value loss",
              },
            ],
          }),
        },
      } as never,
      undefined,
      { signal: new AbortController().signal },
    );

    const blockEvents = events.filter((event) => event.type === "block_update");
    expect(blockEvents).toHaveLength(2);
    expect(blockEvents[0].block.type).toBe("security_review");
    expect(blockEvents[0].block.status).toBe("reviewing");
    const reviewingBlock = blockEvents[0].block;
    if (reviewingBlock.type !== "security_review") throw new Error("expected security review block");
    expect(reviewingBlock.markdown).toContain("Web3 Security Agent");
    expect(reviewingBlock.markdown).not.toMatch(/(?:\.{3}|…)$/);
    expect(blockEvents[1].block.type).toBe("security_review");
    expect(blockEvents[1].block.status).toBe("unsafe");
    const unsafeBlock = blockEvents[1].block;
    if (unsafeBlock.type !== "security_review") throw new Error("expected security review block");
    expect(unsafeBlock.markdown).toContain("Do not sign.");
    expect("systemMessage" in response ? response.systemMessage : "").toContain(
      "decision=stop",
    );
  });

  test("hook passes the current chat session language to the security agent", async () => {
    const store = new MemoryStore();
    store.addMessage("session-1", {
      id: "user-1",
      role: "user",
      content: "帮我检查这笔交易",
      createdAt: "2026-05-20T00:00:00.000Z",
    });
    const seenLanguages: string[] = [];
    const hook = createSecurityReviewHook({
      sessionId: "session-1",
      store: store as never,
      emit: () => undefined,
      config: parseSecurityAgentConfig(JSON.stringify({ review_mode: "warn" })),
      runSecurityAgent: async (_check, { sessionLanguage }) => {
        seenLanguages.push(sessionLanguage);
        return {
          verdict: "unsafe",
          confidence: "high",
          reason: "unexpected outflow",
          markdown: "### Web3 Security Agent Review: Unsafe",
        };
      },
    });

    await hook(
      {
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_use_id: "tool-1",
        tool_input: { command: "web3 security check --transaction-id txi_123" },
        tool_response: {
          stdout: JSON.stringify({
            transaction_id: "txi_123",
            verdict: "warn",
            checks: [{ rule: "usd_value_conservation", status: "warn", detail: "value loss" }],
          }),
        },
      } as never,
      undefined,
      { signal: new AbortController().signal },
    );

    expect(seenLanguages).toEqual(["zh"]);
  });

  test("hook clears backend review before allowing signing when security agent marks pass safe in always mode", async () => {
    const store = new MemoryStore();
    const events: ServerWsEvent[] = [];
    const cleared: unknown[] = [];
    const hook = createSecurityReviewHook({
      sessionId: "session-1",
      store: store as never,
      emit: (event) => events.push(event),
      config: parseSecurityAgentConfig(
        JSON.stringify({ review_mode: "always" }),
      ),
      runSecurityAgent: async () => ({
        verdict: "safe",
        confidence: "high",
        reason: "trace and balance changes match the quoted swap",
        markdown: "### Web3 Security Agent Review: Safe",
      }),
      clearSecurityReview: async (check, review) => {
        cleared.push({ check, review });
      },
    });

    const response = await hook(
      {
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_use_id: "tool-1",
        tool_input: { command: "web3 security check --transaction-id txi_123" },
        tool_response: {
          stdout: JSON.stringify({
            transaction_id: "txi_123",
            status: "security_review_required",
            verdict: "pass",
            checks: [
              {
                rule: "usd_value_conservation",
                status: "pass",
                detail: "value preserved",
              },
            ],
          }),
        },
      } as never,
      undefined,
      { signal: new AbortController().signal },
    );

    expect(cleared).toHaveLength(1);
    const systemMessage =
      "systemMessage" in response ? response.systemMessage : "";
    expect(systemMessage).toContain("decision=passkey_required");
    expect(systemMessage).toContain("You may continue to web3 ssm sign");
  });

  test("hook clears backend review before allowing signing when security agent marks fail safe", async () => {
    const store = new MemoryStore();
    const events: ServerWsEvent[] = [];
    const cleared: unknown[] = [];
    const hook = createSecurityReviewHook({
      sessionId: "session-1",
      store: store as never,
      emit: (event) => events.push(event),
      config: parseSecurityAgentConfig(JSON.stringify({ review_mode: "warn" })),
      runSecurityAgent: async () => ({
        verdict: "safe",
        confidence: "medium",
        reason:
          "static failure was explained by a known bridge settlement trace",
        markdown: "### Web3 Security Agent Review: Safe",
      }),
      clearSecurityReview: async (check, review) => {
        cleared.push({ check, review });
      },
    });

    const response = await hook(
      {
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_use_id: "tool-1",
        tool_input: { command: "web3 security check --transaction-id txi_456" },
        tool_response: {
          stdout: JSON.stringify({
            transaction_id: "txi_456",
            status: "security_review_required",
            verdict: "fail",
            checks: [
              {
                rule: "simulation_success",
                status: "fail",
                detail: "execution reverted",
              },
            ],
          }),
        },
      } as never,
      undefined,
      { signal: new AbortController().signal },
    );

    expect(cleared).toHaveLength(1);
    const systemMessage =
      "systemMessage" in response ? response.systemMessage : "";
    expect(systemMessage).toContain("decision=passkey_required");
    expect(systemMessage).toContain("You may continue to web3 ssm sign");
  });

  test("hook stops signing when security agent review process fails", async () => {
    const store = new MemoryStore();
    const events: ServerWsEvent[] = [];
    const hook = createSecurityReviewHook({
      sessionId: "session-1",
      store: store as never,
      emit: (event) => events.push(event),
      config: parseSecurityAgentConfig(JSON.stringify({ review_mode: "warn" })),
      runSecurityAgent: async () => {
        throw new Error("Claude Code process exited with code 1");
      },
    });

    const response = await hook(
      {
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_use_id: "tool-1",
        tool_input: { command: "web3 security check --transaction-id txi_123" },
        tool_response: {
          stdout: JSON.stringify({
            transaction_id: "txi_123",
            status: "security_review_required",
            verdict: "warn",
            checks: [
              {
                rule: "usd_value_conservation",
                status: "warn",
                detail: "missing price data",
              },
            ],
          }),
        },
      } as never,
      undefined,
      { signal: new AbortController().signal },
    );

    const blockEvents = events.filter((event) => event.type === "block_update");
    expect(blockEvents).toHaveLength(2);
    expect(blockEvents[1].block.type).toBe("security_review");
    expect(blockEvents[1].block.status).toBe("unsafe");
    const unsafeBlock = blockEvents[1].block;
    if (unsafeBlock.type !== "security_review") throw new Error("expected security review block");
    expect(unsafeBlock.markdown).toContain("review failed");
    const systemMessage =
      "systemMessage" in response ? response.systemMessage : "";
    expect(systemMessage).toContain("decision=stop");
    expect(systemMessage).toContain("Security Agent review failed");
    expect(systemMessage).toContain("Do not call web3 ssm sign");
  });
});

describe("resolvePersistedOutput", () => {
  test("returns plain string unchanged", async () => {
    const input = '{"transaction_id":"txi_1","verdict":"pass"}';
    expect(await resolvePersistedOutput(input)).toBe(input);
  });

  test("returns non-string values unchanged", async () => {
    const input = { stdout: '{"verdict":"pass"}' };
    expect(await resolvePersistedOutput(input)).toBe(input);
  });

  test("reads persisted-output file and returns its content", async () => {
    const dir = await mkdtemp(join(tmpdir(), "web3-test-"));
    const filePath = join(dir, "output.txt");
    const expected = '{"transaction_id":"txi_99","verdict":"fail","checks":[],"simulation":null}';
    await writeFileFn(filePath, expected, "utf8");

    const persistedOutput = [
      "<persisted-output>",
      `Output too large (1KB). Full output saved to: ${filePath}`,
      "",
      "Preview (first 2KB):",
      '{"transaction_id":"txi_99",...',
      "</persisted-output>",
    ].join("\n");

    const result = await resolvePersistedOutput(persistedOutput);
    expect(result).toBe(expected);

    await rm(dir, { recursive: true });
  });

  test("returns original value when persisted-output file is missing", async () => {
    const persistedOutput = [
      "<persisted-output>",
      "Output too large. Full output saved to: /nonexistent/path/file.txt",
      "</persisted-output>",
    ].join("\n");

    expect(await resolvePersistedOutput(persistedOutput)).toBe(persistedOutput);
  });

  test("parseSecurityCheckFromTool works after resolving persisted output", async () => {
    const dir = await mkdtemp(join(tmpdir(), "web3-test-"));
    const filePath = join(dir, "output.txt");
    const json = JSON.stringify({
      transaction_id: "txi_persisted",
      status: "security_review_required",
      verdict: "pass",
      summary: "verdict=pass",
      checks: [{ rule: "transaction_shape", status: "pass", detail: "ok" }],
    });
    await writeFileFn(filePath, json, "utf8");

    const persistedOutput = [
      "<persisted-output>",
      `Output too large (33KB). Full output saved to: ${filePath}`,
      "",
      "Preview (first 2KB):",
      json.slice(0, 100),
      "</persisted-output>",
    ].join("\n");

    const resolved = await resolvePersistedOutput(persistedOutput);
    const check = parseSecurityCheckFromTool(
      { command: "web3 security check --transaction-id txi_persisted" },
      resolved,
    );

    expect(check?.transactionId).toBe("txi_persisted");
    expect(check?.verdict).toBe("pass");

    await rm(dir, { recursive: true });
  });
});

class MemoryStore {
  messages = new Map<string, ChatMessage[]>();

  listMessages(sessionId: string) {
    return this.messages.get(sessionId) ?? [];
  }

  addMessage(sessionId: string, message: ChatMessage) {
    const messages = this.messages.get(sessionId) ?? [];
    const next = messages.some((item) => item.id === message.id)
      ? messages.map((item) => (item.id === message.id ? message : item))
      : [...messages, message];
    this.messages.set(sessionId, next);
    return message;
  }
}
