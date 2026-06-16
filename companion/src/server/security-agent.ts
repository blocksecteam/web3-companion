// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { query } from "@anthropic-ai/claude-agent-sdk";
import type { Options, SDKAssistantMessage } from "@anthropic-ai/claude-agent-sdk";
import type { SecurityAgentReview, SecurityCheckSummary, SessionLanguage } from "./security-review-hook";

export type StructuredSecurityReview = SecurityAgentReview & {
  evidence?: string[];
  blockingReasons?: string[];
};

export async function runSecurityAgent({
  check,
  signal,
  sessionLanguage,
}: {
  check: SecurityCheckSummary;
  signal: AbortSignal;
  sessionLanguage: SessionLanguage;
}): Promise<SecurityAgentReview> {
  const result = query(await buildSecurityAgentQuery(check, signal, sessionLanguage));

  try {
    let text = "";
    for await (const message of result) {
      if (signal.aborted) break;
      if (message.type === "assistant") {
        text = assistantText(message as SDKAssistantMessage) || text;
      }
      if (message.type === "result" && (message as { is_error?: boolean }).is_error) {
        throw new Error((message as { errors?: string[] }).errors?.join("\n") || "Web3 Security Agent failed.");
      }
    }
    return parseSecurityAgentOutput(text);
  } finally {
    result.close();
  }
}

async function buildSecurityAgentQuery(
  check: SecurityCheckSummary,
  signal: AbortSignal,
  sessionLanguage: SessionLanguage = "en",
) {
  const options: Options = {
    cwd: resolveAgentWorkdir(),
    sessionId: crypto.randomUUID(),
    persistSession: false,
    includePartialMessages: false,
    maxTurns: 2,
    settingSources: ["user", "project"],
    tools: { type: "preset", preset: "claude_code" },
    allowedTools: ["Bash(web3 security trace *)"],
    disallowedTools: ["Read", "Grep", "Glob", "Write", "Edit", "MultiEdit"],
    permissionMode: "bypassPermissions",
    systemPrompt: await readSecurityPrompt(),
    pathToClaudeCodeExecutable: process.env.CLAUDE_CODE_EXECUTABLE
      ? resolve(process.env.CLAUDE_CODE_EXECUTABLE)
      : undefined,
    canUseTool: async (toolName, input) => {
      if (toolName !== "Bash") {
        return { behavior: "deny", message: `${toolName} is not available to Security Agent.` };
      }
      const command = normalizeShellCommandForMatch(typeof input.command === "string" ? input.command : "");
      if (/^web3 security trace --transaction-id [a-zA-Z0-9_-]+$/.test(command)) {
        return { behavior: "allow" };
      }
      return { behavior: "deny", message: "Security Agent may only run: web3 security trace --transaction-id <id>" };
    },
    env: buildSecurityClaudeEnv(),
    abortController: abortControllerFromSignal(signal),
  };
  return {
    prompt: buildSecurityAgentPrompt(check, sessionLanguage),
    options,
  };
}

export async function buildSecurityAgentQueryForTest(
  check: SecurityCheckSummary,
  signal: AbortSignal,
  sessionLanguage: SessionLanguage = "en",
) {
  return buildSecurityAgentQuery(check, signal, sessionLanguage);
}

export function parseSecurityAgentOutput(output: string): SecurityAgentReview {
  const markdown = taggedContent(output, "security_review_markdown");
  const resultJSON = taggedContent(output, "security_review_result");
  const parsed = parseStructuredReview(resultJSON) ?? parseStructuredReview(output);
  if (!parsed) {
    return {
      verdict: "unsafe",
      confidence: "low",
      reason: "Web3 Security Agent returned an unreadable result.",
      markdown: [
        "### Web3 Security Agent Review: Unsafe",
        "",
        "**Conclusion**",
        "Web3 Security Agent returned an unreadable result, so this transaction is not safe to sign.",
      ].join("\n"),
    };
  }
  return {
    verdict: parsed.verdict,
    confidence: parsed.confidence,
    reason: parsed.reason,
    markdown: markdown || securityReviewToMarkdown(parsed),
  };
}

export function securityReviewToMarkdown(review: StructuredSecurityReview) {
  const titleVerdict = review.verdict === "safe" ? "Safe" : "Unsafe";
  const lines = [
    `### Web3 Security Agent Review: ${titleVerdict}`,
    "",
    `**Confidence:** ${capitalize(review.confidence ?? "low")}`,
    "",
    "**Conclusion**",
    review.verdict === "safe" ? "This transaction appears safe to continue." : "Do not sign this transaction yet.",
    "",
    "**Reason**",
    review.reason || "No reason was provided.",
  ];
  if (review.evidence && review.evidence.length > 0) {
    lines.push("", "**Evidence**", ...review.evidence.map((item) => `- ${item}`));
  }
  if (review.blockingReasons && review.blockingReasons.length > 0) {
    lines.push("", "**Blocking reasons**", ...review.blockingReasons.map((item) => `- ${item}`));
  }
  return lines.join("\n");
}

