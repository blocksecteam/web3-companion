// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { ServerWebSocket } from "bun";
import type { ChatMessage, ServerWsEvent, SessionSummary } from "../types/chat";
import { createCompanionRuntime } from "./app";

type StoreSession = SessionSummary & { hasClaudeSession?: boolean };

class MemoryStore {
  sessions = new Map<string, StoreSession>();
  messages = new Map<string, ChatMessage[]>();
  rawMessages = new Map<string, unknown[]>();

  listSessions() {
    return Array.from(this.sessions.values());
  }

  getSession(id: string) {
    return this.sessions.get(id) ?? null;
  }

  ensureSession(id: string) {
    const existing = this.sessions.get(id);
    if (existing) return existing;
    const now = new Date().toISOString();
    const session: StoreSession = {
      id,
      title: "New on-chain conversation",
      createdAt: now,
      updatedAt: now,
      status: "idle",
      messageCount: 0,
      hasClaudeSession: false,
    };
    this.sessions.set(id, session);
    return session;
  }

  updateSessionStatus(id: string, status: "idle" | "running") {
    const session = this.ensureSession(id);
    const next = { ...session, status, updatedAt: new Date().toISOString() };
    this.sessions.set(id, next);
    return next;
  }

  markClaudeSession(id: string) {
    const session = this.ensureSession(id);
    this.sessions.set(id, { ...session, hasClaudeSession: true });
  }

  renameFromFirstMessage(id: string, content: string) {
    const session = this.ensureSession(id);
    this.sessions.set(id, { ...session, title: content.trim().slice(0, 64) || session.title });
  }

  addMessage(sessionId: string, message: ChatMessage) {
    this.ensureSession(sessionId);
    const messages = this.messages.get(sessionId) ?? [];
    const next = messages.some((item) => item.id === message.id)
      ? messages.map((item) => (item.id === message.id ? message : item))
      : [...messages, message];
    this.messages.set(sessionId, next);
    const session = this.ensureSession(sessionId);
    this.sessions.set(sessionId, { ...session, messageCount: next.length });
    return message;
  }

  addRawSDKMessage(sessionId: string, message: unknown) {
    const messages = this.rawMessages.get(sessionId) ?? [];
    messages.push(message);
    this.rawMessages.set(sessionId, messages);
    return {
      sequence: messages.length,
      sessionId,
      createdAt: new Date().toISOString(),
      message,
    };
  }

  listMessages(sessionId: string) {
    return this.messages.get(sessionId) ?? [];
  }

  deleteSession(id: string) {
    this.sessions.delete(id);
    this.messages.delete(id);
  }
}

class FakeSocket {
  readonly data: { sessionId: string };
  readyState: number = WebSocket.OPEN;
  sent: ServerWsEvent[] = [];

  constructor(sessionId: string) {
    this.data = { sessionId };
  }

  send(raw: string) {
    this.sent.push(JSON.parse(raw) as ServerWsEvent);
  }
}

afterEach(() => {
  delete process.env.ANTHROPIC_AUTH_TOKEN;
  delete process.env.ANTHROPIC_API_KEY;
  delete process.env.COMPANION_STARTUP_NONCE;
  delete process.env.CLAUDE_CONFIG_DIR;
});

async function withSettingsModelAuth() {
  const dir = join(import.meta.dir, "../../dist/test-claude-settings", crypto.randomUUID());
  await rm(dir, { recursive: true, force: true });
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "settings.json"), JSON.stringify({ env: { ANTHROPIC_AUTH_TOKEN: "model-token" } }));
  process.env.CLAUDE_CONFIG_DIR = dir;
}

