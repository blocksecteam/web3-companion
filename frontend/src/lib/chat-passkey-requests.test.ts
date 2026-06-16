// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { passkeyRequestsForMessage } from "@/lib/chat-passkey-requests";
import type { ChatMessage } from "@/types/chat";

function assistantMessage(partial: Partial<ChatMessage>): ChatMessage {
  return {
    id: "msg-1",
    role: "assistant",
    content: "",
    createdAt: "2026-05-20T00:00:00.000Z",
    ...partial,
  };
}

describe("passkeyRequestsForMessage", () => {
  test("uses structured passkey request blocks", () => {
    const result = passkeyRequestsForMessage(assistantMessage({
      content: "Passkey confirmation is ready.",
      blocks: [
        {
          id: "passkey-ch_good",
          type: "passkey_request",
          status: "pending",
          operation: "sign",
          challengeId: "ch_good",
          signUrl: "http://localhost:8081/confirm/ch_good",
          transactionId: "txi_123",
        },
      ],
    }));

    expect(result.requests).toEqual([
      {
        challengeId: "ch_good",
        signUrl: "http://localhost:8081/confirm/ch_good",
        source: "block",
        operation: "sign",
        transactionId: "txi_123",
      },
    ]);
    expect(result.displayContent).toBe("Passkey confirmation is ready.");
  });

  test("structured block wins over corrupted assistant text link", () => {
    const result = passkeyRequestsForMessage(assistantMessage({
      content: "Confirm here: http://localhost:8081/confirm/ch_bad",
      blocks: [
        {
          id: "passkey-ch_good",
          type: "passkey_request",
          status: "pending",
          operation: "sign",
          challengeId: "ch_good",
          signUrl: "http://localhost:8081/confirm/ch_good",
        },
      ],
    }));

    expect(result.requests).toEqual([
      {
        challengeId: "ch_good",
        signUrl: "http://localhost:8081/confirm/ch_good",
        source: "block",
        operation: "sign",
      },
    ]);
    expect(result.displayContent).toBe("Confirm here:");
  });

  test("does not parse text links from streaming partial messages", () => {
    const result = passkeyRequestsForMessage(assistantMessage({
      partial: true,
      content: "Confirm here: http://localhost:8081/confirm/ch_partial",
    }));

    expect(result.requests).toEqual([]);
    expect(result.displayContent).toBe("Confirm here: http://localhost:8081/confirm/ch_partial");
  });
});
