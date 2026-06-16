// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { ChatBlock, ChatMessage, ServerWsEvent, ToolCallView } from "@/types/chat";

export function upsertChatMessage(messages: ChatMessage[], next: ChatMessage) {
  const duplicatePartial = findDuplicatePartialMessage(messages, next);
  if (duplicatePartial) {
    return messages.map((message) =>
      message.id === duplicatePartial.id
        ? { ...next, id: duplicatePartial.id, createdAt: duplicatePartial.createdAt }
        : message,
    );
  }
  const exists = messages.some((message) => message.id === next.id);
  return exists ? messages.map((message) => (message.id === next.id ? next : message)) : [...messages, next];
}

export function applyPartialMessage(
  messages: ChatMessage[],
  event: Extract<ServerWsEvent, { type: "partial" }>,
) {
  const existing = messages.find((message) => message.id === event.id);
  const next: ChatMessage = existing
    ? { ...existing, partial: true }
    : {
        id: event.id,
        role: "assistant",
        content: "",
        createdAt: new Date().toISOString(),
        partial: true,
      };

  if (event.kind === "thinking") {
    next.thinking = `${next.thinking ?? ""}${event.delta}`;
  } else {
    next.content = `${next.content}${event.delta}`;
  }

  return upsertChatMessage(messages, next);
}

export function applyToolUpdate(messages: ChatMessage[], messageId: string, tool: ToolCallView) {
  const existing = messages.find((message) => message.id === messageId);
  const next: ChatMessage = existing
    ? { ...existing, tools: upsertTool(existing.tools ?? [], tool) }
    : {
        id: messageId,
        role: "assistant",
        content: "",
        createdAt: new Date().toISOString(),
        partial: true,
        tools: [tool],
      };

  return upsertChatMessage(messages, next);
}

export function applyBlockUpdate(
  messages: ChatMessage[],
  event: Extract<ServerWsEvent, { type: "block_update" }>,
) {
  const existing = messages.find((message) => message.id === event.messageId);
  const next: ChatMessage = existing
    ? { ...existing, blocks: upsertBlock(existing.blocks ?? [], event.block), partial: event.block.status === "reviewing" || existing.partial }
    : {
        id: event.messageId,
        role: "assistant",
        content: "",
        createdAt: new Date().toISOString(),
        partial: event.block.status === "reviewing",
        blocks: [event.block],
      };

  if (event.block.status !== "reviewing") {
    next.partial = undefined;
  }

  return upsertChatMessage(messages, next);
}

function findDuplicatePartialMessage(messages: ChatMessage[], next: ChatMessage) {
  if (next.partial || next.role !== "assistant" || !next.content.trim()) return undefined;
  return messages.find((message) => (
    message.id !== next.id &&
    message.role === "assistant" &&
    Boolean(message.partial) &&
    message.content.trim() === next.content.trim()
  ));
}

function upsertTool(tools: ToolCallView[], next: ToolCallView) {
  return tools.some((tool) => tool.id === next.id)
    ? tools.map((tool) => (tool.id === next.id ? next : tool))
    : [...tools, next];
}

function upsertBlock(blocks: ChatBlock[], next: ChatBlock) {
  return blocks.some((block) => block.id === next.id)
    ? blocks.map((block) => (block.id === next.id ? next : block))
    : [...blocks, next];
}
