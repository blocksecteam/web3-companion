// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { HookCallback, PostToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";
import { nanoid } from "nanoid";
import type { ChatMessage, ServerWsEvent } from "../types/chat";
import type { SessionStore } from "./session-store";

type Emit = (event: ServerWsEvent) => void;
export type SessionLanguage = "zh" | "en";

export type SecurityAgentConfig = {
  reviewMode: "warn" | "always";
  allowReviewClear: boolean;
};

export type SecurityCheckSummary = {
  transactionId: string;
  status?: string;
  txType?: string;
  txSummary?: Record<string, unknown>;
  verdict: "pass" | "warn" | "fail" | string;
  summary?: string;
  checks: Array<{ rule?: string; status?: string; detail?: string; observed?: unknown }>;
  effect?: unknown;
  simulation?: unknown;
  raw: Record<string, unknown>;
};

export type SecurityAgentReview = {
  verdict: "safe" | "unsafe";
  confidence?: "high" | "medium" | "low";
  reason: string;
  markdown?: string;
  reviewFailed?: boolean;
};

type RunSecurityAgent = (
  check: SecurityCheckSummary,
  options: { signal: AbortSignal; sessionLanguage: SessionLanguage },
) => Promise<SecurityAgentReview>;

type ClearSecurityReview = (
  check: SecurityCheckSummary,
  review: SecurityAgentReview,
  options: { signal: AbortSignal },
) => Promise<void>;

export function parseSecurityAgentConfig(raw: string | undefined): SecurityAgentConfig {
  const parsed = parseRecord(raw);
  const reviewMode = stringField(parsed.review_mode);
  return {
    reviewMode: reviewMode === "warn" || reviewMode === "warn_only" ? "warn" : "always",
    allowReviewClear: parsed.allow_review_clear !== false,
  };
}

export function parseSecurityCheckFromTool(toolInput: unknown, toolResponse: unknown): SecurityCheckSummary | null {
  const command = normalizeShellCommandForMatch(stringField(recordField(toolInput, "command")));
  if (!isSecurityCheckCommand(command)) return null;
  const body = parseToolResponseJSON(toolResponse);
  if (!body) return null;
  const transactionId = stringField(body.transaction_id);
  const verdict = stringField(body.verdict);
  if (!transactionId || !verdict) return null;
  return {
    transactionId,
    status: stringField(body.status) || undefined,
    txType: stringField(body.tx_type) || undefined,
    txSummary: parseTxSummary(body.tx_summary),
    verdict,
    summary: stringField(body.summary) || undefined,
    checks: Array.isArray(body.checks) ? body.checks.filter(isRecord) : [],
    effect: body.effect,
    simulation: body.simulation,
    raw: body,
  };
}

export function shouldReviewSecurityCheck(check: SecurityCheckSummary, config: SecurityAgentConfig) {
  if (config.reviewMode === "always") return ["pass", "warn", "fail"].includes(check.verdict);
  return ["warn", "fail"].includes(check.verdict);
}

export function securityReviewSystemMessage(
  check: SecurityCheckSummary,
  review: SecurityAgentReview,
  config: SecurityAgentConfig,
) {
  const decision = signingDecision(check, review, config);
  return [
    `Web3 Security Agent review completed for ${check.transactionId}.`,
    `static_verdict=${check.verdict}`,
    `verdict=${review.verdict}`,
    `confidence=${review.confidence ?? "low"}`,
    `decision=${decision}`,
    review.reason ? `reason=${review.reason}` : "",
    decision === "passkey_required"
      ? "You may continue to web3 ssm sign. This is not automatic signing; backend policy checks and Passkey approval still apply."
      : decision === "stop"
        ? "Do not call web3 ssm sign. Treat the transaction as unsafe and explain the failing review reason."
        : "Do not call web3 ssm sign yet. Ask the user to explicitly confirm in a new message. After the user confirms, run web3 security override --transaction-id <transaction_id> --reason \"<quote the user's confirmation as the reason>\". If it returns status=pending_user_auth, wait for the user's Passkey approval and then retry with the same reason plus --challenge-id <challenge_id>. Proceed to web3 ssm sign only if the override response returns status=security_overridden. Do not re-run web3 security check.",
  ].filter(Boolean).join("\n");
}

export async function resolvePersistedOutput(value: unknown): Promise<unknown> {
  if (typeof value !== "string") return value;
  if (!value.includes("<persisted-output>")) return value;
  const match = value.match(/\bOutput too large\b[^\n]*\bFull output saved to: ([^\n]+)/);
  if (!match) return value;
  const filePath = match[1].trim();
  try {
    return await readFile(filePath, "utf8");
  } catch {
    return value;
  }
}

