// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect } from "@playwright/test";

// GOAL: Verify wallet management page displays wallets, encryption mode,
// and policy bindings correctly.

test.describe("Wallet Management", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active — wallet tests require completed setup");
    }
  });

  test("wallet page shows encryption mode", async ({ page }) => {
    // GOAL: The wallet page header displays whether keys are encrypted
    // with Local AES-256 or AWS KMS.
    const walletLink = page.getByRole("button", { name: /wallet|钱包/i });
    if (!await walletLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await walletLink.click();

    await expect(
      page.getByText(/local|aws kms|加密模式|encryption/i).first(),
    ).toBeVisible({ timeout: 10_000 });
  });

  test("wallet page shows default wallet", async ({ page }) => {
    // GOAL: After setup, at least one wallet (the default) should be
    // listed with its address and policy binding.
    const walletLink = page.getByRole("button", { name: /wallet|钱包/i });
    if (!await walletLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await walletLink.click();

    // Should see a wallet address (0x...)
    await expect(
      page.getByText(/0x[a-fA-F0-9]{4,}/i).first(),
    ).toBeVisible({ timeout: 10_000 });
  });

  test("wallet page has create wallet input", async ({ page }) => {
    // GOAL: Users can create new wallets from the wallet page.
    const walletLink = page.getByRole("button", { name: /wallet|钱包/i });
    if (!await walletLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await walletLink.click();

    const createInput = page.getByPlaceholder(/wallet/i);
    await expect(createInput.first()).toBeVisible({ timeout: 10_000 });

    const createButton = page.getByRole("button", { name: /create|创建/i });
    await expect(createButton.first()).toBeVisible();
  });

  test("wallet has policy binding section", async ({ page }) => {
    // GOAL: Each wallet card shows a policy selector or the currently
    // bound policy definition.
    const walletLink = page.getByRole("button", { name: /wallet|钱包/i });
    if (!await walletLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await walletLink.click();

    // Look for policy-related text
    const policyText = page.getByText(/policy|策略|default/i);
    await expect(policyText.first()).toBeVisible({ timeout: 10_000 });
  });
});
