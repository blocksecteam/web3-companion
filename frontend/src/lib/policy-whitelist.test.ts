// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { parseRecipientWhitelist, recipientWhitelistStatus } from "@/lib/policy-whitelist";

describe("parseRecipientWhitelist", () => {
  test("normalizes newline-separated Ethereum addresses before saving", () => {
    const result = parseRecipientWhitelist(
      [
        "  0xA0B86991C6218B36C1D19D4A2E9EB0CE3606EB48  ",
        "",
        "0x1111111111111111111111111111111111111111",
        "0x1111111111111111111111111111111111111111",
      ].join("\n"),
    );

    expect(result).toEqual({
      addresses: [
        "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
        "0x1111111111111111111111111111111111111111",
        "0x1111111111111111111111111111111111111111",
      ],
      invalidLines: [],
    });
  });

  test("rejects comma-separated addresses because entries must be line separated", () => {
    const result = parseRecipientWhitelist(
      "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48, 0x1111111111111111111111111111111111111111",
    );

    expect(result).toEqual({
      addresses: [],
      invalidLines: [1],
    });
  });

  test("reports non-empty lines that are not standard Ethereum addresses", () => {
    const result = parseRecipientWhitelist(
      [
        "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
        "0xnot-an-address",
        "ethereum:0x1111111111111111111111111111111111111111",
      ].join("\n"),
    );

    expect(result).toEqual({
      addresses: ["0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"],
      invalidLines: [2, 3],
    });
  });
});

describe("recipientWhitelistStatus", () => {
  test("an empty string is valid (no syntax errors) but carries validCount=0 — load-bearing for the editor's save-button guard", () => {
    // policy-ui.tsx disables the save button when
    // `recipientWhitelistEnabled && validCount === 0`. That guard depends
    // on this exact behaviour: an empty whitelist must report `valid:true`
    // (no line-level error to surface) AND `validCount:0` (no usable
    // addresses). If a future refactor flipped the empty case to
    // `valid:false` or counted whitespace as an entry, the UI guard would
    // either over-block (every fresh whitelist field would error) or
    // under-block (re-introducing the silent auto-sign degradation where
    // the user saves `enabled=true` with an empty list, every transfer
    // then falls through to DecisionWebAuthn at internal/policy/service.go).
    expect(recipientWhitelistStatus("")).toEqual({ valid: true, validCount: 0, invalidLines: [] });
    expect(recipientWhitelistStatus("   \n  \n")).toEqual({ valid: true, validCount: 0, invalidLines: [] });
  });

  test("describes realtime validity and line-level problems for the editor", () => {
    const status = recipientWhitelistStatus(
      [
        "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
        "0xnot-an-address",
        "0x1111111111111111111111111111111111111111, 0x2222222222222222222222222222222222222222",
      ].join("\n"),
    );

    expect(status).toEqual({
      valid: false,
      validCount: 1,
      invalidLines: [
        { line: 2, value: "0xnot-an-address", reason: "invalid_address" },
        {
          line: 3,
          value: "0x1111111111111111111111111111111111111111, 0x2222222222222222222222222222222222222222",
          reason: "multiple_addresses",
        },
      ],
    });
  });
});
