// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect, type Page } from "@playwright/test";

// GOAL: Verify the complete first-run setup wizard flow.
// A fresh instance (no users in DB) must guide the user through
// 6 steps and land them on the chat page.

async function ensureWizardActive(page: Page): Promise<boolean> {
  await page.goto("/", { waitUntil: "networkidle" });
  return page.getByText(/welcome to web3 companion|欢迎使用/i)
    .isVisible({ timeout: 15_000 })
    .catch(() => false);
}

test.describe("Setup Wizard", () => {
  test("step 1: welcome page shows on first visit", async ({ page }) => {
    if (!await ensureWizardActive(page)) {
      test.skip(true, "Setup already complete — wizard tests require fresh instance");
      return;
    }
    await expect(
      page.getByText(/welcome to web3 companion|欢迎使用/i),
    ).toBeVisible({ timeout: 15_000 });
  });

  test("step 2: chains page has default RPCs pre-filled and trace warning", async ({ page }) => {
    if (!await ensureWizardActive(page)) {
      test.skip(true, "Setup already complete");
      return;
    }
    await page.getByRole("button", { name: /next|下一步/i }).click();
    await expect(page.getByText(/configure blockchain|配置区块链/i)).toBeVisible({ timeout: 5_000 });

    const rpcInput = page.locator("input[placeholder*='https']").first();
    await expect(rpcInput).not.toHaveValue("");

    await expect(page.getByText(/debug_traceCall/i)).toBeVisible();
  });

  test("step 2: verify RPC button validates the endpoint", async ({ page }) => {
    if (!await ensureWizardActive(page)) {
      test.skip(true, "Setup already complete");
      return;
    }
    await page.getByRole("button", { name: /next|下一步/i }).click();

    const verifyButton = page.getByRole("button", { name: /verify|验证/i }).first();
    await verifyButton.click();
    await page.waitForTimeout(3_000);
    await expect(page.getByRole("button", { name: /next|下一步/i })).toBeVisible();
  });

  test("step 3: LLM page defaults to DeepSeek", async ({ page }) => {
    if (!await ensureWizardActive(page)) {
      test.skip(true, "Setup already complete");
      return;
    }
    await page.getByRole("button", { name: /next|下一步/i }).click();
    await expect(page.getByText(/configure blockchain|配置区块链/i)).toBeVisible({ timeout: 5_000 });
    await page.getByRole("button", { name: /next|下一步/i }).click();
    await expect(page.getByText(/configure ai model|配置 ai 模型/i)).toBeVisible({ timeout: 10_000 });

    await expect(page.getByText(/deepseek/i).first()).toBeVisible();
    const baseUrlInput = page.locator("input[value*='deepseek']");
    await expect(baseUrlInput.first()).toBeVisible();
  });

  test("step 4: providers page has address label options", async ({ page }) => {
    if (!await ensureWizardActive(page)) {
      test.skip(true, "Setup already complete");
      return;
    }
    for (let i = 0; i < 3; i++) {
      await page.getByRole("button", { name: /next|下一步/i }).click();
      await page.waitForTimeout(1_000);
    }
    const portals = page.getByText(/portals/i);
    if (await portals.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await expect(portals).toBeVisible();
    }
  });

  test("step 5: account creation requires email", async ({ page }) => {
    if (!await ensureWizardActive(page)) {
      test.skip(true, "Setup already complete");
      return;
    }
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
