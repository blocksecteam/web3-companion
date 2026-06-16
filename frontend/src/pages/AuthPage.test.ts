// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("./AuthPage.tsx", import.meta.url), "utf8");

describe("AuthPage registration confirmation", () => {
  test("does not fall back to the current email when pendingUser is missing", () => {
    expect(source).not.toContain("pendingUser?.email ?? normalizedEmail");
    expect(source).toContain("pendingUser.email");
  });

  test("starts unfinished registration with passkey setup", () => {
    expect(source).toContain("registerEmail(normalizedEmail)");
    expect(source).toContain('setPhase("register-passkey")');
  });
});
