// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { messageFromApiError } from "@/lib/client-utils";
import { savePasskeyAction, type StoredPasskeyAction } from "@/lib/passkey-action-store";
import type { PolicyDefinition, WalletChallenge } from "@/lib/user-api";

export type WalletAction =
  | { type: "delete"; walletId: string; confirmation: string; challenge?: WalletChallenge; restoredApproved?: boolean; error?: string }
  | { type: "export"; walletId: string; challenge?: WalletChallenge; restoredApproved?: boolean; privateKey?: string; saved?: boolean; error?: string }
  | { type: "bind_policy"; walletId: string; policyDefinitionId: string; challenge?: WalletChallenge; restoredApproved?: boolean; error?: string };

export function messageFrom(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function walletDeleteErrorMessage(error: unknown, t: (key: string) => string) {
  return messageFromApiError(error, t("settings.walletDeleteError"), {
    wallet_asset_snapshot_required: t("settings.walletDeleteSnapshotRequired"),
    wallet_asset_snapshot_stale: t("settings.walletDeleteSnapshotStale"),
    wallet_has_assets: t("settings.walletDeleteHasAssets"),
  });
}

export function policyName(definitions: PolicyDefinition[], policyDefinitionId?: string) {
  if (!policyDefinitionId) return "default";
  return definitions.find((definition) => definition.policyDefinitionId === policyDefinitionId)?.name ?? policyDefinitionId;
}

export function saveWalletPasskeyAction(userId: string, action: WalletAction) {
  if (!action.challenge) return;
  let stored: StoredPasskeyAction;
  if (action.type === "delete") {
    stored = {
      page: "wallets",
      kind: "delete",
      walletId: action.walletId,
      confirmation: action.confirmation,
      challenge: action.challenge,
    };
  } else if (action.type === "export") {
    stored = {
      page: "wallets",
      kind: "export",
      walletId: action.walletId,
      challenge: action.challenge,
    };
  } else {
    stored = {
      page: "wallets",
      kind: "bind_policy",
      walletId: action.walletId,
      policyDefinitionId: action.policyDefinitionId,
      challenge: action.challenge,
    };
  }
  savePasskeyAction(userId, stored);
}
