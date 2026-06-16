// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { policyDefinitionRowModel } from "@/lib/policy-view";
import type { PolicyDefinition } from "@/lib/user-api";

const baseDefinition: PolicyDefinition = {
  policyDefinitionId: "pdef_1",
  currentVersionId: "pver_1",
  name: "desk policy",
  isDefault: false,
  version: 3,
  mode: "manual",
  maxPerTransactionUsd: 500,
  dailyTransferLimitUsd: 1_000,
  dailyTradeLimitUsd: 2_000,
  autoSignEnabled: false,
  autoSignMaxPerTxUsd: 0,
  autoSignDailyLimitUsd: 0,
  autoSignMaxCountDay: 0,
  recipientWhitelistEnabled: false,
  recipientWhitelistAddrs: [],
  updatedAt: "2026-05-16T12:00:00Z",
};

describe("policyDefinitionRowModel", () => {
  test("summarizes manual policies for a list row", () => {
    const row = policyDefinitionRowModel({ ...baseDefinition, isDefault: true }, "en-US", "Unlimited");

    expect(row.mode).toBe("manual");
    expect(row.modeLabelKey).toBe("policies.modeManual");
    expect(row.modeRowKey).toBe("policies.modeManualRow");
    expect(row.defaultBadge).toBe(true);
    expect(row.limitLabels.maxPerTransaction).toBe("$500.00");
    expect(row.limitLabels.autoSignMaxPerTx).toBe("—");
    expect(row.limitLabels.autoSignDailyLimit).toBe("—");
    expect(row.limitLabels.autoSignMaxCountDay).toBe("—");
  });

  test("summarizes auto-sign budgets and whitelist state for a list row", () => {
    const row = policyDefinitionRowModel(
      {
        ...baseDefinition,
        mode: "auto",
        autoSignEnabled: true,
        autoSignMaxPerTxUsd: 25,
        autoSignDailyLimitUsd: 100,
        autoSignMaxCountDay: 4,
        recipientWhitelistEnabled: true,
        recipientWhitelistAddrs: ["0xabc", "0xdef"],
      },
      "en-US",
      "Unlimited",
    );

    expect(row.mode).toBe("auto");
    expect(row.modeLabelKey).toBe("policies.modeAuto");
    expect(row.modeRowKey).toBe("policies.modeAutoRow");
    expect(row.whitelistCount).toBe(2);
    expect(row.limitLabels.autoSignMaxPerTx).toBe("$25.00");
    expect(row.limitLabels.autoSignDailyLimit).toBe("$100.00");
    expect(row.limitLabels.autoSignMaxCountDay).toBe("4");
  });
});
