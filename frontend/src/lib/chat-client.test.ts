// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { CompanionSocket } from "./chat-client";
import type { ClientWsEvent, ServerWsEvent, User } from "@/types/chat";

class FakeWebSocket extends EventTarget {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  sent: string[] = [];

  constructor(
    readonly url: string,
    readonly protocols?: string[],
  ) {
    super();
    FakeWebSocket.instances.push(this);
  }

  send(message: string) {
    this.sent.push(message);
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.dispatchEvent(new Event("open"));
  }
}

const originalFetch = globalThis.fetch;
const originalLocalStorage = globalThis.localStorage;

describe("CompanionSocket", () => {
  beforeEach(() => {
    const values = new Map<string, string>();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
        removeItem: (key: string) => values.delete(key),
      },
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: originalLocalStorage,
    });
  });

  test("queues messages sent while the realtime channel is connecting", async () => {
    const previousWindow = globalThis.window;
    const previousWebSocket = globalThis.WebSocket;
    FakeWebSocket.instances = [];
    globalThis.window = {
      location: {
        protocol: "http:",
        host: "localhost:5173",
      },
    } as Window & typeof globalThis;
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    globalThis.fetch = websocketTicketFetch("ticket-1");
    try {
      const events: ServerWsEvent[] = [];
      const user: User = {
        userId: "user_1",
        email: "ada@example.com",
        accessToken: "access.jwt",
        tokenType: "Bearer",
        expiresAt: 1893456000,
        authSessionId: "sess_1",
      };
      const socket = new CompanionSocket(user, "session-1", (event) => events.push(event));
      const message: ClientWsEvent = {
        type: "user_message",
        id: "message-1",
        sessionId: "session-1",
        content: "hello",
      };

      socket.connect();
      socket.send(message);
      await waitFor(() => FakeWebSocket.instances.length > 0);

      const fake = FakeWebSocket.instances[0];
      expect(events).toHaveLength(0);
      expect(fake.sent).toHaveLength(0);

      fake.open();

      expect(fake.sent).toEqual([JSON.stringify(message)]);
      expect(events).toHaveLength(0);
    } finally {
      globalThis.window = previousWindow;
      globalThis.WebSocket = previousWebSocket;
    }
  });

  test("opens realtime connections with a one-time websocket ticket subprotocol", async () => {
    const previousWindow = globalThis.window;
    const previousWebSocket = globalThis.WebSocket;
    FakeWebSocket.instances = [];
    globalThis.window = {
      location: {
        protocol: "https:",
        host: "app.example.com",
      },
    } as Window & typeof globalThis;
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    const calls: Array<{ input: string; authorization: string | null; body: string }> = [];
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        input: String(input),
        authorization: new Headers(init?.headers).get("Authorization"),
        body: String(init?.body ?? ""),
      });
      return new Response(JSON.stringify({ ticket: "ticket-1", expires_at: 1893456000 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const user: User = {
        userId: "user_1",
        email: "ada@example.com",
        accessToken: "access.jwt",
        tokenType: "Bearer",
        expiresAt: 1893456000,
        authSessionId: "sess_1",
      };

      new CompanionSocket(user, "session-1", () => undefined).connect();
      await waitFor(() => FakeWebSocket.instances.length > 0);

      const fake = FakeWebSocket.instances[0];
      expect(fake.url).toBe("wss://app.example.com/ws/chat?session=session-1");
      expect(fake.protocols).toEqual(["web3-ws-ticket", "ticket-1"]);
      expect(calls).toEqual([
        {
          input: "/api/companion/ws-ticket",
          authorization: "Bearer access.jwt",
          body: JSON.stringify({ session_id: "session-1" }),
        },
      ]);
    } finally {
      globalThis.window = previousWindow;
      globalThis.WebSocket = previousWebSocket;
    }
  });

  test("refreshes expired JWT credentials before opening the realtime connection", async () => {
    const previousWindow = globalThis.window;
    const previousWebSocket = globalThis.WebSocket;
    FakeWebSocket.instances = [];
    globalThis.window = {
      location: {
        protocol: "https:",
        host: "app.example.com",
      },
    } as Window & typeof globalThis;
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      if (String(input) === "/auth/refresh") {
        return new Response(
          JSON.stringify({
            access_token: "fresh.jwt",
            token_type: "Bearer",
            expires_at: 1893456000,
            session_id: "sess_1",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      expect(String(input)).toBe("/api/companion/ws-ticket");
      return new Response(JSON.stringify({ ticket: "ticket-after-refresh", expires_at: 1893456000 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const user: User = {
        userId: "user_1",
        email: "ada@example.com",
        accessToken: "expired.jwt",
        tokenType: "Bearer",
        expiresAt: 1,
        authSessionId: "sess_1",
      };

      new CompanionSocket(user, "session-1", () => undefined).connect();

      expect(FakeWebSocket.instances).toHaveLength(0);
      await waitFor(() => FakeWebSocket.instances.length > 0);

      const fake = FakeWebSocket.instances[0];
      expect(fake.protocols).toEqual(["web3-ws-ticket", "ticket-after-refresh"]);
      expect(user.refreshToken).toBeUndefined();
    } finally {
      globalThis.window = previousWindow;
      globalThis.WebSocket = previousWebSocket;
    }
  });

  test("refreshes missing access credentials before opening the realtime connection", async () => {
    const previousWindow = globalThis.window;
    const previousWebSocket = globalThis.WebSocket;
    FakeWebSocket.instances = [];
    globalThis.window = {
      location: {
        protocol: "https:",
        host: "app.example.com",
      },
    } as Window & typeof globalThis;
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      if (String(input) === "/auth/refresh") {
        return new Response(
          JSON.stringify({
            access_token: "fresh.jwt",
            token_type: "Bearer",
            expires_at: 1893456000,
            session_id: "sess_1",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      expect(String(input)).toBe("/api/companion/ws-ticket");
      return new Response(JSON.stringify({ ticket: "ticket-after-missing-refresh", expires_at: 1893456000 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const user: User = {
        userId: "user_1",
        email: "ada@example.com",
        authSessionId: "sess_1",
      };

      new CompanionSocket(user, "session-1", () => undefined).connect();

      expect(FakeWebSocket.instances).toHaveLength(0);
      await waitFor(() => FakeWebSocket.instances.length > 0);

      const fake = FakeWebSocket.instances[0];
      expect(fake.protocols).toEqual(["web3-ws-ticket", "ticket-after-missing-refresh"]);
      expect(user.refreshToken).toBeUndefined();
    } finally {
      globalThis.window = previousWindow;
      globalThis.WebSocket = previousWebSocket;
    }
  });

  test("queues messages while credentials refresh before the realtime connection opens", async () => {
    const previousWindow = globalThis.window;
    const previousWebSocket = globalThis.WebSocket;
    let releaseRefresh: (() => void) | undefined;
    FakeWebSocket.instances = [];
    globalThis.window = {
      location: {
        protocol: "https:",
        host: "app.example.com",
      },
    } as Window & typeof globalThis;
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    globalThis.fetch = (async (input: RequestInfo | URL, _init?: RequestInit) => {
      if (String(input) === "/auth/refresh") {
        await new Promise<void>((resolve) => {
          releaseRefresh = resolve;
        });
        return new Response(
          JSON.stringify({
            access_token: "fresh.jwt",
            token_type: "Bearer",
            expires_at: 1893456000,
            session_id: "sess_1",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      expect(String(input)).toBe("/api/companion/ws-ticket");
      return new Response(JSON.stringify({ ticket: "ticket-after-queued-refresh", expires_at: 1893456000 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const events: ServerWsEvent[] = [];
      const user: User = {
        userId: "user_1",
        email: "ada@example.com",
        accessToken: "expired.jwt",
        tokenType: "Bearer",
        expiresAt: 1,
        authSessionId: "sess_1",
      };
      const message: ClientWsEvent = {
        type: "user_message",
        id: "message-1",
        sessionId: "session-1",
        content: "hello",
      };
      const socket = new CompanionSocket(user, "session-1", (event) => events.push(event));

      socket.connect();
      socket.send(message);
      expect(events).toHaveLength(0);
      expect(FakeWebSocket.instances).toHaveLength(0);

      releaseRefresh?.();
      await waitFor(() => FakeWebSocket.instances.length > 0);
      const fake = FakeWebSocket.instances[0];
      fake.open();

      expect(fake.sent).toEqual([JSON.stringify(message)]);
      expect(events).toHaveLength(0);
    } finally {
      globalThis.window = previousWindow;
      globalThis.WebSocket = previousWebSocket;
    }
  });
});

async function waitFor(condition: () => boolean) {
  for (let index = 0; index < 10; index++) {
    if (condition()) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
}

function websocketTicketFetch(ticket: string) {
  return (async () =>
    new Response(JSON.stringify({ ticket, expires_at: 1893456000 }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })) as unknown as typeof fetch;
}
