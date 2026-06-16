// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { MAX_DECIMAL_INPUT_VALUE, validateDecimalInput, validateIntegerInput, validateNameInput } from "@/lib/input-validation";
import { savePasskeyAction, type StoredPasskeyAction } from "@/lib/passkey-action-store";
import { parseRecipientWhitelist } from "@/lib/policy-whitelist";
import { formatUsd } from "@/lib/utils";
import type { PolicyDefinition, PolicyInput, PolicyMode, WalletChallenge } from "@/lib/user-api";

export type PolicyDraft = {
  name: string;
  mode: PolicyMode;
  maxPerTransactionUsd: string;
  dailyTransferLimitUsd: string;
  dailyTradeLimitUsd: string;
  autoSignMaxPerTxUsd: string;
  autoSignDailyLimitUsd: string;
  autoSignMaxCountDay: string;
  recipientWhitelistEnabled: boolean;
  recipientWhitelistAddrs: string;
};

export type PolicyAction =
  | { kind: "create"; input: PolicyInput; challenge?: WalletChallenge; restoredApproved?: boolean; error?: string }
  | { kind: "update"; definitionId: string; input: PolicyInput; challenge?: WalletChallenge; restoredApproved?: boolean; error?: string }
  | { kind: "delete"; definitionId: string; name: string; challenge?: WalletChallenge; restoredApproved?: boolean; error?: string };

export type PolicyDraftFieldErrors = Partial<Record<keyof Pick<
  PolicyDraft,
  | "name"
  | "maxPerTransactionUsd"
  | "dailyTransferLimitUsd"
  | "dailyTradeLimitUsd"
  | "autoSignMaxPerTxUsd"
  | "autoSignDailyLimitUsd"
  | "autoSignMaxCountDay"
>, string>>;

export function emptyDraft(): PolicyDraft {
  return {
    name: "",
    mode: "manual",
    maxPerTransactionUsd: "",
    dailyTransferLimitUsd: "",
    dailyTradeLimitUsd: "",
    autoSignMaxPerTxUsd: "",
    autoSignDailyLimitUsd: "",
    autoSignMaxCountDay: "",
    recipientWhitelistEnabled: false,
    recipientWhitelistAddrs: "",
  };
}

export function draftFromDefinition(definition: PolicyDefinition): PolicyDraft {
  return {
    name: definition.name,
    mode: definition.mode,
    maxPerTransactionUsd: valueText(definition.maxPerTransactionUsd),
    dailyTransferLimitUsd: valueText(definition.dailyTransferLimitUsd),
    dailyTradeLimitUsd: valueText(definition.dailyTradeLimitUsd),
    autoSignMaxPerTxUsd: valueText(definition.autoSignMaxPerTxUsd),
    autoSignDailyLimitUsd: valueText(definition.autoSignDailyLimitUsd),
    autoSignMaxCountDay: valueText(definition.autoSignMaxCountDay),
    recipientWhitelistEnabled: definition.recipientWhitelistEnabled,
    recipientWhitelistAddrs: definition.recipientWhitelistAddrs.join("\n"),
  };
}

export function policyInputFromDraft(draft: PolicyDraft): PolicyInput {
  return preparedPolicyInputFromDraft(draft, addressLines(draft.recipientWhitelistAddrs));
}

export function preparePolicyInputFromDraft(draft: PolicyDraft): {
  input: PolicyInput;
  whitelistText: string;
  error?: { key: string; options: Record<string, string> };
} {
  const parsed = parseRecipientWhitelist(draft.recipientWhitelistAddrs);
  if (draft.recipientWhitelistEnabled && parsed.invalidLines.length > 0) {
    return {
      input: preparedPolicyInputFromDraft(draft, []),
      whitelistText: parsed.addresses.join("\n"),
      error: {
        key: "policies.recipientWhitelistInvalid",
        options: { lines: parsed.invalidLines.join(", ") },
      },
    };
  }
  return {
    input: preparedPolicyInputFromDraft(draft, draft.recipientWhitelistEnabled ? parsed.addresses : []),
    whitelistText: parsed.addresses.join("\n"),
  };
}

