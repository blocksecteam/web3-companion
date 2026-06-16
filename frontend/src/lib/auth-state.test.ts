// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { authStatus, hasSessionCredential } from "@/lib/auth-state";
import type { User } from "@/types/chat";

describe("auth state", () => {
  test("treats missing users as guest", () => {
    expect(authStatus(null)).toBe("guest");
  });

  test("treats stored users without a session as stale", () => {
    const user: User = {
      userId: "user_1",
      email: "ada@example.com",
      credentialId: "cred_1",
    };

    expect(authStatus(user)).toBe("stale");
    expect(hasSessionCredential(user)).toBe(false);
  });

  test("treats access tokens or browser session ids as ready credentials", () => {
    const base = { userId: "user_1", email: "ada@example.com" };

    expect(authStatus({ ...base, accessToken: "access.jwt" })).toBe("ready");
    expect(authStatus({ ...base, authSessionId: "sess_1" })).toBe("ready");
    expect(authStatus({ ...base, refreshToken: "refresh-token" })).toBe("stale");
  });
});
