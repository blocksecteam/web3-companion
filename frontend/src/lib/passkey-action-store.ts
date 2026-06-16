// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { PolicyInput, WalletChallenge } from "@/lib/user-api";

export type StoredPasskeyAction =
  | {
      page: "settings";
      kind: "delete";
      walletId: string;
      confirmation: string;
      challenge: WalletChallenge;
    }
  | {
      page: "settings";
      kind: "export";
      walletId: string;
      challenge: WalletChallenge;
    }
  | {
      page: "wallets";
      kind: "delete";
      walletId: string;
      confirmation: string;
      challenge: WalletChallenge;
    }
  | {
      page: "wallets";
      kind: "export";
      walletId: string;
      challenge: WalletChallenge;
    }
  | {
      page: "wallets";
      kind: "bind_policy";
      walletId: string;
      policyDefinitionId: string;
      challenge: WalletChallenge;
    }
  | {
      page: "policies";
      kind: "policy";
      policyId: string;
      input: PolicyInput;
      challenge: WalletChallenge;
    }
  | {
      page: "policies";
      kind: "policy_definition_create";
      input: PolicyInput;
      challenge: WalletChallenge;
    }
  | {
      page: "policies";
      kind: "policy_definition_update";
      policyDefinitionId: string;
      input: PolicyInput;
      challenge: WalletChallenge;
    }
  | {
      page: "policies";
      kind: "policy_definition_delete";
      policyDefinitionId: string;
      name: string;
      challenge: WalletChallenge;
    };

export type StoredPasskeyPage = StoredPasskeyAction["page"];
export type RestorableChallengeStatus = "pending" | "approved" | "timeout" | "cancelled" | "used" | "completed";
export type RestoreDecision = "restore_modal" | "auto_confirm" | "clear_and_restart";

const storagePrefix = "web3-companion:pending-passkey";

export function savePasskeyAction(userId: string, action: StoredPasskeyAction) {
  if (typeof sessionStorage === "undefined") return;
  sessionStorage.setItem(storageKey(userId, action.page), JSON.stringify(action));
}

export function loadPasskeyAction(userId: string, page: StoredPasskeyPage) {
  if (typeof sessionStorage === "undefined") return null;
  const raw = sessionStorage.getItem(storageKey(userId, page));
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (isStoredPasskeyAction(parsed, page)) return parsed;
  } catch {
    // Fall through to clear invalid state.
  }
  clearPasskeyAction(userId, page);
  return null;
}

export function clearPasskeyAction(userId: string, page: StoredPasskeyPage) {
  if (typeof sessionStorage === "undefined") return;
  sessionStorage.removeItem(storageKey(userId, page));
}

export function restoreDecisionForStatus(status: string): RestoreDecision {
  if (status === "pending") return "restore_modal";
  if (status === "approved") return "auto_confirm";
  return "clear_and_restart";
}

function storageKey(userId: string, page: StoredPasskeyPage) {
  return `${storagePrefix}:${userId}:${page}`;
}

function isStoredPasskeyAction(value: unknown, page: StoredPasskeyPage): value is StoredPasskeyAction {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  if (record.page !== page) return false;
  if (!isChallenge(record.challenge)) return false;
  if (page === "settings" || page === "wallets") {
    if (record.kind === "delete") {
      return typeof record.walletId === "string" && typeof record.confirmation === "string";
    }
    if (record.kind === "export") {
      return typeof record.walletId === "string";
    }
    if (page === "wallets" && record.kind === "bind_policy") {
      return typeof record.walletId === "string" && typeof record.policyDefinitionId === "string";
    }
    return false;
  }
  if (record.kind === "policy") {
    return typeof record.policyId === "string" && isPolicyInput(record.input);
  }
  if (record.kind === "policy_definition_create") {
    return isPolicyInput(record.input);
  }
  if (record.kind === "policy_definition_update") {
    return typeof record.policyDefinitionId === "string" && isPolicyInput(record.input);
  }
  if (record.kind === "policy_definition_delete") {
    return typeof record.policyDefinitionId === "string" && typeof record.name === "string";
  }
  return false;
}

function isChallenge(value: unknown): value is WalletChallenge {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return typeof record.challengeId === "string" && record.challengeId.trim().length > 0;
}

function isPolicyInput(value: unknown): value is PolicyInput {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return typeof record.walletId === "string" && typeof record.name === "string" && (record.mode === "manual" || record.mode === "auto");
}
