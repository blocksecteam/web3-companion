// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type {
  SDKAssistantMessage,
  SDKMessage,
  SDKPartialAssistantMessage,
} from "@anthropic-ai/claude-agent-sdk";
import type { ChatMessage, ServerWsEvent, ToolCallKind, ToolCallView } from "../types/chat";

type IDFactory = () => string;

export function browserChatMessage(
  id: string,
  content: string,
  createdAt = new Date().toISOString(),
): ChatMessage {
  return {
    id,
    role: "user",
    content,
    createdAt,
  };
}

export class MessageProjector {
  private activeStreamMessageId: string | null = null;
  private readonly fragments = new Map<string, ChatMessage>();
  private readonly messageAliases = new Map<string, string>();
  private readonly toolUseMessageIds = new Map<string, string>();
  private readonly toolViews = new Map<string, ToolCallView>();

  constructor(
    private readonly sessionId: string,
    private readonly createId: IDFactory,
  ) {}

  projectAssistant(message: SDKAssistantMessage): ChatMessage | null {
    const explicitKey = assistantMessageKey(message);
    const fallbackKey = explicitKey || sdkUUID(message) || this.createId();
    const key = this.activeStreamMessageId || this.messageAliases.get(fallbackKey) || fallbackKey;
    const existing = this.fragments.get(key);
    const contentBlocks = assistantContentBlocks(message);
    const next: ChatMessage = existing
      ? { ...existing }
      : {
          id: key,
          role: "assistant",
          content: "",
          createdAt: new Date().toISOString(),
        };

    for (const block of contentBlocks) {
      if (!isRecord(block)) continue;
      if (block.type === "thinking") {
        const thinking = stringField(block.thinking) || stringField(block.text);
        if (thinking) next.thinking = appendUnique(next.thinking ?? "", thinking);
      }
      if (block.type === "text") {
        const text = typeof block.text === "string" ? block.text : "";
        if (text) next.content = appendUnique(next.content, text);
      }
      if (block.type === "tool_use") {
        const tool = toolFromBlock(block);
        next.tools = upsertTool(next.tools ?? [], tool);
        this.toolUseMessageIds.set(tool.id, next.id);
        this.toolViews.set(tool.id, tool);
      }
    }

    if (!next.content.trim() && !next.thinking?.trim() && (next.tools?.length ?? 0) === 0) {
      return null;
    }

    this.fragments.set(key, next);
    if (explicitKey && explicitKey !== next.id) {
      this.messageAliases.set(explicitKey, next.id);
    }
    if (sdkUUID(message) && sdkUUID(message) !== next.id) {
      this.messageAliases.set(sdkUUID(message), next.id);
    }
    if (this.activeStreamMessageId === next.id) {
      this.activeStreamMessageId = null;
    }
    return next;
  }

  projectStream(message: SDKPartialAssistantMessage): Extract<ServerWsEvent, { type: "partial" }> | null {
    const event = (message as { event?: unknown }).event;
    if (!isRecord(event)) return null;

    if (event.type === "message_start") {
      const eventMessage = isRecord(event.message) ? event.message : {};
      const rawMessageId = stringField(eventMessage.id);
      this.activeStreamMessageId = rawMessageId || sdkUUID(message as unknown as SDKMessage) || this.createId();
      return null;
    }

    if (event.type === "content_block_start") {
      if (!shouldKeepContentBlock(event.content_block)) return null;
      this.activeStreamMessageId =
        this.activeStreamMessageId ?? (sdkUUID(message as unknown as SDKMessage) || this.createId());
      return null;
    }

    if (event.type !== "content_block_delta") return null;
    const delta = isRecord(event.delta) ? event.delta : {};
    const id = this.activeStreamMessageId ?? (sdkUUID(message as unknown as SDKMessage) || this.createId());

    if (delta.type === "text_delta") {
      const text = typeof delta.text === "string" ? delta.text : "";
      if (!text) return null;
      return { type: "partial", id, sessionId: this.sessionId, delta: text, kind: "text" };
    }

    if (delta.type === "thinking_delta") {
      const thinking =
        typeof delta.thinking === "string" ? delta.thinking : typeof delta.text === "string" ? delta.text : "";
      if (!thinking.trim()) return null;
      return { type: "partial", id, sessionId: this.sessionId, delta: thinking, kind: "thinking" };
    }

    return null;
  }