describe("createCompanionRuntime", () => {
  test("health reports process readiness when model auth is missing", async () => {
    const runtime = createCompanionRuntime(new MemoryStore() as never);

    const response = await runtime.app.request("/api/health");

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      ok: true,
      code: "model_auth_missing",
      sdk: "claude-agent-sdk",
      model_auth_configured: false,
    });
  });

  test("health returns sdk and nonce when model auth is configured", async () => {
    await withSettingsModelAuth();
    process.env.COMPANION_STARTUP_NONCE = "nonce-1";
    const runtime = createCompanionRuntime(new MemoryStore() as never);

    const response = await runtime.app.request("/api/health");

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      ok: true,
      sdk: "claude-agent-sdk",
      model_auth_configured: true,
      nonce: "nonce-1",
    });
  });

  test("malformed websocket message returns error without creating a running session", async () => {
    await withSettingsModelAuth();
    const store = new MemoryStore();
    const runtime = createCompanionRuntime(store as never);
    const ws = new FakeSocket("session-1");
    runtime.websocket.open(ws as unknown as ServerWebSocket<{ sessionId: string }>);

    runtime.websocket.message(
      ws as unknown as ServerWebSocket<{ sessionId: string }>,
      JSON.stringify({ type: "user_message", sessionId: "session-1" }),
    );
    await Bun.sleep(5);

    expect(ws.sent.some((event) => event.type === "error")).toBe(true);
    expect(store.getSession("session-1")?.status).not.toBe("running");
  });

  test("closing the last websocket does not abort an active run", async () => {
    await withSettingsModelAuth();
    const store = new MemoryStore();
    const runtime = createCompanionRuntime(store as never, {
      runCompanion: async ({ signal }) => {
        await Bun.sleep(25);
        if (signal.aborted) {
          throw new Error("run was aborted");
        }
      },
    });
    const ws = new FakeSocket("session-1");
    runtime.websocket.open(ws as unknown as ServerWebSocket<{ sessionId: string }>);

    runtime.websocket.message(
      ws as unknown as ServerWebSocket<{ sessionId: string }>,
      JSON.stringify({ type: "user_message", id: "msg-1", sessionId: "session-1", content: "hello" }),
    );
    await Bun.sleep(1);
    ws.readyState = WebSocket.CLOSED;
    runtime.websocket.close(ws as unknown as ServerWebSocket<{ sessionId: string }>, 1000, "refresh");
    await Bun.sleep(40);

    expect(ws.sent.some((event) => event.type === "error" && event.message.includes("aborted"))).toBe(false);
    expect(store.getSession("session-1")?.status).toBe("idle");
  });

  test("persists and broadcasts browser prompt and assistant chat messages in order", async () => {
    await withSettingsModelAuth();
    const store = new MemoryStore();
    const runtime = createCompanionRuntime(store as never, {
      runCompanion: async ({ store, sessionId, emit }) => {
        const assistant: ChatMessage = {
          id: "assistant-1",
          role: "assistant",
          content: "I checked it.",
          thinking: "check wallet",
          createdAt: "2026-05-20T00:00:00.000Z",
          tools: [{ id: "tool-1", name: "Bash", kind: "command", status: "running", input: { command: "web3 context status" } }],
        };
        emit({ type: "message", sessionId, message: store.addMessage(sessionId, assistant) });
      },
    });
    const ws = new FakeSocket("session-1");
    runtime.websocket.open(ws as unknown as ServerWebSocket<{ sessionId: string }>);

    runtime.websocket.message(
      ws as unknown as ServerWebSocket<{ sessionId: string }>,
      JSON.stringify({ type: "user_message", id: "client-msg-1", sessionId: "session-1", content: "hello" }),
    );
    await Bun.sleep(5);

    const messageEvents = ws.sent.filter((event) => event.type === "message");
    expect(messageEvents.map((event) => event.message.role)).toEqual(["user", "assistant"]);
    expect(store.listMessages("session-1").map((message) => message.role)).toEqual([
      "user",
      "assistant",
    ]);
  });

  test("serves ordered chat messages", async () => {
    const store = new MemoryStore();
    store.addMessage("session-1", {
      id: "assistant-1",
      role: "assistant",
      content: "hello",
      createdAt: "2026-05-20T00:00:00.000Z",
    });
    const runtime = createCompanionRuntime(store as never);

    const response = await runtime.app.request("/api/sessions/session-1/messages");

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual(store.listMessages("session-1"));
  });
});
