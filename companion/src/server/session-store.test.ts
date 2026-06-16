// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, test } from "bun:test";
import { createSessionStoreFromConfig } from "./session-store";

const tempDirs: string[] = [];

function tempDB() {
  const dir = mkdtempSync(join(tmpdir(), "web3-companion-store-"));
  tempDirs.push(dir);
  return join(dir, "sessions.sqlite");
}

afterEach(() => {
  for (const dir of tempDirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("createSessionStoreFromConfig", () => {
  test("rejects empty sqlite store paths", () => {
    expect(() => createSessionStoreFromConfig("sqlite:file=")).toThrow(
      "COMPANION_SESSION_STORE sqlite:file requires a database path.",
    );
  });

  test("treats pg stores as unsupported until implemented", () => {
    expect(() => createSessionStoreFromConfig("pg://user:pass@localhost/db")).toThrow(
      "Unsupported companion session store: pg://user:pass@localhost/db",
    );
  });
});

describe("SessionStore display messages", () => {
  test("persists display messages in stable sequence order", async () => {
    const { SessionStore } = await import("./session-store");
    const store = new SessionStore(tempDB());

    store.addMessage("session-1", {
      id: "user-1",
      role: "user",
      content: "hello",
      createdAt: "2026-05-20T00:00:00.000Z",
    });
    store.addMessage("session-1", {
      id: "assistant-1",
      role: "assistant",
      content: "answer",
      thinking: "think",
      createdAt: "2026-05-20T00:00:00.000Z",
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

    expect(store.listMessages("session-1")).toEqual([
      {
        id: "user-1",
        role: "user",
        content: "hello",
        createdAt: "2026-05-20T00:00:00.000Z",
        partial: false,
      },
      {
        id: "assistant-1",
        role: "assistant",
        content: "answer",
        thinking: "think",
        createdAt: "2026-05-20T00:00:00.000Z",
        partial: false,
        tools: [
          {
            id: "tool-1",
            name: "Bash",
            kind: "command",
            status: "running",
            input: { command: "web3 context status" },
          },
        ],
      },
    ]);
    expect(store.getSession("session-1")?.messageCount).toBe(2);
  });

  test("upserts display messages by session and message id without changing order", async () => {
    const { SessionStore } = await import("./session-store");
    const store = new SessionStore(tempDB());

    store.addMessage("session-1", {
      id: "assistant-1",
      role: "assistant",
      content: "first",
      createdAt: "2026-05-20T00:00:00.000Z",
    });
    store.addMessage("session-1", {
      id: "assistant-1",
      role: "assistant",
      content: "updated",
      thinking: "think",
      createdAt: "2026-05-20T00:00:01.000Z",
    });

    expect(store.listMessages("session-1")).toEqual([
      {
        id: "assistant-1",
        role: "assistant",
        content: "updated",
        thinking: "think",
        createdAt: "2026-05-20T00:00:01.000Z",
        partial: false,
      },
    ]);
    expect(store.getSession("session-1")?.messageCount).toBe(1);
  });

  test("persists security review blocks with display messages", async () => {
    const { SessionStore } = await import("./session-store");
    const store = new SessionStore(tempDB());

    store.addMessage("session-1", {
      id: "security-review-1",
      role: "assistant",
      content: "",
      createdAt: "2026-05-20T00:00:00.000Z",
      blocks: [
        {
          id: "security-block-1",
          type: "security_review",
          status: "safe",
          transactionId: "txi_123",
          markdown: "### Web3 Security Agent Review: Safe",
        },
      ],
    });

    expect(store.listMessages("session-1")[0]).toMatchObject({
      id: "security-review-1",
      blocks: [
        {
          id: "security-block-1",
          type: "security_review",
          status: "safe",
          transactionId: "txi_123",
          markdown: "### Web3 Security Agent Review: Safe",
        },
      ],
    });
  });

  test("persists passkey request blocks with display messages", async () => {
    const { SessionStore } = await import("./session-store");
    const store = new SessionStore(tempDB());

    store.addMessage("session-1", {
      id: "passkey-request-ch_abc",
      role: "assistant",
      content: "",
      createdAt: "2026-05-20T00:00:00.000Z",
      blocks: [
        {
          id: "passkey-ch_abc",
          type: "passkey_request",
          status: "pending",
          operation: "sign",
          challengeId: "ch_abc",
          signUrl: "http://localhost:8081/confirm/ch_abc",
          transactionId: "txi_123",
          summary: { type: "swap", chain: "bsc" },
        },
      ],
    });

    expect(store.listMessages("session-1")[0]).toMatchObject({
      id: "passkey-request-ch_abc",
      blocks: [
        {
          id: "passkey-ch_abc",
          type: "passkey_request",
          status: "pending",
          operation: "sign",
          challengeId: "ch_abc",
          signUrl: "http://localhost:8081/confirm/ch_abc",
          transactionId: "txi_123",
          summary: { type: "swap", chain: "bsc" },
        },
      ],
    });
  });

  test("archives raw sdk messages separately from display messages", async () => {
    const { SessionStore } = await import("./session-store");
    const store = new SessionStore(tempDB());
    const raw = {
      type: "assistant",
      uuid: "sdk-1",
      session_id: "session-1",
      parent_tool_use_id: null,
      message: { role: "assistant", content: [{ type: "text", text: "hello" }] },
    };

    store.addRawSDKMessage("session-1", raw as never, "2026-05-20T00:00:00.000Z");
    store.addRawSDKMessage(
      "session-1",
      { ...raw, message: { role: "assistant", content: [{ type: "text", text: "updated" }] } } as never,
      "2026-05-20T00:00:01.000Z",
    );

    expect(store.listMessages("session-1")).toEqual([]);
    expect(store.getSession("session-1")?.messageCount).toBe(0);
    expect(store.listRawSDKMessages("session-1").map((entry) => entry.message) as unknown).toEqual([
      { ...raw, message: { role: "assistant", content: [{ type: "text", text: "updated" }] } },
    ]);
  });

});