  projectToolProgress(message: SDKMessage): { messageId: string; tool: ToolCallView } | null {
    const record = message as unknown as Record<string, unknown>;
    const toolUseID = stringField(record.tool_use_id);
    if (!toolUseID) return null;
    const previous = this.toolViews.get(toolUseID);
    const tool: ToolCallView = {
      id: toolUseID,
      name: previous?.name ?? (stringField(record.tool_name) || "tool"),
      kind: previous?.kind ?? classifyTool(stringField(record.tool_name)),
      status: "running",
      input: previous?.input,
    };
    this.toolViews.set(toolUseID, tool);
    return {
      messageId: this.toolUseMessageIds.get(toolUseID) ?? this.activeStreamMessageId ?? toolUseID,
      tool,
    };
  }
}

function assistantMessageKey(message: SDKAssistantMessage) {
  const sdkMessage: Record<string, unknown> = isRecord(message.message) ? message.message : {};
  return stringField(sdkMessage.id);
}

function assistantContentBlocks(message: SDKAssistantMessage) {
  const sdkMessage: Record<string, unknown> = isRecord(message.message) ? message.message : {};
  return Array.isArray(sdkMessage.content) ? sdkMessage.content.filter(shouldKeepContentBlock) : [];
}

function shouldKeepContentBlock(block: unknown) {
  if (!isRecord(block)) return true;
  if (block.type === "redacted_thinking") return false;
  if (block.type === "thinking") {
    return Boolean((stringField(block.thinking) || stringField(block.text)).trim());
  }
  return true;
}

function toolFromBlock(block: Record<string, unknown>): ToolCallView {
  const name = stringField(block.name) || "tool";
  return {
    id: stringField(block.id) || name,
    name,
    kind: classifyTool(name),
    status: "running",
    input: block.input,
  };
}

function classifyTool(name: string): ToolCallKind {
  const lower = name.toLowerCase();
  if (lower.includes("skill")) return "skill";
  if (lower.includes("bash")) return "command";
  if (lower.includes("read") || lower.includes("write") || lower.includes("edit")) return "file";
  if (lower.includes("grep") || lower.includes("glob") || lower.includes("search") || lower.includes("ls")) return "search";
  if (lower.includes("portfolio") || lower.includes("balance")) return "portfolio";
  if (lower.includes("swap") || lower.includes("quote")) return "swap";
  if (lower.includes("bridge")) return "bridge";
  if (lower.includes("security") || lower.includes("simulate") || lower.includes("check")) return "security";
  if (lower.includes("wallet") || lower.includes("ssm") || lower.includes("auth")) return "wallet";
  if (lower.includes("chain") || lower.includes("gas") || lower.includes("tx")) return "chain";
  if (lower.includes("token") || lower.includes("price")) return "token";
  return "tool";
}

function upsertTool(tools: ToolCallView[], next: ToolCallView) {
  return tools.some((tool) => tool.id === next.id)
    ? tools.map((tool) => (tool.id === next.id ? next : tool))
    : [...tools, next];
}

function appendUnique(existing: string, next: string) {
  if (!existing) return next;
  if (!next) return existing;
  if (next.startsWith(existing)) return next;
  if (existing.includes(next)) return existing;
  return `${existing}${next}`;
}

function sdkUUID(message: SDKMessage) {
  const uuid = (message as { uuid?: unknown }).uuid;
  return typeof uuid === "string" && uuid.trim() ? uuid : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function stringField(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}
