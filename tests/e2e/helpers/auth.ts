// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { BrowserContext, Page } from "@playwright/test";

/**
 * Enable Playwright's virtual WebAuthn authenticator on the given context.
 * This allows Passkey registration and login without real hardware.
 */
export async function enableVirtualAuthenticator(context: BrowserContext) {
  const cdp = await context.newCDPSession(await context.newPage());
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

/**
 * Register a new account via the setup wizard or login page.
 * Assumes virtual authenticator is already enabled.
 */
export async function registerAccount(page: Page, email: string) {
  await page.getByPlaceholder(/email/i).fill(email);
  await page.getByRole("button", { name: /create|register|注册/i }).click();
  // Passkey creation is handled by virtual authenticator automatically
  await page.waitForURL(/\/chat/, { timeout: 30_000 });
}

/**
 * Wait for the app to be ready (not showing loading spinner).
 */
export async function waitForAppReady(page: Page) {
  await page.waitForLoadState("networkidle");
}
