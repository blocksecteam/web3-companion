// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { afterEach, describe, expect, test } from "bun:test";
import { logoutSession, registerEmail, userFromLogin, userFromRegistration } from "@/lib/auth-client";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe("auth user mapping", () => {
  test("stores access token metadata from passkey login without retaining refresh tokens", () => {
    const user = userFromLogin({
      status: "authenticated",
      user_id: "user_1",
      email: "ada@example.com",
      credential_id: "cred_1",
      access_token: "access.jwt",
      refresh_token: "refresh-token",
      token_type: "Bearer",
      expires_at: 1893456000,
      session_id: "sess_1",
    });

    expect(user).toMatchObject({
      userId: "user_1",
      email: "ada@example.com",
      credentialId: "cred_1",
      accessToken: "access.jwt",
      tokenType: "Bearer",
      expiresAt: 1893456000,
      authSessionId: "sess_1",
    });
    expect(user.refreshToken).toBeUndefined();
  });

  test("stores access token metadata from passkey registration without retaining refresh tokens", () => {
    const user = userFromRegistration("ada@example.com", {
      status: "saved",
      user_id: "user_1",
      credential_id: "cred_1",
      access_token: "registered.jwt",
      refresh_token: "refresh-token",
      token_type: "Bearer",
      expires_at: 1893456000,
      session_id: "sess_1",
    });

    expect(user.accessToken).toBe("registered.jwt");
    expect(user.refreshToken).toBeUndefined();
    expect(user.authSessionId).toBe("sess_1");
  });
});

describe("registration requests", () => {
  test("starts passkey registration directly with email only", async () => {
    const bodies: unknown[] = [];
    globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
      bodies.push(JSON.parse(String(init?.body ?? "{}")));
      return new Response(JSON.stringify({ status: "pending_passkey", user_id: "user_1", temp_token: "tmp_1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    await registerEmail("ada@example.com");

    expect(bodies).toEqual([{ email: "ada@example.com" }]);
  });
});

describe("logout requests", () => {
  test("calls the backend logout endpoint with credentials so the refresh cookie can be cleared", async () => {
    const calls: Array<{ input: string; method?: string; credentials?: RequestCredentials; keepalive?: boolean }> = [];
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        input: String(input),
        method: init?.method,
        credentials: init?.credentials,
        keepalive: init?.keepalive,
      });
      return new Response(JSON.stringify({ status: "logged_out" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    await logoutSession();

    expect(calls).toEqual([
      {
        input: "/auth/logout",
        method: "POST",
        credentials: "include",
        keepalive: true,
      },
    ]);
  });
});