export function createSecurityReviewHook({
  sessionId,
  emit,
  store,
  config = parseSecurityAgentConfig(process.env.WEB3_SECURITY_AGENT_CONFIG),
  runSecurityAgent,
  clearSecurityReview = clearBackendSecurityReview,
}: {
  sessionId: string;
  emit: Emit;
  store: SessionStore;
  config?: SecurityAgentConfig;
  runSecurityAgent: RunSecurityAgent;
  clearSecurityReview?: ClearSecurityReview;
}): HookCallback {
  return async (input, _toolUseID, { signal }) => {
    if (!isPostToolUse(input) || input.tool_name !== "Bash") return { continue: true };
    const toolResponse = await resolvePersistedOutput(input.tool_response);
    const check = parseSecurityCheckFromTool(input.tool_input, toolResponse);
    if (!check || !shouldReviewSecurityCheck(check, config)) return { continue: true };
    const sessionLanguage = inferSessionLanguage(store.listMessages(sessionId));

    const messageId = `security-review-${nanoid()}`;
    const blockId = `security-${nanoid()}`;
    emitSecurityBlock(sessionId, messageId, store, emit, {
      id: blockId,
      type: "security_review",
      status: "reviewing",
      transactionId: check.transactionId,
      markdown: securityReviewingMarkdown(sessionLanguage),
    });

    let review: SecurityAgentReview;
    try {
      review = await runSecurityAgent(check, { signal, sessionLanguage });
    } catch (error) {
      review = {
        verdict: "unsafe",
        confidence: "high",
        reason: formatSecurityReviewFailure(error),
        markdown: securityReviewFailedMarkdown(sessionLanguage),
        reviewFailed: true,
      };
    }

    const decision = signingDecision(check, review, config);
    if (decision === "passkey_required") {
      try {
        await clearSecurityReview(check, review, { signal });
      } catch (error) {
        review = {
          verdict: "unsafe",
          confidence: "high",
          reason: formatSecurityClearFailure(error),
          markdown: securityClearFailedMarkdown(sessionLanguage),
          reviewFailed: true,
        };
      }
    }

    emitSecurityBlock(sessionId, messageId, store, emit, {
      id: blockId,
      type: "security_review",
      status: review.verdict === "safe" ? "safe" : "unsafe",
      transactionId: check.transactionId,
      markdown: review.markdown || review.reason,
    });

    return {
      continue: true,
      systemMessage: securityReviewSystemMessage(check, review, config),
    };
  };
}

export function inferSessionLanguage(messages: ChatMessage[]): SessionLanguage {
  for (const message of messages) {
    if (message.role !== "user") continue;
    const content = message.content.trim();
    if (!content) continue;
    return hasCJK(content) ? "zh" : "en";
  }
  return "en";
}

function hasCJK(value: string) {
  return /[\u3400-\u9fff]/u.test(value);
}

function securityReviewingMarkdown(language: SessionLanguage) {
  return language === "zh"
    ? "Web3 Security Agent 正在审查交易安全性"
    : "Web3 Security Agent is reviewing transaction safety";
}

function securityReviewFailedMarkdown(language: SessionLanguage) {
  if (language === "zh") {
    return [
      "### Web3 Security Agent Review: Unsafe",
      "",
      "**结论**",
      "Web3 Security Agent 审查失败，因此这笔交易现在不能安全签名。",
    ].join("\n");
  }
  return [
    "### Web3 Security Agent Review: Unsafe",
    "",
    "**Conclusion**",
    "Web3 Security Agent review failed, so this transaction is not safe to sign.",
  ].join("\n");
}

function securityClearFailedMarkdown(language: SessionLanguage) {
  if (language === "zh") {
    return [
      "### Web3 Security Agent Review: Unsafe",
      "",
      "**结论**",
      "Web3 Security Agent 判断交易安全，但后端安全审查清除失败。不要签名这笔交易。",
    ].join("\n");
  }
  return [
    "### Web3 Security Agent Review: Unsafe",
    "",
    "**Conclusion**",
    "Web3 Security Agent marked the transaction safe, but backend security review clearance failed. Do not sign this transaction.",
  ].join("\n");
}

function emitSecurityBlock(
  sessionId: string,
  messageId: string,
  store: SessionStore,
  emit: Emit,
  block: NonNullable<ChatMessage["blocks"]>[number],
) {
  const existing = store.listMessages(sessionId).find((message) => message.id === messageId);
  const message: ChatMessage = {
    id: messageId,
    role: "assistant",
    content: "",
    createdAt: existing?.createdAt ?? new Date().toISOString(),
    partial: block.status === "reviewing" ? true : undefined,
    blocks: existing?.blocks?.some((item) => item.id === block.id)
      ? existing.blocks.map((item) => (item.id === block.id ? block : item))
      : [block],
  };
  const saved = store.addMessage(sessionId, message);
  const updated = saved.blocks?.find((item) => item.id === block.id) ?? block;
  emit({ type: "block_update", sessionId, messageId, block: updated });
}

function signingDecision(
  check: SecurityCheckSummary,
  review: SecurityAgentReview,
  config: SecurityAgentConfig,
) {
  if (review.reviewFailed) return "stop";
  if (review.verdict === "safe" && review.confidence === "low") return "wait_user_confirm";
  if (review.verdict !== "safe") {
    return review.confidence === "high" || review.confidence === "medium" ? "stop" : "wait_user_confirm";
  }
  if (review.confidence !== "high" && review.confidence !== "medium") return "wait_user_confirm";
  if (!config.allowReviewClear) return "wait_user_confirm";
  return "passkey_required";
}

