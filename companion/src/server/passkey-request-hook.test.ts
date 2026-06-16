// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, test } from "bun:test";
import {
  createPasskeyRequestHook,
  parsePasskeyRequestFromTool,
} from "./passkey-request-hook";
import { SessionStore } from "./session-store";
import type { ServerWsEvent } from "../types/chat";

const tempDirs: string[] = [];

function tempDB() {
  const dir = mkdtempSync(join(tmpdir(), "web3-passkey-block-"));
  tempDirs.push(dir);
  return join(dir, "sessions.sqlite");
}

afterEach(() => {
  for (const dir of tempDirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("parsePasskeyRequestFromTool", () => {
  test("parses pending sign response from web3 ssm sign stdout", () => {
    const parsed = parsePasskeyRequestFromTool(
      { command: "web3 ssm sign --transaction-id txi_123" },
      {
        stdout: JSON.stringify({
          status: "pending_user_auth",
          transaction_id: "txi_123",
          challenge_id: "ch_good123",
          sign_url: "http://localhost:8081/confirm/ch_good123",
          tx_summary: { type: "swap", chain: "bsc" },
        }),
      },
    );

    expect(parsed).toEqual({
      operation: "sign",
      transactionId: "txi_123",
      challengeId: "ch_good123",
      signUrl: "http://localhost:8081/confirm/ch_good123",
      summary: { type: "swap", chain: "bsc" },
    });
  });

  test("parses pending sign response from CLI 409 wrapper response", () => {
    const parsed = parsePasskeyRequestFromTool(
      { command: "web3 ssm sign --transaction-id txi_123" },
      [
        "Exit code 1",
        JSON.stringify({
          error: {
            message: "API error (status 409)",
          },
          response: {
            status: "pending_user_auth",
            transaction_id: "txi_123",
            challenge_id: "ch_retry456",
            sign_url: "http://localhost:8081/confirm/ch_retry456",
            tx_summary: { type: "swap", protocol: "pancake" },
          },
        }),
      ].join("\n"),
    );

    expect(parsed).toEqual({
      operation: "sign",
      transactionId: "txi_123",
      challengeId: "ch_retry456",
      signUrl: "http://localhost:8081/confirm/ch_retry456",
      summary: { type: "swap", protocol: "pancake" },
    });
  });

  test("parses pending delegation response from web3 ssm delegate stdout", () => {
    const parsed = parsePasskeyRequestFromTool(
      { command: "web3 ssm delegate --wallet main --chain bsc" },
      {
        stdout: JSON.stringify({
          status: "pending_delegation",
          challenge_id: "ch_delegate789",
          sign_url: "http://localhost:8081/confirm/ch_delegate789",
        }),
      },
    );

    expect(parsed).toEqual({
      operation: "delegate",
      challengeId: "ch_delegate789",
      signUrl: "http://localhost:8081/confirm/ch_delegate789",
    });
  });

  test("parses pending sign response when command has trailing stderr redirection", () => {
    const parsed = parsePasskeyRequestFromTool(
      { command: "web3 ssm sign --transaction-id txi_123 2>&1" },
      [
        "warning: slow local network",
        JSON.stringify({
          status: "pending_user_auth",
          transaction_id: "txi_123",
          challenge_id: "ch_redirect",
          sign_url: "http://localhost:8081/confirm/ch_redirect",
        }),
      ].join("\n"),
    );

    expect(parsed).toMatchObject({
      operation: "sign",
      transactionId: "txi_123",
      challengeId: "ch_redirect",
      signUrl: "http://localhost:8081/confirm/ch_redirect",
    });
  });

  test("parses pending_user_auth response from web3 security override", () => {
    const parsed = parsePasskeyRequestFromTool(
      { command: "web3 security override --transaction-id txi_abc --reason \"I accept the risk\"" },
      {
        stdout: JSON.stringify({
          status: "pending_user_auth",
          transaction_id: "txi_abc",
          challenge_id: "ch_override001",
          sign_url: "http://localhost:8081/confirm/ch_override001",
        }),
      },
    );

    expect(parsed).toEqual({
      operation: "security_override",
      transactionId: "txi_abc",
      challengeId: "ch_override001",
      signUrl: "http://localhost:8081/confirm/ch_override001",
    });
  });

  test("ignores non-passkey commands and incomplete responses", () => {
    expect(parsePasskeyRequestFromTool(
      { command: "web3 security check --transaction-id txi_123" },
      { stdout: "{\"challenge_id\":\"ch_ignored\",\"sign_url\":\"/confirm/ch_ignored\"}" },
    )).toBeNull();

    expect(parsePasskeyRequestFromTool(
      { command: "web3 ssm sign-status ch_123" },
      { stdout: "{\"challenge_id\":\"ch_ignored\",\"status\":\"pending\"}" },
    )).toBeNull();
  });
});

describe("createPasskeyRequestHook", () => {
  test("emits and persists a stable passkey request block", async () => {
    const store = new SessionStore(tempDB());
    const events: ServerWsEvent[] = [];
    const hook = createPasskeyRequestHook({
      sessionId: "session-1",
      store,
      emit: (event) => events.push(event),
    });

    await hook({
      hook_event_name: "PostToolUse",
      tool_name: "Bash",
      tool_input: { command: "web3 ssm sign --transaction-id txi_123" },
      tool_response: {
        stdout: JSON.stringify({
          status: "pending_user_auth",
          transaction_id: "txi_123",
          challenge_id: "ch_good123",
          sign_url: "http://localhost:8081/confirm/ch_good123",
          tx_summary: { type: "swap" },
        }),
      },
    } as never, "tool-1", { signal: new AbortController().signal } as never);

    expect(events).toHaveLength(1);
    expect(events[0]).toEqual({
      type: "block_update",
      sessionId: "session-1",
      messageId: "passkey-request-ch_good123",
      block: {
        id: "passkey-ch_good123",
        type: "passkey_request",
        status: "pending",
        operation: "sign",
        transactionId: "txi_123",
        challengeId: "ch_good123",
        signUrl: "http://localhost:8081/confirm/ch_good123",
        summary: { type: "swap" },
      },
    });

    expect(store.listMessages("session-1")[0]).toMatchObject({
      id: "passkey-request-ch_good123",
      role: "assistant",
      blocks: [
        {
          id: "passkey-ch_good123",
          type: "passkey_request",
          challengeId: "ch_good123",
        },
      ],
    });
  });

  test("uses tool JSON challenge id even if later assistant text could differ", async () => {
    const store = new SessionStore(tempDB());
    const events: ServerWsEvent[] = [];
    const hook = createPasskeyRequestHook({
      sessionId: "session-1",
      store,
      emit: (event) => events.push(event),
    });

    await hook({
      hook_event_name: "PostToolUse",
      tool_name: "Bash",
      tool_input: { command: "web3 ssm sign --transaction-id txi_123" },
      tool_response: {
        stdout: JSON.stringify({
          status: "pending_user_auth",
          transaction_id: "txi_123",
          challenge_id: "ch_NZsLagOIKIeU6HL9ty5Twg",
          sign_url: "http://localhost:8081/confirm/ch_NZsLagOIKIeU6HL9ty5Twg",
        }),
      },
    } as never, "tool-1", { signal: new AbortController().signal } as never);

    expect(events[0]).toMatchObject({
      block: {
        challengeId: "ch_NZsLagOIKIeU6HL9ty5Twg",
        signUrl: "http://localhost:8081/confirm/ch_NZsLagOIKIeU6HL9ty5Twg",
      },
    });
  });
});
