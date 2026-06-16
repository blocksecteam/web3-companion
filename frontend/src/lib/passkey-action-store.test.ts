// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { beforeEach, describe, expect, test } from "bun:test";
import {
  clearPasskeyAction,
  loadPasskeyAction,
  restoreDecisionForStatus,
  savePasskeyAction,
  type StoredPasskeyAction,
} from "@/lib/passkey-action-store";

const challenge = {
  status: "pending_user_auth" as const,
  challengeId: "ch_test",
  signUrl: "/confirm/ch_test",
};

const memoryStorage = new Map<string, string>();
Object.defineProperty(globalThis, "sessionStorage", {
  configurable: true,
  value: {
    clear: () => memoryStorage.clear(),
    getItem: (key: string) => memoryStorage.get(key) ?? null,
    removeItem: (key: string) => {
      memoryStorage.delete(key);
    },
    setItem: (key: string, value: string) => {
      memoryStorage.set(key, value);
    },
  },
});

describe("passkey action persistence", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  test("saves and restores a settings delete action for the same user and page", () => {
    const action: StoredPasskeyAction = {
      page: "settings",
      kind: "delete",
      walletId: "wallet_main",
      confirmation: "wallet_main",
      challenge,
    };

    savePasskeyAction("user_1", action);

    expect(loadPasskeyAction("user_1", "settings")).toEqual(action);
    expect(loadPasskeyAction("user_2", "settings")).toBeNull();
    expect(loadPasskeyAction("user_1", "policies")).toBeNull();
  });

  test("clears invalid stored payloads instead of restoring stale UI state", () => {
    sessionStorage.setItem("web3-companion:pending-passkey:user_1:settings", JSON.stringify({ page: "settings", kind: "delete" }));

    expect(loadPasskeyAction("user_1", "settings")).toBeNull();
    expect(sessionStorage.getItem("web3-companion:pending-passkey:user_1:settings")).toBeNull();
  });

  test("maps refreshed challenge status to the correct recovery behavior", () => {
    expect(restoreDecisionForStatus("pending")).toBe("restore_modal");
    expect(restoreDecisionForStatus("approved")).toBe("auto_confirm");
    expect(restoreDecisionForStatus("timeout")).toBe("clear_and_restart");
    expect(restoreDecisionForStatus("cancelled")).toBe("clear_and_restart");
    expect(restoreDecisionForStatus("used")).toBe("clear_and_restart");
  });

  test("clears stored action explicitly", () => {
    savePasskeyAction("user_1", {
      page: "settings",
      kind: "export",
      walletId: "wallet_main",
      challenge,
    });

    clearPasskeyAction("user_1", "settings");

    expect(loadPasskeyAction("user_1", "settings")).toBeNull();
  });

  test("saves and restores policy definition actions", () => {
    const action: StoredPasskeyAction = {
      page: "policies",
      kind: "policy_definition_update",
      policyDefinitionId: "pdef_1",
      input: {
        walletId: "",
        name: "desk",
        mode: "manual",
        maxPerTransactionUsd: 0,
        dailyTransferLimitUsd: 0,
        dailyTradeLimitUsd: 0,
        autoSignEnabled: false,
        autoSignMaxPerTxUsd: 0,
        autoSignDailyLimitUsd: 0,
        autoSignMaxCountDay: 0,
        recipientWhitelistEnabled: false,
        recipientWhitelistAddrs: [],
      },
      challenge,
    };

    savePasskeyAction("user_1", action);

    expect(loadPasskeyAction("user_1", "policies")).toEqual(action);
  });

  test("saves and restores policy delete display names", () => {
    const action: StoredPasskeyAction = {
      page: "policies",
      kind: "policy_definition_delete",
      policyDefinitionId: "pdef_1",
      name: "Trading desk",
      challenge,
    };

    savePasskeyAction("user_1", action);

    expect(loadPasskeyAction("user_1", "policies")).toEqual(action);
  });

  test("clears policy delete actions without display names", () => {
    sessionStorage.setItem(
      "web3-companion:pending-passkey:user_1:policies",
      JSON.stringify({
        page: "policies",
        kind: "policy_definition_delete",
        policyDefinitionId: "pdef_1",
        challenge,
      }),
    );

    expect(loadPasskeyAction("user_1", "policies")).toBeNull();
    expect(sessionStorage.getItem("web3-companion:pending-passkey:user_1:policies")).toBeNull();
  });

  test("saves and restores wallet policy binding actions", () => {
    const action: StoredPasskeyAction = {
      page: "wallets",
      kind: "bind_policy",
      walletId: "wallet_main",
      policyDefinitionId: "pdef_1",
      challenge,
    };

    savePasskeyAction("user_1", action);

    expect(loadPasskeyAction("user_1", "wallets")).toEqual(action);
  });
});
