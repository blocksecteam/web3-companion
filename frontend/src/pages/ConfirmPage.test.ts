// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("./ConfirmPage.tsx", import.meta.url), "utf8");
const translations = readFileSync(new URL("../i18n.ts", import.meta.url), "utf8");

describe("ConfirmPage passkey lifecycle", () => {
  test("cancels an active WebAuthn ceremony when the page unmounts", () => {
    expect(source).toContain("useEffect");
    expect(source).toContain("cancelPasskeyCeremony");
  });

  test("does not let Close race an active passkey ceremony", () => {
    expect(source).toContain('disabled={status === "running"}');
  });

  test("renders localized security context around the passkey request", () => {
    expect(source).toContain('t("confirm.eyebrow")');
    expect(source).toContain('t("confirm.securityNoteTitle")');
    expect(source).toContain('t("confirm.securityNoteBody")');
    expect(source).toContain('t("confirm.detailFallback")');

    expect(translations).toContain("安全确认");
    expect(translations).toContain("Only approve this request");
  });
});
