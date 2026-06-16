// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";

export function normalizeSDKMessageForArchive(
  sessionId: string,
  message: SDKMessage,
  fallbackUUID: string,
): SDKMessage {
  const record = message as SDKMessage & { uuid?: string; session_id?: string };
  return {
    ...message,
    uuid: typeof record.uuid === "string" && record.uuid.trim() ? record.uuid : fallbackUUID,
    session_id:
      typeof record.session_id === "string" && record.session_id.trim() ? record.session_id : sessionId,
  } as SDKMessage;
}

export function shouldArchiveSDKMessage(message: SDKMessage) {
  const type = (message as { type?: unknown }).type;
  return typeof type === "string" && type !== "stream_event";
}

export function sdkMessageUUID(message: SDKMessage) {
  const uuid = (message as { uuid?: unknown }).uuid;
  return typeof uuid === "string" && uuid.trim() ? uuid : "";
}

export function sdkMessageType(message: SDKMessage) {
  const type = (message as { type?: unknown }).type;
  return typeof type === "string" && type.trim() ? type : "unknown";
}

export function sdkParentToolUseID(message: SDKMessage) {
  const parentToolUseID = (message as { parent_tool_use_id?: unknown }).parent_tool_use_id;
  return typeof parentToolUseID === "string" && parentToolUseID.trim() ? parentToolUseID : null;
}