export function preparedPolicyInputFromDraft(draft: PolicyDraft, recipientWhitelistAddrs: string[]): PolicyInput {
  const mode = draft.mode;
  return {
    walletId: "",
    name: draft.name.trim(),
    mode,
    maxPerTransactionUsd: toNumber(draft.maxPerTransactionUsd),
    dailyTransferLimitUsd: toNumber(draft.dailyTransferLimitUsd),
    dailyTradeLimitUsd: toNumber(draft.dailyTradeLimitUsd),
    autoSignEnabled: mode === "auto",
    autoSignMaxPerTxUsd: mode === "auto" ? toNumber(draft.autoSignMaxPerTxUsd) : 0,
    autoSignDailyLimitUsd: mode === "auto" ? toNumber(draft.autoSignDailyLimitUsd) : 0,
    autoSignMaxCountDay: mode === "auto" ? Math.trunc(toNumber(draft.autoSignMaxCountDay)) : 0,
    recipientWhitelistEnabled: draft.recipientWhitelistEnabled,
    recipientWhitelistAddrs: draft.recipientWhitelistEnabled ? recipientWhitelistAddrs : [],
  };
}

export function validatePolicyInput(input: PolicyInput, nameRequired: string, autoLimitsRequired: string) {
  if (!input.name) return nameRequired;
  if (input.mode === "auto" && (input.autoSignMaxPerTxUsd <= 0 || input.autoSignDailyLimitUsd <= 0 || input.autoSignMaxCountDay <= 0)) {
    return autoLimitsRequired;
  }
  return "";
}

export function validatePolicyDraftFields(draft: PolicyDraft, t: (key: string, options?: Record<string, number>) => string): PolicyDraftFieldErrors {
  const errors: PolicyDraftFieldErrors = {};
  const nameValidation = validateNameInput(draft.name);
  if (!nameValidation.valid) {
    errors.name = t("policies.nameTooLong", { bytes: nameValidation.bytes, max: nameValidation.maxBytes });
  }

  const decimalError = t("policies.decimalInputInvalid", { max: MAX_DECIMAL_INPUT_VALUE });
  for (const field of ["maxPerTransactionUsd", "dailyTransferLimitUsd", "dailyTradeLimitUsd"] as const) {
    if (!validateDecimalInput(draft[field]).valid) errors[field] = decimalError;
  }

  if (draft.mode === "auto") {
    for (const field of ["autoSignMaxPerTxUsd", "autoSignDailyLimitUsd"] as const) {
      if (!validateDecimalInput(draft[field]).valid) errors[field] = decimalError;
    }
    if (!validateIntegerInput(draft.autoSignMaxCountDay).valid) {
      errors.autoSignMaxCountDay = t("policies.integerInputInvalid", { max: MAX_DECIMAL_INPUT_VALUE });
    }
  }

  return errors;
}

export function hasFieldErrors(errors: PolicyDraftFieldErrors) {
  return Object.values(errors).some(Boolean);
}

export function firstFieldError(errors: PolicyDraftFieldErrors) {
  return Object.values(errors).find(Boolean);
}

export function policyIsDirty(definition: PolicyDefinition, input: PolicyInput) {
  const current = policyInputFromDraft(draftFromDefinition(definition));
  return JSON.stringify(current) !== JSON.stringify(input);
}

export function addressLines(value: string) {
  return value
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

export function toNumber(value: string) {
  const normalized = value.replace(/[$,\s]/g, "");
  const parsed = Number(normalized);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

export function valueText(value: number) {
  return value > 0 ? String(value) : "";
}

export function limitLabel(value: number, locale: string, unlimited: string) {
  return value > 0 ? formatUsd(value, locale) : unlimited;
}

export function messageFrom(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function savePolicyPasskeyAction(userId: string, action: PolicyAction) {
  if (!action.challenge) return;
  let stored: StoredPasskeyAction;
  if (action.kind === "create") {
    stored = { page: "policies", kind: "policy_definition_create", input: action.input, challenge: action.challenge };
  } else if (action.kind === "update") {
    stored = { page: "policies", kind: "policy_definition_update", policyDefinitionId: action.definitionId, input: action.input, challenge: action.challenge };
  } else {
    stored = { page: "policies", kind: "policy_definition_delete", policyDefinitionId: action.definitionId, name: action.name, challenge: action.challenge };
  }
  savePasskeyAction(userId, stored);
}
