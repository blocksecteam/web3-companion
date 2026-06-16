// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { applyBlockUpdate, applyPartialMessage, upsertChatMessage } from "./message-state";

describe("chat message state", () => {
  test("replaces a streamed partial when the final assistant message arrives under a different id", () => {
    const withPartial = applyPartialMessage([], {
      type: "partial",
      id: "stream-msg-1",
      sessionId: "session-1",
      delta: "让我先看看你的账户状态和交易历史。",
      kind: "text",
    });

    const merged = upsertChatMessage(withPartial, {
      id: "final-msg-1",
      role: "assistant",
      content: "让我先看看你的账户状态和交易历史。",
      createdAt: "2026-05-25T00:00:00.000Z",
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

    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({
      id: "stream-msg-1",
      role: "assistant",
      content: "让我先看看你的账户状态和交易历史。",
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
    expect(merged[0].partial).toBeUndefined();
  });

  test("applies security review block updates to an assistant message", () => {
    const reviewing = applyBlockUpdate([], {
      type: "block_update",
      sessionId: "session-1",
      messageId: "security-review-1",
      block: {
        id: "security-block-1",
        type: "security_review",
        status: "reviewing",
        transactionId: "txi_123",
        markdown: "Web3 Security Agent 正在 Review 交易安全性...",
      },
    });

    expect(reviewing).toHaveLength(1);
    expect(reviewing[0]).toMatchObject({
      id: "security-review-1",
      role: "assistant",
      blocks: [
        {
          id: "security-block-1",
          status: "reviewing",
        },
      ],
    });

    const done = applyBlockUpdate(reviewing, {
      type: "block_update",
      sessionId: "session-1",
      messageId: "security-review-1",
      block: {
        id: "security-block-1",
        type: "security_review",
        status: "unsafe",
        transactionId: "txi_123",
        markdown: "### Web3 Security Agent Review: Unsafe\n\nDo not sign.",
      },
    });

    expect(done[0].blocks?.[0]).toMatchObject({
      id: "security-block-1",
      status: "unsafe",
      markdown: "### Web3 Security Agent Review: Unsafe\n\nDo not sign.",
    });
  });

  test("applies passkey request block updates to an assistant message", () => {
    const messages = applyBlockUpdate([], {
      type: "block_update",
      sessionId: "session-1",
      messageId: "passkey-request-ch_abc",
      block: {
        id: "passkey-ch_abc",
        type: "passkey_request",
        status: "pending",
        operation: "sign",
        challengeId: "ch_abc",
        signUrl: "http://localhost:8081/confirm/ch_abc",
        transactionId: "txi_123",
      },
    });

    expect(messages).toHaveLength(1);
    expect(messages[0]).toMatchObject({
      id: "passkey-request-ch_abc",
      role: "assistant",
      blocks: [
        {
          id: "passkey-ch_abc",
          type: "passkey_request",
          challengeId: "ch_abc",
          signUrl: "http://localhost:8081/confirm/ch_abc",
        },
      ],
    });
    expect(messages[0].partial).toBeUndefined();
  });
});
