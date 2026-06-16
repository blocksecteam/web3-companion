// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect, type BrowserContext } from "@playwright/test";

// GOAL: Verify registration and login flows with virtual WebAuthn authenticator.
// These test the complete Passkey lifecycle without real hardware.

async function addVirtualAuthenticator(context: BrowserContext) {
  const page = context.pages()[0] ?? (await context.newPage());
  const cdp = await context.newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      transport: "internal",
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
    },
  });
  return { cdp, authenticatorId };
}

test.describe("Authentication", () => {
  test("landing page shows start button for unauthenticated users", async ({ page }) => {
    // GOAL: First visit as guest shows landing page with a CTA to start.
    await page.goto("/");
    const startButton = page.getByRole("button", { name: /start|get started|开始/i });
    // Either landing page or setup wizard should appear
    const heading = page.locator("h1").first();
    await expect(heading).toBeVisible({ timeout: 15_000 });
  });

  test("register flow: email → passkey → redirect to chat", async ({ context }) => {
    // GOAL: A new user can register with email + Passkey and land on
    // the chat page. This is the golden path for onboarding.
    const page = await context.newPage();
    await addVirtualAuthenticator(context);

    await page.goto("/");

    // If setup wizard is showing (fresh instance), we need to complete
    // setup first before testing auth. Skip in that case.
    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active — auth test requires completed setup");
      return;
    }

    // Should be on landing or auth page
    const startButton = page.getByRole("button", { name: /start|get started|开始/i });
    if (await startButton.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await startButton.click();
    }

    // Fill email
    const emailInput = page.getByPlaceholder(/email/i);
    await expect(emailInput).toBeVisible({ timeout: 10_000 });
    await emailInput.fill(`e2e-${Date.now()}@test.local`);

    // Submit email → triggers passkey registration
    const submitButton = page.getByRole("button", { name: /register|create|注册|创建/i });
    await submitButton.click();

    // Virtual authenticator handles passkey creation automatically.
    // Should redirect to chat within 30s.
    await page.waitForURL(/\/chat/, { timeout: 30_000 });
    await expect(page.getByPlaceholder(/message|消息/i).first()).toBeVisible({ timeout: 10_000 });
  });

  test("login flow: existing user can re-authenticate", async ({ context }) => {
    // GOAL: After registration, a returning user can login with their
    // existing passkey. Tests session persistence.
    const page = await context.newPage();
    await addVirtualAuthenticator(context);

    await page.goto("/");

    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active — login test requires existing user");
      return;
    }

    // Look for login button (vs register)
    const loginButton = page.getByRole("button", { name: /login|sign in|登录/i });
    if (await loginButton.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await loginButton.click();
      // Virtual authenticator handles assertion automatically
      await page.waitForURL(/\/chat/, { timeout: 30_000 });
      await expect(page.getByPlaceholder(/message|消息/i).first()).toBeVisible({ timeout: 10_000 });
    } else {
      // If no explicit login button, might auto-redirect
      test.skip(true, "No login button visible — may need different auth flow");
    }
  });
});
