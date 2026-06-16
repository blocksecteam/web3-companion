// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { AuthExpiredError, fetchWithAuth, hydrateSession, needsSessionHydration } from "@/lib/auth-session";
import type { User } from "@/types/chat";

const originalFetch = globalThis.fetch;
const originalLocalStorage = globalThis.localStorage;

describe("fetchWithAuth", () => {
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

  test("refreshes an expired access token with the cookie session, persists metadata only, and retries the request", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      accessToken: "expired.jwt",
      tokenType: "Bearer",
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
      authSessionId: "sess_1",
    };
    const calls: Array<{ url: string; authorization: string | null; body: string }> = [];

    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      calls.push({
        url: String(input),
        authorization: headers.get("Authorization"),
        body: String(init?.body ?? ""),
      });

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

      if (calls.filter((call) => call.url === "/api/v1/users/context").length === 1) {
        return new Response(JSON.stringify({ error: { code: "invalid_token" } }), { status: 401 });
      }
      return new Response(JSON.stringify({ wallets: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const response = await fetchWithAuth(user, "/api/v1/users/context");

    expect(response.status).toBe(200);
    expect(calls).toEqual([
      { url: "/api/v1/users/context", authorization: "Bearer expired.jwt", body: "" },
      { url: "/auth/refresh", authorization: null, body: "{}" },
      { url: "/api/v1/users/context", authorization: "Bearer fresh.jwt", body: "" },
    ]);
    expect(user.accessToken).toBe("fresh.jwt");
    expect(user.refreshToken).toBeUndefined();
    const stored = JSON.parse(localStorage.getItem("web3-companion:user") ?? "{}");
    expect(stored.accessToken).toBeUndefined();
    expect(stored.refreshToken).toBeUndefined();
    expect(stored.authSessionId).toBe("sess_1");
  });

  test("refreshes before a request when only a browser session id is stored", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      authSessionId: "sess_1",
    };
    const calls: Array<{ url: string; authorization: string | null; body: string }> = [];

    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      calls.push({
        url: String(input),
        authorization: headers.get("Authorization"),
        body: String(init?.body ?? ""),
      });

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

      return new Response(JSON.stringify({ sessions: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const response = await fetchWithAuth(user, "/api/companion/sessions");

    expect(response.status).toBe(200);
    expect(calls).toEqual([
      { url: "/auth/refresh", authorization: null, body: "{}" },
      { url: "/api/companion/sessions", authorization: "Bearer fresh.jwt", body: "" },
    ]);
    expect(user.accessToken).toBe("fresh.jwt");
  });

  test("refresh locks are isolated by browser session", async () => {
    const users: User[] = [
      { userId: "user_1", email: "ada@example.com", authSessionId: "sess_1" },
      { userId: "user_2", email: "grace@example.com", authSessionId: "sess_2" },
    ];
    const refreshes: string[] = [];

    globalThis.fetch = (async (input: RequestInfo | URL) => {
      if (String(input) === "/auth/refresh") {
        const currentUser = refreshes.length === 0 ? users[0] : users[1];
        refreshes.push(currentUser.authSessionId ?? "");
        await Promise.resolve();
        return new Response(
          JSON.stringify({
            access_token: `${currentUser.authSessionId}.jwt`,
            token_type: "Bearer",
            expires_at: 1893456000,
            session_id: currentUser.authSessionId,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    }) as typeof fetch;

    const [firstToken, secondToken] = await Promise.all(users.map((user) => fetchWithAuth(user, "/api/test").then(() => user.accessToken)));

    expect(refreshes).toEqual(["sess_1", "sess_2"]);
    expect(firstToken).toBe("sess_1.jwt");
    expect(secondToken).toBe("sess_2.jwt");
  });

  test("refreshes before use when access token has no expiry metadata", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      accessToken: "stale.jwt",
      authSessionId: "sess_1",
    };
    const calls: string[] = [];

    globalThis.fetch = (async (input: RequestInfo | URL) => {
      calls.push(String(input));
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
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    }) as typeof fetch;

    await fetchWithAuth(user, "/api/test");

    expect(calls).toEqual(["/auth/refresh", "/api/test"]);
    expect(user.accessToken).toBe("fresh.jwt");
  });

  test("refresh failure clears local auth state and raises auth expired", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      accessToken: "expired.jwt",
      authSessionId: "sess_1",
      expiresAt: 1,
    };
    localStorage.setItem("web3-companion:user", JSON.stringify({ userId: user.userId, email: user.email, authSessionId: user.authSessionId }));

    globalThis.fetch = (async (input: RequestInfo | URL) => {
      if (String(input) === "/auth/refresh") {
        return new Response(JSON.stringify({ error: { code: "invalid_refresh_token" } }), { status: 401 });
      }
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    }) as typeof fetch;

    await expect(fetchWithAuth(user, "/api/test")).rejects.toThrow(AuthExpiredError);

    expect(localStorage.getItem("web3-companion:user")).toBeNull();
    expect(user.accessToken).toBeUndefined();
    expect(user.authSessionId).toBeUndefined();
  });

  test("refresh publishes a replacement user object instead of mutating React state in place", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      accessToken: "expired.jwt",
      authSessionId: "sess_1",
      expiresAt: 1,
    };
    const updatedUsers: User[] = [];

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
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    }) as typeof fetch;

    await fetchWithAuth(user, "/api/test", {}, { onUserUpdated: (nextUser) => { updatedUsers.push(nextUser); } });

    const updatedUser = updatedUsers[0];
    expect(updatedUser).toEqual({ ...user, accessToken: "fresh.jwt", tokenType: "Bearer", expiresAt: 1893456000 });
    expect(updatedUser).not.toBe(user);
  });

  test("hydrates a stored cookie-backed browser session before the app treats it as ready", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      authSessionId: "sess_1",
    };
    const updatedUsers: User[] = [];

    expect(needsSessionHydration(user)).toBe(true);
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      expect(String(input)).toBe("/auth/refresh");
      return new Response(
        JSON.stringify({
          access_token: "fresh.jwt",
          token_type: "Bearer",
          expires_at: 1893456000,
          session_id: "sess_1",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as typeof fetch;

    const hydrated = await hydrateSession(user, { onUserUpdated: (nextUser) => updatedUsers.push(nextUser) });

    expect(hydrated?.accessToken).toBe("fresh.jwt");
    expect(updatedUsers[0]?.accessToken).toBe("fresh.jwt");
    expect(needsSessionHydration(hydrated)).toBe(false);
  });

  test("returns null when stored browser session hydration fails", async () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      authSessionId: "sess_1",
    };

    globalThis.fetch = (async () => new Response(JSON.stringify({ error: { code: "invalid_refresh_token" } }), { status: 401 })) as unknown as typeof fetch;

    await expect(hydrateSession(user)).resolves.toBeNull();
    expect(localStorage.getItem("web3-companion:user")).toBeNull();
  });
});
