// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { buildSecurityAgentQueryForTest, parseSecurityAgentOutput, securityReviewToMarkdown } from "./security-agent";

describe("security agent output parsing", () => {
  test("uses a valid UUID session id for Claude Code subprocesses", async () => {
    const query = await buildSecurityAgentQueryForTest({
      transactionId: "txi_123",
      verdict: "warn",
      checks: [],
      raw: {},
    } as never, new AbortController().signal);

    const sessionId = query.options.sessionId ?? "";
    expect(sessionId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
    expect(sessionId.startsWith("security-")).toBe(false);
  });

  test("passes session language instructions to the security agent prompt", async () => {
    const query = await buildSecurityAgentQueryForTest({
      transactionId: "txi_123",
      verdict: "warn",
      checks: [],
      raw: {},
    } as never, new AbortController().signal, "zh");

    expect(query.prompt).toContain("Session language: Chinese");
    expect(query.prompt).toContain("Write the user-facing Markdown section in Chinese.");
    expect(query.prompt).toContain("Keep the JSON section keys and enum values exactly as specified.");
  });

  test("parses markdown and structured result tags", () => {
    const parsed = parseSecurityAgentOutput(`
<security_review_markdown>
### Web3 Security Agent Review: Safe

**Reason**
Trace and balances match the expected swap.
</security_review_markdown>

<security_review_result>
{"verdict":"safe","confidence":"high","reason":"matched","evidence":["trace"],"blockingReasons":[]}
</security_review_result>
`);

    expect(parsed.verdict).toBe("safe");
    expect(parsed.confidence).toBe("high");
    expect(parsed.reason).toBe("matched");
    expect(parsed.markdown).toContain("Web3 Security Agent Review: Safe");
  });

  test("unreadable output becomes unsafe markdown", () => {
    const parsed = parseSecurityAgentOutput("not-json");

    expect(parsed.verdict).toBe("unsafe");
    expect(parsed.confidence).toBe("low");
    expect(parsed.markdown).toContain("Web3 Security Agent Review: Unsafe");
  });

  test("formats structured review as markdown when markdown tag is absent", () => {
    const markdown = securityReviewToMarkdown({
      verdict: "unsafe",
      confidence: "medium",
      reason: "unexpected outflow",
      evidence: ["Owner spends 100 USDC"],
      blockingReasons: ["No matching inflow"],
    });

    expect(markdown).toContain("### Web3 Security Agent Review: Unsafe");
    expect(markdown).toContain("Owner spends 100 USDC");
    expect(markdown).toContain("No matching inflow");
  });

  test("treats safe output with low confidence as unsafe", () => {
    const parsed = parseSecurityAgentOutput(`
<security_review_result>
{"verdict":"safe","confidence":"low","reason":"uncertain","evidence":["price coverage missing"],"blockingReasons":[]}
</security_review_result>
`);

    expect(parsed.verdict).toBe("unsafe");
    expect(parsed.confidence).toBe("low");
    expect(parsed.reason).toContain("treated as unsafe");
    expect(parsed.markdown).toContain("### Web3 Security Agent Review: Unsafe");
  });
});

describe("security agent query options", () => {
  test("maxTurns is 2 to allow one tool call cycle", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    expect(query.options.maxTurns).toBe(2);
  });

  test("Bash is not in disallowedTools", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const disallowed = query.options.disallowedTools ?? [];
    expect(disallowed).not.toContain("Bash");
  });

  test("allowedTools includes Bash scoped to web3 security trace", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const allowed = query.options.allowedTools ?? [];
    expect(allowed).toContain("Bash(web3 security trace *)");
  });

  test("canUseTool allows web3 security trace command", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const canUse = query.options.canUseTool;
    if (!canUse) throw new Error("canUseTool not set");
    const result = await canUse("Bash", { command: "web3 security trace --transaction-id txi_123" }, {} as never);
    expect(result).toEqual({ behavior: "allow" });
  });

  test("canUseTool allows security trace with trailing stderr redirection", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const canUse = query.options.canUseTool;
    if (!canUse) throw new Error("canUseTool not set");
    const result = await canUse("Bash", { command: "web3 security trace --transaction-id txi_123 2>&1" }, {} as never);
    expect(result).toEqual({ behavior: "allow" });
  });

  test("canUseTool still denies commands with extra shell operators", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const canUse = query.options.canUseTool;
    if (!canUse) throw new Error("canUseTool not set");
    const result = await canUse("Bash", { command: "web3 security trace --transaction-id txi_123 2>&1; web3 context status" }, {} as never);
    expect((result as { behavior: string }).behavior).toBe("deny");
  });

  test("canUseTool denies other Bash commands", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const canUse = query.options.canUseTool;
    if (!canUse) throw new Error("canUseTool not set");
    const result = await canUse("Bash", { command: "web3 security check --transaction-id txi_123" }, {} as never);
    expect((result as { behavior: string }).behavior).toBe("deny");
  });

  test("canUseTool denies non-Bash tools", async () => {
    const query = await buildSecurityAgentQueryForTest(
      { transactionId: "txi_123", verdict: "pass", checks: [], raw: {} } as never,
      new AbortController().signal,
    );
    const canUse = query.options.canUseTool;
    if (!canUse) throw new Error("canUseTool not set");
    const result = await canUse("Read", { file_path: "/etc/passwd" }, {} as never);
    expect((result as { behavior: string }).behavior).toBe("deny");
  });
});
