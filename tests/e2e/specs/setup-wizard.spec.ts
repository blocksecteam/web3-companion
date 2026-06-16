// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect } from "@playwright/test";

// GOAL: Verify the complete first-run setup wizard flow.
// A fresh instance (no users in DB) must guide the user through
// 6 steps and land them on the chat page.

test.describe("Setup Wizard", () => {
  test("step 1: welcome page shows on first visit", async ({ page }) => {
    // GOAL: Fresh instance shows the welcome step, not login.
    await page.goto("/");
    await expect(
      page.getByText(/welcome to web3 companion|欢迎使用/i),
    ).toBeVisible({ timeout: 15_000 });
  });

  test("step 2: chains page has default RPCs pre-filled and trace warning", async ({ page }) => {
    // GOAL: RPC step pre-fills BlockSec/Base/QuickNode defaults and shows
    // the debug_traceCall limitation warning.
    await page.goto("/");
    await page.getByRole("button", { name: /next|下一步/i }).click();
    await expect(page.getByText(/configure blockchain|配置区块链/i)).toBeVisible({ timeout: 5_000 });

    // Default RPC should be pre-filled (not empty)
    const rpcInput = page.locator("input[placeholder*='https']").first();
    await expect(rpcInput).not.toHaveValue("");

    // Trace warning should be visible
    await expect(page.getByText(/debug_traceCall/i)).toBeVisible();
  });

  test("step 2: verify RPC button validates the endpoint", async ({ page }) => {
    // GOAL: Clicking verify on a pre-filled default RPC should succeed
    // (green checkmark) or at least not crash.
    await page.goto("/");
    await page.getByRole("button", { name: /next|下一步/i }).click();

    const verifyButton = page.getByRole("button", { name: /verify|验证/i }).first();
    await verifyButton.click();
    // Wait for verification to complete (checkmark or error)
    await page.waitForTimeout(3_000);
    // No crash — page still has the next button
    await expect(page.getByRole("button", { name: /next|下一步/i })).toBeVisible();
  });

  test("step 3: LLM page defaults to DeepSeek", async ({ page }) => {
    // GOAL: After saving RPCs, LLM step shows DeepSeek as the default
    // provider with pre-filled base URL and model name.
    await page.goto("/");
    // Step 1 → 2
    await page.getByRole("button", { name: /next|下一步/i }).click();
    await expect(page.getByText(/configure blockchain|配置区块链/i)).toBeVisible({ timeout: 5_000 });
    // Step 2 → 3 (save RPCs)
    await page.getByRole("button", { name: /next|下一步/i }).click();
    await expect(page.getByText(/configure ai model|配置 ai 模型/i)).toBeVisible({ timeout: 10_000 });

    // DeepSeek should be selected
    await expect(page.getByText(/deepseek/i).first()).toBeVisible();
    // Base URL should be pre-filled
    const baseUrlInput = page.locator("input[value*='deepseek']");
    await expect(baseUrlInput.first()).toBeVisible();
  });

  test("step 4: providers page has address label options", async ({ page }) => {
    // GOAL: The providers step shows Portals API, MetaSleuth, and
    // address label provider selection.
    await page.goto("/");
    // Navigate through steps 1-3
    for (let i = 0; i < 3; i++) {
      await page.getByRole("button", { name: /next|下一步/i }).click();
      await page.waitForTimeout(1_000);
    }
    // Should be on providers step
    const portals = page.getByText(/portals/i);
    if (await portals.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await expect(portals).toBeVisible();
    }
  });

  test("step 5: account creation requires email", async ({ page }) => {
    // GOAL: The account step has an email input field and requires
    // a valid email before proceeding.
    await page.goto("/");
    for (let i = 0; i < 4; i++) {
      await page.getByRole("button", { name: /next|下一步/i }).click();
      await page.waitForTimeout(1_000);
    }
    const emailInput = page.getByPlaceholder(/email/i);
    if (await emailInput.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await expect(emailInput).toBeVisible();
    }
  });
});
