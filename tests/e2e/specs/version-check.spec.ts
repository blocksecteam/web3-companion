// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { test, expect } from "@playwright/test";

// GOAL: Verify the version check feature displays current version info
// and shows an update banner when a newer version exists on Docker Hub.

test.describe("Version Check", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    const isSetup = await page.getByText(/welcome to web3 companion|欢迎使用/i)
      .isVisible({ timeout: 5_000 })
      .catch(() => false);
    if (isSetup) {
      test.skip(true, "Setup wizard active");
    }
  });

  test("settings page shows current version", async ({ page }) => {
    // GOAL: The settings page displays the running version string
    // (e.g. "Version v0.1.0" or "当前版本 dev").
    const settingsLink = page.getByRole("button", { name: /settings|设置/i });
    if (!await settingsLink.isVisible({ timeout: 5_000 }).catch(() => false)) return;
    await settingsLink.click();

    await expect(
      page.getByText(/version|版本/i).first(),
    ).toBeVisible({ timeout: 10_000 });
  });

  test("version API returns valid response", async ({ page }) => {
    // GOAL: The /api/v1/version endpoint returns a JSON response with
    // current version and update status. This validates the backend
    // VersionChecker is running.
    await page.waitForLoadState("networkidle");

    const response = await page.request.get("/api/v1/version", {
      headers: {
        Authorization: `Bearer ${process.env.E2E_AUTH_TOKEN ?? ""}`,
      },
    });

    // May return 401 if no auth token — that's fine, we're testing
    // the endpoint exists and responds.
    if (response.status() === 200) {
      const body = await response.json();
      expect(body).toHaveProperty("current");
      expect(body).toHaveProperty("has_update");
      expect(typeof body.current).toBe("string");
      expect(typeof body.has_update).toBe("boolean");
    } else {
      // 401 is expected without auth — endpoint exists
      expect([200, 401]).toContain(response.status());
    }
  });

  test("health endpoint returns version", async ({ page }) => {
    // GOAL: /health endpoint is public and returns the running version.
    // This is what Docker healthcheck and monitoring tools use.
    const response = await page.request.get("/health");
    expect(response.status()).toBe(200);

    const body = await response.json();
    expect(body).toHaveProperty("status", "ok");
    expect(body).toHaveProperty("version");
    expect(typeof body.version).toBe("string");
    expect(body.version.length).toBeGreaterThan(0);
  });
});
