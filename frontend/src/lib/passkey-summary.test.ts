// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import i18n from "@/i18n";
import { localizePasskeySummary } from "@/lib/passkey-summary";
import type { PasskeyChallengeSummary } from "@/lib/user-api";

describe("localizePasskeySummary", () => {
  test("translates backend operation titles and detail labels", () => {
    i18n.changeLanguage("zh");
    const summary: PasskeyChallengeSummary = {
      operation: "sign",
      title: "Approve signature",
      details: [
        { label: "Wallet", value: "wallet_main" },
        { label: "Amount USD", value: "12.50" },
        { label: "Security verdict", value: "pass" },
      ],
    };

    expect(localizePasskeySummary(summary, i18n.t.bind(i18n))).toEqual({
      operation: "sign",
      title: "批准签名",
      details: [
        { label: "钱包", value: "wallet_main" },
        { label: "USD 金额", value: "12.50" },
        { label: "安全结论", value: "pass" },
      ],
    });
  });

  test("every operation string the backend emits has a localized title (regression: backend named `bind_wallet_policy` but the map had `bind_policy`; `security_override` was missing entirely; the policy_definition_* family was missing — all three classes surfaced the raw English enum verbatim in the modal title)", () => {
    // The source of truth: `grep '"operation":' internal/` returns this
    // exact set. Adding any operation to the backend without extending
    // this list (and the map / i18n keys) will make the modal title fall
    // back to the raw enum for that operation.
    const backendOperations = [
      "delete_wallet",
      "export_wallet",
      "bind_wallet_policy",
      "create_policy_definition",
      "update_policy_definition",
      "delete_policy_definition",
      "delegate_wallet",
      "security_override",
      "sign",
    ];
    for (const lang of ["zh", "en"]) {
      i18n.changeLanguage(lang);
      for (const operation of backendOperations) {
        const summary: PasskeyChallengeSummary = { operation, title: operation, details: [] };
        const localized = localizePasskeySummary(summary, i18n.t.bind(i18n));
        expect(localized?.title, `${lang}: ${operation}`).not.toBe(operation);
        expect(localized?.title.length, `${lang}: ${operation}`).toBeGreaterThan(0);
      }
    }
  });

  test("keeps unknown backend labels readable", () => {
    i18n.changeLanguage("zh");
    const summary: PasskeyChallengeSummary = {
      operation: "custom_operation",
      title: "Custom operation",
      details: [{ label: "Protocol", value: "Aave" }],
    };

    expect(localizePasskeySummary(summary, i18n.t.bind(i18n))).toEqual(summary);
  });
});
