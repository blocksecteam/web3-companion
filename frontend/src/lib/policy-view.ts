// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { formatUsd } from "@/lib/utils";
import type { PolicyDefinition, PolicyMode } from "@/lib/user-api";

export type PolicyDefinitionRowModel = {
  mode: PolicyMode;
  modeLabelKey: "policies.modeManual" | "policies.modeAuto";
  modeRowKey: "policies.modeManualRow" | "policies.modeAutoRow";
  defaultBadge: boolean;
  whitelistCount: number;
  limitLabels: {
    maxPerTransaction: string;
    dailyTransfer: string;
    dailyTrade: string;
    autoSignMaxPerTx: string;
    autoSignDailyLimit: string;
    autoSignMaxCountDay: string;
  };
};

export function policyDefinitionRowModel(definition: PolicyDefinition, locale: string, unlimited: string): PolicyDefinitionRowModel {
  const auto = definition.mode === "auto" && definition.autoSignEnabled;
  return {
    mode: auto ? "auto" : "manual",
    modeLabelKey: auto ? "policies.modeAuto" : "policies.modeManual",
    modeRowKey: auto ? "policies.modeAutoRow" : "policies.modeManualRow",
    defaultBadge: definition.isDefault,
    whitelistCount: definition.recipientWhitelistEnabled ? definition.recipientWhitelistAddrs.length : 0,
    limitLabels: {
      maxPerTransaction: limitLabel(definition.maxPerTransactionUsd, locale, unlimited),
      dailyTransfer: limitLabel(definition.dailyTransferLimitUsd, locale, unlimited),
      dailyTrade: limitLabel(definition.dailyTradeLimitUsd, locale, unlimited),
      autoSignMaxPerTx: auto ? limitLabel(definition.autoSignMaxPerTxUsd, locale, unlimited) : "—",
      autoSignDailyLimit: auto ? limitLabel(definition.autoSignDailyLimitUsd, locale, unlimited) : "—",
      autoSignMaxCountDay: auto ? (definition.autoSignMaxCountDay > 0 ? String(definition.autoSignMaxCountDay) : unlimited) : "—",
    },
  };
}

function limitLabel(value: number, locale: string, unlimited: string) {
  return value > 0 ? formatUsd(value, locale) : unlimited;
}
