// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import {
  MAX_DECIMAL_INPUT_VALUE,
  MAX_NAME_UTF8_BYTES,
  utf8ByteLength,
  validateDecimalInput,
  validateIntegerInput,
  validateNameInput,
} from "@/lib/input-validation";

describe("input validation", () => {
  test("measures name length in UTF-8 bytes", () => {
    expect(MAX_NAME_UTF8_BYTES).toBe(50);
    expect(utf8ByteLength("a".repeat(50))).toBe(50);
    expect(utf8ByteLength("策略".repeat(8))).toBe(48);
    expect(validateNameInput("策略".repeat(8)).valid).toBe(true);
    expect(validateNameInput("策略".repeat(9))).toEqual({ valid: false, bytes: 54, maxBytes: 50 });
  });

  test("validates decimal text with a one billion maximum", () => {
    expect(MAX_DECIMAL_INPUT_VALUE).toBe(1_000_000_000);
    expect(validateDecimalInput("")).toEqual({ valid: true });
    expect(validateDecimalInput("12.34")).toEqual({ valid: true });
    expect(validateDecimalInput("1000000000")).toEqual({ valid: true });
    expect(validateDecimalInput("1000000000.01")).toEqual({ valid: false, max: 1_000_000_000 });
    expect(validateDecimalInput("1e3")).toEqual({ valid: false, max: 1_000_000_000 });
    expect(validateDecimalInput("-1")).toEqual({ valid: false, max: 1_000_000_000 });
  });

  test("validates integer-only numeric text with a one billion maximum", () => {
    expect(validateIntegerInput("")).toEqual({ valid: true });
    expect(validateIntegerInput("42")).toEqual({ valid: true });
    expect(validateIntegerInput("1000000000")).toEqual({ valid: true });
    expect(validateIntegerInput("42.5")).toEqual({ valid: false, max: 1_000_000_000 });
    expect(validateIntegerInput("1000000001")).toEqual({ valid: false, max: 1_000_000_000 });
  });
});
