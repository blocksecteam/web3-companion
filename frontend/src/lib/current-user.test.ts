// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { getCurrentUser, logout, onCurrentUserChanged, setCurrentUser } from "@/lib/current-user";

const originalLocalStorage = globalThis.localStorage;

describe("current user storage", () => {
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
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: originalLocalStorage,
    });
  });

  test("persists user metadata without bearer or refresh tokens", () => {
    const user = {
      userId: "user_1",
      email: "ada@example.com",
      accessToken: "access.jwt",
      refreshToken: "refresh-token",
      authSessionId: "sess_1",
      credentialId: "cred_1",
    };
    setCurrentUser(user);

    expect(user.accessToken).toBe("access.jwt");
    expect(user.refreshToken).toBe("refresh-token");
    expect(JSON.parse(localStorage.getItem("web3-companion:user") ?? "{}")).toEqual({
      userId: "user_1",
      email: "ada@example.com",
      authSessionId: "sess_1",
      credentialId: "cred_1",
    });

    expect(getCurrentUser()).toEqual({
      userId: "user_1",
      email: "ada@example.com",
      authSessionId: "sess_1",
      credentialId: "cred_1",
    });

    logout();

    expect(getCurrentUser()).toBeNull();
  });

  test("notifies subscribers with the full in-memory user object", () => {
    const seen: Array<string | null> = [];
    const unsubscribe = onCurrentUserChanged((user) => seen.push(user?.accessToken ?? null));

    setCurrentUser({ userId: "user_1", email: "ada@example.com", accessToken: "access.jwt", authSessionId: "sess_1" });
    logout();
    unsubscribe();
    setCurrentUser({ userId: "user_1", email: "ada@example.com", accessToken: "ignored.jwt", authSessionId: "sess_1" });

    expect(seen).toEqual(["access.jwt", null]);
  });
});
