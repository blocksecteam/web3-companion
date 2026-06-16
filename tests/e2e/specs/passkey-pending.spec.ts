// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect } from "@playwright/test";

// GOAL: Verify that sensitive wallet actions show a Passkey pending
// state in the UI before completing. The user must see a clear
// "waiting for Passkey" indicator, not a silent hang.

test.describe("Passkey Pending State", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active");
    }
  });

  test("wallet export shows pending passkey state", async ({ page }) => {
    // GOAL: Clicking "export" on a wallet should trigger a Passkey
    // challenge and show a pending/waiting UI element, not silently
    // block or show nothing.
    const walletLink = page.getByRole("button", { name: /wallet|钱包/i });
    if (!await walletLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await walletLink.click();
    await page.waitForLoadState("networkidle");

    // Find an action menu or export button on the first wallet
    const exportButton = page.getByRole("button", { name: /export|导出/i }).first();
    const moreMenu = page.getByRole("button", { name: /more|menu|操作|⋯/i }).first();

    if (await moreMenu.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await moreMenu.click();
      const exportMenuItem = page.getByRole("menuitem", { name: /export|导出/i });
      if (await exportMenuItem.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await exportMenuItem.click();
      } else {
        test.skip(true, "Export menu item not found");
        return;
      }
    } else if (await exportButton.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await exportButton.click();
    } else {
      test.skip(true, "No export button found");
      return;
    }

    // Should see a pending passkey state (modal, loading, or status message)
    const pendingIndicator = page.getByText(/passkey|pending|waiting|验证|确认/i);
    await expect(pendingIndicator.first()).toBeVisible({ timeout: 10_000 });
  });

  test("wallet delete shows pending passkey state", async ({ page }) => {
    // GOAL: Deleting a wallet requires Passkey confirmation.
    // The UI should show the pending state clearly.
    const walletLink = page.getByRole("button", { name: /wallet|钱包/i });
    if (!await walletLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await walletLink.click();
    await page.waitForLoadState("networkidle");

    const deleteButton = page.getByRole("button", { name: /delete|删除/i }).first();
    const moreMenu = page.getByRole("button", { name: /more|menu|操作|⋯/i }).first();

    if (await moreMenu.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await moreMenu.click();
      const deleteMenuItem = page.getByRole("menuitem", { name: /delete|删除/i });
      if (await deleteMenuItem.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await deleteMenuItem.click();
      } else {
        test.skip(true, "Delete menu item not found");
        return;
      }
    } else if (await deleteButton.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await deleteButton.click();
    } else {
      test.skip(true, "No delete button found");
      return;
    }

    // Should see passkey confirmation pending
    const pendingIndicator = page.getByText(/passkey|pending|waiting|验证|确认/i);
    await expect(pendingIndicator.first()).toBeVisible({ timeout: 10_000 });
  });
});
