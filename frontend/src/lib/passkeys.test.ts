// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { passkeyErrorMessage, passkeyTimeoutMs } from "@/lib/passkeys";
import i18n from "@/i18n";

describe("passkey timeout normalization", () => {
  test("clamps excessive server timeout to a browser-safe upper bound", () => {
    expect(passkeyTimeoutMs(3_000_000_000)).toBe(600000);
  });
});

describe("passkey error normalization", () => {
  test("wraps the browser privacy NotAllowedError message for canceled ceremonies", () => {
    i18n.changeLanguage("en");
    const error = new Error(
      "The operation either timed out or was not allowed. See: https://www.w3.org/TR/webauthn-2/#sctn-privacy-considerations-client.",
    );
    error.name = "NotAllowedError";

    expect(passkeyErrorMessage(error, "Passkey failed.")).toBe("Passkey prompt was canceled or timed out.");
  });
});