function formatSecurityReviewFailure(error: unknown) {
  const detail = error instanceof Error ? error.message.trim() : "";
  return detail ? `Security Agent review failed: ${detail}` : "Security Agent review failed.";
}

function formatSecurityClearFailure(error: unknown) {
  const detail = error instanceof Error ? error.message.trim() : "";
  return detail ? `Security review clearance failed: ${detail}` : "Security review clearance failed.";
}

async function clearBackendSecurityReview(
  check: SecurityCheckSummary,
  review: SecurityAgentReview,
  { signal }: { signal: AbortSignal },
) {
  const apiURL = (process.env.WEB3_API_URL || "http://localhost:8080").replace(/\/+$/, "");
  const accessToken = await mintContainerAccessToken(apiURL, signal);
  const response = await fetch(`${apiURL}/api/v1/security/clear-review`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Authorization": `Bearer ${accessToken}`,
    },
    body: JSON.stringify({
      transaction_id: check.transactionId,
      reviewer: "web3_security_agent",
      decision: review.verdict,
      confidence: review.confidence ?? "low",
      reason: review.reason,
    }),
    signal,
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(`API error (status ${response.status}): ${detail}`);
  }
}

type ContainerCredential = {
  credential_id?: string;
  secret?: string;
};

async function mintContainerAccessToken(apiURL: string, signal: AbortSignal) {
  const credential = await readContainerCredential();
  const response = await fetch(`${apiURL}/auth/container-token`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      credential_id: credential.credential_id,
      secret: credential.secret,
    }),
    signal,
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(`container token failed (status ${response.status}): ${detail}`);
  }
  const body = await response.json() as { access_token?: unknown };
  if (typeof body.access_token !== "string" || !body.access_token.trim()) {
    throw new Error("container token response did not include access_token");
  }
  return body.access_token.trim();
}

async function readContainerCredential() {
  const home = process.env.HOME || "";
  if (!home) throw new Error("HOME is not set; cannot read container credential");
  const raw = await readFile(join(home, ".local", "web3", "container-credential.json"), "utf8");
  const credential = JSON.parse(raw) as ContainerCredential;
  if (!credential.credential_id?.trim() || !credential.secret?.trim()) {
    throw new Error("container credential missing credential_id or secret");
  }
  return {
    credential_id: credential.credential_id.trim(),
    secret: credential.secret.trim(),
  };
}

function isSecurityCheckCommand(command: string) {
  return command.startsWith("web3 security check ");
}

function parseToolResponseJSON(value: unknown): Record<string, unknown> | null {
  if (isRecord(value)) {
    for (const key of ["stdout", "output", "content"]) {
      const parsed = parseRecord(stringField(value[key]));
      if (Object.keys(parsed).length > 0) return parsed;
    }
    return value;
  }
  const parsed = parseRecord(stringField(value));
  return Object.keys(parsed).length > 0 ? parsed : null;
}

function parseRecord(raw: string | undefined) {
  if (!raw) return {};
  for (const candidate of recordCandidates(raw)) {
    try {
      const parsed = JSON.parse(candidate) as unknown;
      if (isRecord(parsed)) return parsed;
    } catch {
      continue;
    }
  }
  return {};
}

function normalizeShellCommandForMatch(command: string) {
  return command.replace(/\s+/g, " ").trim().replace(/\s+2>\s*&\s*1\s*$/, "");
}

function recordCandidates(raw: string) {
  const trimmed = raw.trim();
  const candidates = [trimmed];
  candidates.push(...jsonObjectCandidates(trimmed));
  return candidates;
}

function jsonObjectCandidates(raw: string) {
  const candidates: string[] = [];
  for (let start = raw.indexOf("{"); start >= 0; start = raw.indexOf("{", start + 1)) {
    let depth = 0;
    let inString = false;
    let escaped = false;
    for (let index = start; index < raw.length; index += 1) {
      const char = raw[index];
      if (escaped) {
        escaped = false;
        continue;
      }
      if (char === "\\") {
        escaped = inString;
        continue;
      }
      if (char === "\"") {
        inString = !inString;
        continue;
      }
      if (inString) continue;
      if (char === "{") depth += 1;
      if (char === "}") {
        depth -= 1;
        if (depth === 0) {
          candidates.push(raw.slice(start, index + 1));
          break;
        }
      }
    }
  }
  return candidates;
}

function recordField(value: unknown, key: string) {
  return isRecord(value) ? value[key] : undefined;
}

function isPostToolUse(input: unknown): input is PostToolUseHookInput {
  return isRecord(input) && input.hook_event_name === "PostToolUse";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function stringField(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function parseTxSummary(value: unknown): Record<string, unknown> | undefined {
  if (isRecord(value)) return value;
  const parsed = parseRecord(stringField(value));
  return Object.keys(parsed).length > 0 ? parsed : undefined;
}
