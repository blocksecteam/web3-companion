// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { formatDateTime, formatTime } from "./utils";

describe("formatting utilities", () => {
  test("formats time markers with the requested locale", () => {
    const value = "2026-05-15T12:01:30Z";

    expect(formatTime(value, "en-US")).toMatch(/\b12:01 PM\b/);
    expect(formatTime(value, "zh-CN")).toContain("12:01");
  });

  test("formats date time markers with the requested locale", () => {
    const value = "2026-05-15T12:01:30Z";

    expect(formatDateTime(value, "en-US")).toMatch(/May 15, 12:01 PM|May 15, 12:01/);
    expect(formatDateTime(value, "zh-CN")).toContain("5月15日");
  });
});
