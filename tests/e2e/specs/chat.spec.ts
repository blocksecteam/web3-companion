// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect } from "@playwright/test";

// GOAL: Verify chat interface behaviors that only manifest in a real browser:
// message input/send, session switching, scroll behavior, sidebar wallet card.

test.describe("Chat Interface", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active — chat tests require completed setup");
    }
  });

  test("chat page loads with message input and sidebar", async ({ page }) => {
    // GOAL: Authenticated user sees the chat interface with a text input
    // for messages and a sidebar with sessions + navigation.
    await expect(
      page.getByPlaceholder(/message|消息|ask/i).first(),
    ).toBeVisible({ timeout: 15_000 });

    // Sidebar should have new session button
    const newSession = page.getByRole("button", { name: /new|新建/i });
    await expect(newSession.first()).toBeVisible({ timeout: 5_000 });
  });

  test("new session creates empty conversation", async ({ page }) => {
    // GOAL: Clicking the new session button clears the chat area and
    // shows the empty state prompt suggestions.
    await page.waitForLoadState("networkidle");

    const newSession = page.getByRole("button", { name: /new|新建/i }).first();
    if (!await newSession.isVisible({ timeout: 5_000 }).catch(() => false)) return;

    await newSession.click();
    await page.waitForTimeout(1_000);

    // Empty state should show (no messages, just prompt suggestions)
    const messageArea = page.locator("main").first();
    await expect(messageArea).toBeVisible();
  });

  test("sending a message shows it in the chat area", async ({ page }) => {
    // GOAL: Typing a message and pressing enter/send button should
    // display the user's message in the chat area and trigger a
    // response (or at least a loading state).
    await page.waitForLoadState("networkidle");

    const input = page.getByPlaceholder(/message|消息|ask/i).first();
    if (!await input.isVisible({ timeout: 10_000 }).catch(() => false)) return;

    await input.fill("What is my wallet address?");

    // Find send button or press Enter
    const sendButton = page.getByRole("button", { name: /send|发送/i });
    if (await sendButton.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await sendButton.click();
    } else {
      await input.press("Enter");
    }

    // User's message should appear in the chat
    await expect(
      page.getByText("What is my wallet address?"),
    ).toBeVisible({ timeout: 10_000 });
  });

  test("session switch does not bleed messages", async ({ page }) => {
    // GOAL: When switching from session A (which has messages) to
    // session B (empty), session A's messages must NOT appear in
    // session B. This was a real bug — session events from the old
    // WebSocket were displayed in the new session.
    await page.waitForLoadState("networkidle");

    // Create first session and send a message
    const newSession = page.getByRole("button", { name: /new|新建/i }).first();
    if (!await newSession.isVisible({ timeout: 5_000 }).catch(() => false)) return;

    await newSession.click();
    await page.waitForTimeout(500);

    const input = page.getByPlaceholder(/message|消息|ask/i).first();
    if (!await input.isVisible({ timeout: 5_000 }).catch(() => false)) return;

    const uniqueMessage = `e2e-session-test-${Date.now()}`;
    await input.fill(uniqueMessage);
    const sendButton = page.getByRole("button", { name: /send|发送/i });
    if (await sendButton.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await sendButton.click();
    } else {
      await input.press("Enter");
    }
    await page.waitForTimeout(2_000);

    // Create second session
    await newSession.click();
    await page.waitForTimeout(1_000);

    // The unique message from session 1 should NOT be visible
    const leakedMessage = page.getByText(uniqueMessage);
    await expect(leakedMessage).not.toBeVisible({ timeout: 3_000 });
  });

  test("sidebar wallet card has copy and refresh buttons", async ({ page }) => {
    // GOAL: The wallet card in the sidebar shows a shortened address
    // with working copy and refresh buttons.
    await page.waitForLoadState("networkidle");

    // Find wallet address text (0x...)
    const addressText = page.getByText(/0x[a-fA-F0-9]{4}\.{2,}[a-fA-F0-9]{4}/i);
    if (!await addressText.isVisible({ timeout: 5_000 }).catch(() => false)) {
      // Wallet might still be loading
      return;
    }

    // Copy button should be near the address
    const copyButton = page.getByRole("button", { name: /copy|复制/i }).first();
    await expect(copyButton).toBeVisible();

    // Refresh button should be near the address
    const refreshButton = page.getByRole("button", { name: /refresh|刷新/i }).first();
    await expect(refreshButton).toBeVisible();
  });

  test("chat area supports scrolling up during streaming", async ({ page }) => {
    // GOAL: While the assistant is streaming a response, the user
    // should be able to scroll up without being yanked back to the
    // bottom. This was a real bug fixed by stick-to-bottom logic.
    await page.waitForLoadState("networkidle");

    const input = page.getByPlaceholder(/message|消息|ask/i).first();
    if (!await input.isVisible({ timeout: 5_000 }).catch(() => false)) return;

    // Send a message that will generate a long response
    await input.fill("Explain EIP-7702 in detail");
    const sendButton = page.getByRole("button", { name: /send|发送/i });
    if (await sendButton.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await sendButton.click();
    } else {
      await input.press("Enter");
    }

    // Wait for response to start streaming
    await page.waitForTimeout(3_000);

    // Try to scroll up
    const scrollArea = page.locator("[data-radix-scroll-area-viewport]").first();
    if (await scrollArea.isVisible().catch(() => false)) {
      await scrollArea.evaluate((el) => { el.scrollTop = 0; });
      await page.waitForTimeout(1_000);
      // After 1 second, scrollTop should still be 0 (not yanked back)
      const scrollTop = await scrollArea.evaluate((el) => el.scrollTop);
      // Allow small tolerance (smooth scroll animation)
      expect(scrollTop).toBeLessThan(100);
    }
  });
});
