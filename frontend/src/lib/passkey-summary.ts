// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { PasskeyChallengeSummary } from "@/lib/user-api";

type TranslationFn = (key: string, options?: Record<string, unknown>) => string;

// Maps every operation string the backend emits (via `payload.operation`
// in passkey challenge payloads, or `ch.Type` as fallback) to a key under
// `passkeyModal.operations.*` in i18n.ts. To find the source of truth on
// the backend side, grep `internal/` for `"operation":` — every match must
// have a corresponding entry here, otherwise the challenge title falls
// back to the raw English enum string in the Passkey modal.
//
// English-title keys (e.g. "Delete wallet") are kept as fallbacks for code
// paths that populate the localized title instead of `payload.operation`.
// New backend emissions should always set `payload.operation` to the
// snake_case enum.
const operationKeyByValue: Record<string, string> = {
  delete_wallet: "delete_wallet",
  "Delete wallet": "delete_wallet",
  export_wallet: "export_wallet",
  "Export wallet": "export_wallet",
  bind_wallet_policy: "bind_wallet_policy",
  "Bind wallet policy": "bind_wallet_policy",
  create_policy_definition: "create_policy_definition",
  "Create policy definition": "create_policy_definition",
  update_policy_definition: "update_policy_definition",
  "Update policy definition": "update_policy_definition",
  delete_policy_definition: "delete_policy_definition",
  "Delete policy definition": "delete_policy_definition",
  sign: "sign",
  "Approve signature": "sign",
  delegate_wallet: "delegate_wallet",
  security_override: "security_override",
  "Security override": "security_override",
  confirm_operation: "confirm_operation",
  "Confirm operation": "confirm_operation",
};

const detailLabelKeyByValue: Record<string, string> = {
  Wallet: "wallet",
  Policy: "policy",
  Type: "type",
  Chain: "chain",
  To: "to",
  "Amount USD": "amountUsd",
  Reason: "reason",
  "Security verdict": "securityVerdict",
  "Security warning": "securityWarning",
  "Security block": "securityBlock",
  "Policy name": "policyName",
  Mode: "mode",
  Challenge: "challenge",
};

export function localizePasskeySummary(summary: PasskeyChallengeSummary | undefined, t: TranslationFn) {
  if (!summary) return undefined;
  return {
    ...summary,
    title: localizeOperationTitle(summary, t),
    details: summary.details.map((detail) => ({
      ...detail,
      label: localizeDetailLabel(detail.label, t),
    })),
  };
}

function localizeOperationTitle(summary: PasskeyChallengeSummary, t: TranslationFn) {
  const key = operationKeyByValue[summary.operation] || operationKeyByValue[summary.title];
  if (!key) return summary.title;
  return t(`passkeyModal.operations.${key}`);
}

function localizeDetailLabel(label: string, t: TranslationFn) {
  const key = detailLabelKeyByValue[label];
  return key ? t(`passkeyModal.detailLabels.${key}`) : label;
}
