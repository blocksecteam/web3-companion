// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { hasResetAuthRequest, readRoute, shouldWriteHistory } from "@/lib/routes";

describe("routes", () => {
  test("treats query strings as part of the current browser path", () => {
    expect(
      shouldWriteHistory("/", {
        pathname: "/",
        search: "?resetAuth=1",
        hash: "",
      }),
    ).toBe(true);
  });

  test("does not rewrite history when pathname, search, and hash already match", () => {
    expect(
      shouldWriteHistory("/chat/session-1", {
        pathname: "/chat/session-1",
        search: "",
        hash: "",
      }),
    ).toBe(false);
  });

  test("detects reset auth requests from the current search params", () => {
    expect(hasResetAuthRequest("?resetAuth=1")).toBe(true);
    expect(hasResetAuthRequest("?resetAuth=0")).toBe(false);
    expect(hasResetAuthRequest("")).toBe(false);
  });

  test("reads transactions as its own route", () => {
    const previousWindow = globalThis.window;
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { location: { pathname: "/transactions" } },
    });

    try {
      expect(readRoute()).toEqual({ page: "transactions" });
    } finally {
      Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: previousWindow,
      });
    }
  });

  test("reads policies as its own product route", () => {
    const previousWindow = globalThis.window;
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { location: { pathname: "/policies" } },
    });

    try {
      expect(readRoute()).toEqual({ page: "policies" });
    } finally {
      Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: previousWindow,
      });
    }
  });

  test("reads wallets as its own route", () => {
    const previousWindow = globalThis.window;
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { location: { pathname: "/wallets" } },
    });

    try {
      expect(readRoute()).toEqual({ page: "wallets" });
    } finally {
      Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: previousWindow,
      });
    }
  });
});