function buildSecurityAgentPrompt(check: SecurityCheckSummary, sessionLanguage: SessionLanguage) {
  const payload: Record<string, unknown> = {
    transaction_id: check.transactionId,
    status: check.status,
    tx_type: check.txType,
    tx_summary: check.txSummary,
    verdict: check.verdict,
    summary: check.summary,
    checks: check.checks,
    effect: check.effect,
    simulation: check.simulation,
  };
  const languageName = sessionLanguage === "zh" ? "Chinese" : "English";
  return [
    `Session language: ${languageName}.`,
    `Write the user-facing Markdown section in ${languageName}.`,
    "Keep the JSON section keys and enum values exactly as specified.",
    "",
    "Review this server-produced transaction security payload.",
    "Return the required markdown and JSON tags exactly as instructed.",
    "",
    JSON.stringify(payload, null, 2),
  ].join("\n");
}

function normalizeShellCommandForMatch(command: string) {
  return command.replace(/\s+/g, " ").trim().replace(/\s+2>\s*&\s*1\s*$/, "");
}

async function readSecurityPrompt() {
  try {
    return await readFile(resolve(resolveBackendRoot(), "companion/security.md"), "utf8");
  } catch {
    return [
      "You are Web3 Security Agent.",
      "Review deterministic checks, transaction trace, address labels, and balance changes.",
      "If evidence is incomplete, return unsafe.",
    ].join("\n");
  }
}

function taggedContent(output: string, tag: string) {
  const pattern = new RegExp(`<${tag}>\\s*([\\s\\S]*?)\\s*</${tag}>`, "i");
  return output.match(pattern)?.[1]?.trim() ?? "";
}

function parseStructuredReview(raw: string): StructuredSecurityReview | null {
  if (!raw.trim()) return null;
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!isRecord(parsed)) return null;
    const verdict = parsed.verdict === "safe" ? "safe" : parsed.verdict === "unsafe" ? "unsafe" : "";
    if (!verdict) return null;
    const confidence = parsed.confidence === "high" || parsed.confidence === "medium" ? parsed.confidence : "low";
    if (verdict === "safe" && confidence === "low") {
      return {
        verdict: "unsafe",
        confidence: "low",
        reason: "Web3 Security Agent returned safe with low confidence, so the review is treated as unsafe.",
        evidence: stringArray(parsed.evidence),
        blockingReasons: ["Safe verdict with low confidence"],
      };
    }
    return {
      verdict,
      confidence,
      reason: typeof parsed.reason === "string" ? parsed.reason : "",
      evidence: stringArray(parsed.evidence),
      blockingReasons: stringArray(parsed.blockingReasons),
    };
  } catch {
    return null;
  }
}

function assistantText(message: SDKAssistantMessage) {
  const sdkMessage: Record<string, unknown> = isRecord(message.message) ? message.message : {};
  const contentValue = sdkMessage["content"];
  const content = Array.isArray(contentValue) ? contentValue : [];
  return content
    .map((block: unknown) => (isRecord(block) && typeof block["text"] === "string" ? block["text"] : ""))
    .join("");
}

function stringArray(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function capitalize(value: string) {
  return value ? `${value.slice(0, 1).toUpperCase()}${value.slice(1)}` : value;
}

function resolveBackendRoot(env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.WEB3_BACKEND_ROOT ?? ".");
}

function resolveAgentWorkdir(env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.WEB3_AGENT_WORKDIR ?? ".");
}

function resolveClaudeConfigDir(env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.CLAUDE_CONFIG_DIR ?? "dist/home");
}

function buildSecurityClaudeEnv() {
  const env: Record<string, string | undefined> = {
    CLAUDE_CONFIG_DIR: resolveClaudeConfigDir(),
    HOME: process.env.HOME,
    USER: process.env.USER,
    LOGNAME: process.env.LOGNAME,
    TMPDIR: process.env.TMPDIR,
    PATH: process.env.PATH,
    WEB3_API_URL: process.env.WEB3_API_URL,
    WEB3_COMPANION_ROOT: resolve("."),
  };
  delete env.ANTHROPIC_SMALL_FAST_MODEL;
  return env;
}

function abortControllerFromSignal(signal: AbortSignal) {
  const controller = new AbortController();
  if (signal.aborted) {
    controller.abort();
  } else {
    signal.addEventListener("abort", () => controller.abort(), { once: true });
  }
  return controller;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
