// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import i18n from "@/i18n";
import { chainDisplayName, chainOptionLabel } from "./chain-display";

describe("chain display copy", () => {
  test("renders bsc as Binance Smart Chain", () => {
    i18n.changeLanguage("zh");
    expect(chainDisplayName("bsc", i18n.t)).toBe("Binance Smart Chain");
    expect(chainOptionLabel({ name: "bsc", nativeSymbol: "BNB" }, i18n.t)).toBe("Binance Smart Chain (BNB)");

    i18n.changeLanguage("en");
    expect(chainDisplayName("bsc", i18n.t)).toBe("Binance Smart Chain");
    expect(chainOptionLabel({ name: "bsc", nativeSymbol: "BNB" }, i18n.t)).toBe("Binance Smart Chain (BNB)");
  });

  test("falls back to the API chain name for unknown chains", () => {
    i18n.changeLanguage("en");

    expect(chainDisplayName("arbitrum", i18n.t)).toBe("arbitrum");
  });
});
