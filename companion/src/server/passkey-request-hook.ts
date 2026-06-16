// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { HookCallback, PostToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";
import type { ChatMessage, PasskeyRequestBlock, ServerWsEvent } from "../types/chat";
import type { SessionStore } from "./session-store";

type Emit = (event: ServerWsEvent) => void;

export type ParsedPasskeyRequest = {
  operation: "sign" | "delegate" | "security_override" | string;
  challengeId: string;
  signUrl: string;
  transactionId?: string;
  summary?: Record<string, unknown>;
};

export function createPasskeyRequestHook({
  sessionId,
  store,
  emit,
}: {
  sessionId: string;
  store: SessionStore;
  emit: Emit;
}): HookCallback {
  return async (input) => {
    if (!isPostToolUse(input) || input.tool_name !== "Bash") return { continue: true };
    const request = parsePasskeyRequestFromTool(input.tool_input, input.tool_response);
    if (!request) return { continue: true };

    const block: PasskeyRequestBlock = {
      id: blockId(request.challengeId),
      type: "passkey_request",
      status: "pending",
      operation: request.operation,
      challengeId: request.challengeId,
      signUrl: request.signUrl,
      ...(request.transactionId ? { transactionId: request.transactionId } : {}),
      ...(request.summary ? { summary: request.summary } : {}),
    };
    emitPasskeyBlock(sessionId, messageId(request.challengeId), store, emit, block);
    return { continue: true };
  };
}

export function parsePasskeyRequestFromTool(toolInput: unknown, toolResponse: unknown): ParsedPasskeyRequest | null {
  const command = normalizeShellCommandForMatch(stringField(recordField(toolInput, "command")));
  const operation = passkeyOperationFromCommand(command);
  if (!operation) return null;

  const root = parseToolResponseJSON(toolResponse);
  if (!root) return null;
  const body = isRecord(root.response) ? root.response : root;
  const status = stringField(body.status);
  if (status !== "pending_user_auth" && status !== "pending_delegation") return null;

  const challengeId = stringField(body.challenge_id);
  const signUrl = stringField(body.sign_url);
  if (!challengeId || !signUrl) return null;

  return {
    operation,
    challengeId,
    signUrl,
    transactionId: stringField(body.transaction_id) || undefined,
    summary: recordOrUndefined(body.tx_summary),
  };
}

function emitPasskeyBlock(
  sessionId: string,
  messageIdValue: string,
  store: SessionStore,
  emit: Emit,
  block: PasskeyRequestBlock,
) {
  const existing = store.listMessages(sessionId).find((message) => message.id === messageIdValue);
  const nextBlocks = existing?.blocks?.some((item) => item.id === block.id)
    ? existing.blocks.map((item) => (item.id === block.id ? block : item))
    : [...(existing?.blocks ?? []), block];
  const message: ChatMessage = {
    id: messageIdValue,
    role: "assistant",
    content: "",
    createdAt: existing?.createdAt ?? new Date().toISOString(),
    blocks: nextBlocks,
  };
  const saved = store.addMessage(sessionId, message);
  const savedBlock = saved.blocks?.find((item) => item.id === block.id) ?? block;
  emit({ type: "block_update", sessionId, messageId: messageIdValue, block: savedBlock });
}

function passkeyOperationFromCommand(command: string): "sign" | "delegate" | "security_override" | "" {
  if (command.startsWith("web3 ssm sign ")) return "sign";
  // Match `delegate <args>` but NOT `delegate-estimate <args>`. The
  // trailing-space prefix check already distinguishes them (delegate is
  // followed by a space, delegate-estimate by a dash), but the explicit
  // negative guard makes the intent obvious to a reader and resists
  // future renames.
  if (command.startsWith("web3 ssm delegate ") && !command.startsWith("web3 ssm delegate-")) return "delegate";
  if (command.startsWith("web3 security override ")) return "security_override";
  return "";
}

function parseToolResponseJSON(value: unknown): Record<string, unknown> | null {
  if (isRecord(value)) {
    for (const key of ["stdout", "output", "content"]) {
      const parsed = parseRecordFromString(stringField(value[key]));
      if (parsed) return parsed;
    }
    return value;
  }
  return parseRecordFromString(stringField(value));
}

function normalizeShellCommandForMatch(command: string) {
  return command.replace(/\s+/g, " ").trim().replace(/\s+2>\s*&\s*1\s*$/, "");
}

function parseRecordFromString(raw: string) {
  if (!raw) return null;
  const direct = tryParseRecord(raw);
  if (direct) return direct;
  const firstBrace = raw.indexOf("{");
  const lastBrace = raw.lastIndexOf("}");
  if (firstBrace < 0 || lastBrace <= firstBrace) return null;
  return tryParseRecord(raw.slice(firstBrace, lastBrace + 1));
}

function tryParseRecord(raw: string) {
  try {
    const parsed = JSON.parse(raw) as unknown;
    return isRecord(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function messageId(challengeId: string) {
  return `passkey-request-${challengeId}`;
}

function blockId(challengeId: string) {
  return `passkey-${challengeId}`;
}

function recordField(value: unknown, key: string) {
  return isRecord(value) ? value[key] : undefined;
}

function recordOrUndefined(value: unknown) {
  return isRecord(value) ? value : undefined;
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
