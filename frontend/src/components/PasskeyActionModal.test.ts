// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const modalSource = readFileSync(new URL("./PasskeyActionModal.tsx", import.meta.url), "utf8");
const walletPageSource = readFileSync(new URL("../pages/WalletsPage.tsx", import.meta.url), "utf8");
const walletModalSource = readFileSync(new URL("../pages/wallets/wallet-ui.tsx", import.meta.url), "utf8");
const walletSource = [
  walletPageSource,
  walletModalSource,
].join("\n");
const policySource = [
  readFileSync(new URL("../pages/PolicyPage.tsx", import.meta.url), "utf8"),
  readFileSync(new URL("../pages/policies/policy-ui.tsx", import.meta.url), "utf8"),
].join("\n");
const chatSource = readFileSync(new URL("./chat/ChatWorkspace.tsx", import.meta.url), "utf8");

describe("streamlined passkey actions", () => {
  test("PasskeyActionModal completes the business action immediately after WebAuthn approval", () => {
    expect(modalSource).toContain("confirmAction(challengeId)");
    expect(modalSource).toContain("finishAction(fresh.challengeId)");
    expect(modalSource).toContain("setState(\"confirming\")");
    expect(modalSource).not.toContain("continue");
  });

  test("modal auto-fires the challenge request on mount when caller did not supply one — collapses request→approve into a single click for policy / wallet flows (regression: dual-click bug where the second click finally surfaced the Passkey QR)", () => {
    // The single-flight ref prevents StrictMode double-mount from
    // creating two backend challenges. Both halves are load-bearing
    // and must travel together.
    expect(modalSource).toContain("autoRequestedRef");
    expect(modalSource).toContain("void request();");
    expect(modalSource).toContain("if (autoRequestedRef.current) return;");
  });

  test("wallet and policy pages use the unified modal instead of secondary confirmation cards", () => {
    expect(walletSource).toContain("<PasskeyActionModal");
    expect(policySource).toContain("<PasskeyActionModal");
    expect(walletSource).not.toContain("settings.passkeyConfirmFirst");
    expect(policySource).not.toContain("policies.passkeyConfirmFirst");
    expect(walletSource).not.toContain("settings.passkeyModalOpen");
    expect(policySource).not.toContain("policies.passkeyModalOpen");
  });

  test("wallet policy binding creates its challenge before opening the passkey modal", () => {
    expect(walletPageSource).toContain("await requestWalletPolicyBinding(user, wallet.walletId, policyDefinitionId)");
    expect(walletPageSource).toContain("challenge");
    expect(walletModalSource).not.toContain("requestWalletPolicyBinding");
  });

  test("chat passkey approval resumes the companion with a localized user message", () => {
    expect(chatSource).toContain("sendSyntheticUserMessage(");
    expect(chatSource).toContain("chat.passkeyAuthenticatedMessageWithChallenge");
    expect(chatSource).toContain("challengeId: chatPasskeyRequest.challengeId");
    expect(chatSource).toContain("onSuccess={() =>");
  });
});
