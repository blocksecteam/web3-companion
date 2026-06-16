// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import {
  createSessionId,
  isViewportNearBottom,
  messageFromApiError,
  scrollToViewportBottom,
  shouldWarnBeforeLeavingWalletAction,
} from "@/lib/client-utils";

describe("client utils", () => {
  test("creates browser-native UUID session ids", () => {
    const id = createSessionId();

    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });

  test("targets the viewport scroll height so bottom padding is included", () => {
    const calls: ScrollToOptions[] = [];
    const viewport = {
      scrollHeight: 1248,
      scrollTo: (options: ScrollToOptions) => calls.push(options),
    };

    scrollToViewportBottom(viewport);

    expect(calls).toEqual([{ top: 1248, behavior: "smooth" }]);
  });

  test("treats a viewport pinned at the bottom as near-bottom", () => {
    expect(isViewportNearBottom({ scrollTop: 920, scrollHeight: 1000, clientHeight: 80 })).toBe(true);
  });

  test("treats a viewport scrolled up beyond the threshold as not near-bottom", () => {
    expect(isViewportNearBottom({ scrollTop: 200, scrollHeight: 1000, clientHeight: 80 })).toBe(false);
  });

  test("still counts as near-bottom within the threshold band", () => {
    // 1000 - 880 - 80 = 40px from the bottom, inside the default 80px band.
    expect(isViewportNearBottom({ scrollTop: 880, scrollHeight: 1000, clientHeight: 80 })).toBe(true);
  });

  test("defaults to near-bottom when there is no viewport yet", () => {
    expect(isViewportNearBottom(null)).toBe(true);
  });

  test("uses local messages for known backend error codes", () => {
    const error = Object.assign(new Error("backend raw"), { code: "wallet_asset_snapshot_stale" });

    expect(messageFromApiError(error, "fallback", { wallet_asset_snapshot_stale: "Refresh first." })).toBe("Refresh first.");
  });

  test("falls back to backend message for unknown codes", () => {
    expect(messageFromApiError(new Error("backend raw"), "fallback", {})).toBe("backend raw");
  });

  test("blocks leaving after export until the user confirms the key is saved", () => {
    expect(shouldWarnBeforeLeavingWalletAction({ type: "export", privateKey: "0xabc" })).toBe(true);
    expect(shouldWarnBeforeLeavingWalletAction({ type: "export", privateKey: "0xabc", saved: true })).toBe(false);
    expect(shouldWarnBeforeLeavingWalletAction({ type: "delete" })).toBe(false);
    expect(shouldWarnBeforeLeavingWalletAction({ type: "bind_policy" })).toBe(false);
    expect(shouldWarnBeforeLeavingWalletAction(null)).toBe(false);
  });
});
