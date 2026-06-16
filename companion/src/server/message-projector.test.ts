// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { MessageProjector, browserChatMessage } from "./message-projector";

describe("message projector", () => {
  test("builds browser prompt chat messages", () => {
    expect(browserChatMessage("msg-1", "hello", "2026-05-20T00:00:00.000Z")).toEqual({
      id: "msg-1",
      role: "user",
      content: "hello",
      createdAt: "2026-05-20T00:00:00.000Z",
    });
  });

  test("projects assistant content into ChatMessage fields", () => {
    const projector = new MessageProjector("session-1", () => "generated-1");
    const projected = projector.projectAssistant({
      type: "assistant",
      uuid: "assistant-uuid",
      session_id: "session-1",
      parent_tool_use_id: null,
      message: {
        id: "msg-1",
        role: "assistant",
        content: [
          { type: "thinking", signature: "sig-only" },
          { type: "redacted_thinking", data: "hidden" },
          { type: "thinking", thinking: "think\nmore", signature: "sig" },
          { type: "text", text: "Visible answer." },
          { type: "tool_use", id: "tool-1", name: "Bash", input: { command: "web3 context status" } },
        ],
      },
    } as never);

    expect(projected).toEqual({
      id: "msg-1",
      role: "assistant",
      content: "Visible answer.",
      thinking: "think\nmore",
      createdAt: expect.any(String),
      tools: [
        {
          id: "tool-1",
          name: "Bash",
          kind: "command",
          status: "running",
          input: { command: "web3 context status" },
        },
      ],
    });
  });

  test("merges assistant fragments with the same sdk message id", () => {
    const projector = new MessageProjector("session-1", () => "generated-1");
    const first = projector.projectAssistant({
      type: "assistant",
      uuid: "fragment-text",
      session_id: "session-1",
      message: { id: "msg-1", role: "assistant", content: [{ type: "text", text: "visible" }] },
    } as never);
    const second = projector.projectAssistant({
      type: "assistant",
      uuid: "fragment-thinking",
      session_id: "session-1",
      message: { id: "msg-1", role: "assistant", content: [{ type: "thinking", thinking: "think" }] },
    } as never);

    expect(first?.id).toBe("msg-1");
    expect(second).toMatchObject({
      id: "msg-1",
      role: "assistant",
      content: "visible",
      thinking: "think",
    });
  });

  test("final assistant replaces a streamed partial with the same message id", () => {
    const projector = new MessageProjector("session-1", () => "generated-1");
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-1",
        session_id: "session-1",
        event: { type: "message_start", message: { id: "msg-1", role: "assistant", content: [] } },
      } as never),
    ).toBeNull();
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-2",
        session_id: "session-1",
        event: { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "hello" } },
      } as never),
    ).toEqual({ type: "partial", id: "msg-1", sessionId: "session-1", delta: "hello", kind: "text" });

    const projected = projector.projectAssistant({
      type: "assistant",
      uuid: "assistant-uuid",
      session_id: "session-1",
      message: { role: "assistant", content: [{ type: "text", text: "hello" }] },
    } as never);

    expect(projected).toMatchObject({
      id: "msg-1",
      role: "assistant",
      content: "hello",
    });
  });

  test("final assistant reuses the streamed message id even when sdk emits a different final id", () => {
    const projector = new MessageProjector("session-1", () => "generated-1");
    projector.projectStream({
      type: "stream_event",
      uuid: "stream-uuid-1",
      session_id: "session-1",
      event: { type: "message_start", message: { id: "stream-msg-1", role: "assistant", content: [] } },
    } as never);
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-uuid-2",
        session_id: "session-1",
        event: { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "hello" } },
      } as never),
    ).toEqual({ type: "partial", id: "stream-msg-1", sessionId: "session-1", delta: "hello", kind: "text" });

    const projected = projector.projectAssistant({
      type: "assistant",
      uuid: "assistant-uuid",
      session_id: "session-1",
      message: {
        id: "final-msg-1",
        role: "assistant",
        content: [
          { type: "text", text: "hello" },
          { type: "tool_use", id: "tool-1", name: "Read", input: { file_path: "web3.domains.context" } },
        ],
      },
    } as never);

    expect(projected).toMatchObject({
      id: "stream-msg-1",
      role: "assistant",
      content: "hello",
      tools: [
        {
          id: "tool-1",
          name: "Read",
          kind: "file",
          status: "running",
          input: { file_path: "web3.domains.context" },
        },
      ],
    });
  });

  test("does not append cumulative assistant content twice", () => {
    const projector = new MessageProjector("session-1", () => "generated-1");
    projector.projectAssistant({
      type: "assistant",
      uuid: "fragment-1",
      session_id: "session-1",
      message: { id: "msg-1", role: "assistant", content: [{ type: "text", text: "hello" }] },
    } as never);

    const projected = projector.projectAssistant({
      type: "assistant",
      uuid: "fragment-2",
      session_id: "session-1",
      message: { id: "msg-1", role: "assistant", content: [{ type: "text", text: "hello world" }] },
    } as never);

    expect(projected).toMatchObject({
      id: "msg-1",
      role: "assistant",
      content: "hello world",
    });
  });

  test("projects stream deltas into partial events and ignores signature-only data", () => {
    const projector = new MessageProjector("session-1", () => "generated-1");
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-1",
        session_id: "session-1",
        event: { type: "message_start", message: { id: "msg-1", role: "assistant", content: [] } },
      } as never),
    ).toBeNull();
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-2",
        session_id: "session-1",
        event: { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "think" } },
      } as never),
    ).toEqual({ type: "partial", id: "msg-1", sessionId: "session-1", delta: "think", kind: "thinking" });
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-3",
        session_id: "session-1",
        event: { type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "visible" } },
      } as never),
    ).toEqual({ type: "partial", id: "msg-1", sessionId: "session-1", delta: "visible", kind: "text" });
    expect(
      projector.projectStream({
        type: "stream_event",
        uuid: "stream-4",
        session_id: "session-1",
        event: { type: "content_block_delta", index: 2, delta: { type: "signature_delta", signature: "sig" } },
      } as never),
    ).toBeNull();
  });
});
