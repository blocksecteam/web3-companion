// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const workspaceSource = readFileSync(new URL("./ChatWorkspace.tsx", import.meta.url), "utf8");
const bubbleSource = readFileSync(new URL("./MessageBubble.tsx", import.meta.url), "utf8");

describe("chat layout polish", () => {
  test("renders streaming status near the composer instead of inside assistant markdown", () => {
    expect(workspaceSource).toContain("<StreamingStatus");
    expect(workspaceSource).toContain("t(\"chat.generating\")");
    expect(bubbleSource).not.toContain("t(\"chat.streaming\")");
  });

  test("keeps the composer streaming status compact and quiet", () => {
    expect(workspaceSource).toContain("streaming-status-pill");
    expect(workspaceSource).not.toContain("shadow-soft");
    expect(workspaceSource).not.toContain("min-h-10");
    expect(workspaceSource).not.toContain("sm:text-base");
  });

  test("keeps thinking-only messages from crowding adjacent messages", () => {
    expect(workspaceSource).toContain("isThinkingOnly(message)");
    expect(workspaceSource).toContain('return "mt-2"');
    expect(bubbleSource).not.toContain("mb-0 text-left text-sm text-muted-foreground");
  });

  test("keeps thinking blocks tight before following content", () => {
    expect(bubbleSource).toContain('className="mb-1 text-left text-sm text-muted-foreground"');
    expect(bubbleSource).not.toContain('className="mb-2 text-left text-sm text-muted-foreground"');
  });

  test("keeps composer streaming status vertically balanced", () => {
    expect(workspaceSource).toContain('className="mx-auto my-2 flex max-w-4xl justify-center"');
    expect(workspaceSource).not.toContain('className="mx-auto mb-2 flex max-w-4xl justify-center"');
    expect(workspaceSource).toContain('activeRunning && "pt-0"');
  });
});
