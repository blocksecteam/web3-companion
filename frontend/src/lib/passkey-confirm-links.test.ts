// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import {
  confirmLinksFromContent,
  removeConfirmLinksFromContent,
} from "@/lib/passkey-confirm-links";

describe("confirmLinksFromContent", () => {
  test("extracts browser confirm page links", () => {
    const links = confirmLinksFromContent(
      "Please confirm: https://app.example.com/confirm/ch_abc123?from=chat",
    );

    expect(links).toEqual([
      {
        challengeId: "ch_abc123",
        url: "https://app.example.com/confirm/ch_abc123?from=chat",
      },
    ]);
  });

  test("extracts bold raw passkey links without markdown markers", () => {
    const links = confirmLinksFromContent("Confirm: **http://localhost:8081/confirm/ch_bold123**");

    expect(links).toEqual([
      {
        challengeId: "ch_bold123",
        url: "http://localhost:8081/confirm/ch_bold123",
      },
    ]);
  });
});

describe("removeConfirmLinksFromContent", () => {
  test("removes raw and markdown passkey links from assistant markdown", () => {
    const content = [
      "Review the operation.",
      "Confirm here: https://app.example.com/confirm/ch_abc123",
      "Then I will continue.",
    ].join("\n");

    expect(removeConfirmLinksFromContent(content)).toBe(
      [
        "Review the operation.",
        "Confirm here:",
        "Then I will continue.",
      ].join("\n"),
    );
  });

  test("removes bold raw passkey links from assistant markdown", () => {
    expect(removeConfirmLinksFromContent("Confirm: **http://localhost:8081/confirm/ch_bold123**")).toBe("Confirm:");
  });
});
