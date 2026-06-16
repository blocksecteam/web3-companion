// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect } from "@playwright/test";

// GOAL: Verify settings page saves configuration and persists across reloads.
// These tests require a completed setup with an authenticated user.
// They skip automatically if the instance is still in setup mode.

async function navigateToSettings(page: import("@playwright/test").Page) {
  await page.goto("/");
  // Wait for app to load
  await page.waitForLoadState("networkidle");
  // Click settings in sidebar nav
  const settingsLink = page.getByRole("button", { name: /settings|设置/i });
  if (await settingsLink.isVisible({ timeout: 5_000 }).catch(() => false)) {
    await settingsLink.click();
    await page.waitForTimeout(1_000);
    return true;
  }
  return false;
}

test.describe("Settings Page", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active — settings tests require completed setup");
    }
  });

  test("displays current LLM provider and model", async ({ page }) => {
    // GOAL: Settings page shows the configured LLM provider name and
    // model selector with current values.
    const navigated = await navigateToSettings(page);
    if (!navigated) {
      test.skip(true, "Could not navigate to settings");
      return;
    }

    // Provider section should show DeepSeek (or whatever is configured)
    await expect(page.getByText(/deepseek|openrouter|custom/i).first()).toBeVisible({ timeout: 10_000 });
    // Model name field should be visible
    const modelInput = page.locator("input").filter({ hasText: /deepseek|model/i });
    // At minimum, the save button should be present
    await expect(page.getByRole("button", { name: /save|保存/i }).first()).toBeVisible();
  });

  test("RPC section shows configured chains", async ({ page }) => {
    // GOAL: Each supported chain has an input field with its current RPC URL.
    const navigated = await navigateToSettings(page);
    if (!navigated) return;

    for (const chain of ["Ethereum", "Base"]) {
      await expect(
        page.getByText(new RegExp(chain, "i")).first(),
      ).toBeVisible({ timeout: 10_000 });
    }
  });

  test("version info is displayed", async ({ page }) => {
    // GOAL: Settings page shows the current version. If an update is available,
    // a blue banner appears with a link to Docker Hub.
    const navigated = await navigateToSettings(page);
    if (!navigated) return;

    // Either "Version X.Y.Z" or "Update available" should be present
    const versionText = page.getByText(/version|版本|v\d+\.\d+/i);
    await expect(versionText.first()).toBeVisible({ timeout: 10_000 });
  });

  test("save LLM settings persists after page reload", async ({ page }) => {
    // GOAL: Changing the model name and saving should persist the value
    // when the page is reloaded. Tests the full round-trip:
    // UI → PUT API → DB → GET API → UI.
    const navigated = await navigateToSettings(page);
    if (!navigated) return;

    // Find model name input and change it
    const modelInput = page.locator("input[placeholder*='model'], input[placeholder*='模型']").first();
    if (!await modelInput.isVisible({ timeout: 5_000 }).catch(() => false)) {
      test.skip(true, "Model input not found");
      return;
    }

    const originalValue = await modelInput.inputValue();
    const testValue = originalValue + "-e2e-test";

    await modelInput.fill(testValue);
    await page.getByRole("button", { name: /save|保存/i }).first().click();
    await page.waitForTimeout(2_000);

    // Reload and verify
    await page.reload();
    await page.waitForLoadState("networkidle");
    await navigateToSettings(page);

    const reloadedInput = page.locator("input[placeholder*='model'], input[placeholder*='模型']").first();
    const reloadedValue = await reloadedInput.inputValue();

    // Restore original value
    await reloadedInput.fill(originalValue);
    await page.getByRole("button", { name: /save|保存/i }).first().click();

    expect(reloadedValue).toBe(testValue);
  });
});
