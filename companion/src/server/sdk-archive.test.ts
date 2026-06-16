// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import {
  normalizeSDKMessageForArchive,
  sdkMessageType,
  sdkMessageUUID,
  sdkParentToolUseID,
  shouldArchiveSDKMessage,
} from "./sdk-archive";

describe("sdk archive helpers", () => {
  test("normalizes missing uuid and session_id without changing content", () => {
    const message = normalizeSDKMessageForArchive(
      "session-1",
      { type: "assistant", message: { content: [{ type: "text", text: "hello" }] } } as never,
      "fallback-id",
    );

    expect((message as { uuid?: string }).uuid).toBe("fallback-id");
    expect((message as { session_id?: string }).session_id).toBe("session-1");
    expect((message as { message?: unknown }).message).toEqual({
      content: [{ type: "text", text: "hello" }],
    });
  });

  test("extracts sdk metadata for raw archive rows", () => {
    const message = {
      type: "assistant",
      uuid: "sdk-1",
      parent_tool_use_id: "tool-parent-1",
    };

    expect(sdkMessageUUID(message as never)).toBe("sdk-1");
    expect(sdkMessageType(message as never)).toBe("assistant");
    expect(sdkParentToolUseID(message as never)).toBe("tool-parent-1");
  });

  test("archives final sdk message categories but not stream events", () => {
    expect(shouldArchiveSDKMessage({ type: "assistant" } as never)).toBe(true);
    expect(shouldArchiveSDKMessage({ type: "user" } as never)).toBe(true);
    expect(shouldArchiveSDKMessage({ type: "stream_event" } as never)).toBe(false);
    expect(shouldArchiveSDKMessage({ type: "tool_progress" } as never)).toBe(true);
    expect(shouldArchiveSDKMessage({ type: "result" } as never)).toBe(true);
  });
});
