// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { skillCardFromTool, skillTranslationKey, toolCardKind } from "@/lib/tool-card";
import type { ToolCallView } from "@/types/chat";

describe("toolCardKind", () => {
  test("keeps web3 CLI calls in the web3 card", () => {
    const tool: ToolCallView = {
      id: "tool-1",
      name: "Bash",
      kind: "command",
      status: "running",
      input: { command: "web3 portfolio ethereum 0x1234" },
    };

    expect(toolCardKind(tool)).toBe("web3-cli");
  });

  test("wraps skill tool calls in the web3 skill card", () => {
    const tool: ToolCallView = {
      id: "tool-1",
      name: "Skill",
      kind: "skill",
      status: "running",
      input: { skill: "frontend-design" },
    };

    expect(toolCardKind(tool)).toBe("web3-skill");
    expect(skillCardFromTool(tool)).toMatchObject({
      name: "frontend-design",
      path: "",
    });
  });

  test("wraps reads of SKILL.md files in the web3 skill card", () => {
    const tool: ToolCallView = {
      id: "tool-1",
      name: "Read",
      kind: "file",
      status: "running",
      input: { file_path: "/home/yufeng/.codex/skills/impeccable/SKILL.md" },
    };

    expect(toolCardKind(tool)).toBe("web3-skill");
    expect(skillCardFromTool(tool)).toMatchObject({
      name: "impeccable",
      path: "/home/yufeng/.codex/skills/impeccable/SKILL.md",
    });
  });

  test("wraps reads of shared skill markdown in the web3 skill card", () => {
    const tool: ToolCallView = {
      id: "tool-1",
      name: "Read",
      kind: "file",
      status: "running",
      input: { file_path: "/home/yufeng/web3-companion/backend/skills/_shared/passkey.md" },
    };

    expect(toolCardKind(tool)).toBe("web3-skill");
    expect(skillCardFromTool(tool)).toMatchObject({
      name: "_shared/passkey",
      path: "/home/yufeng/web3-companion/backend/skills/_shared/passkey.md",
    });
  });

  test("maps web3 and shared skills to localized copy keys", () => {
    expect(skillTranslationKey({ name: "web3-swap", path: "" })).toBe("web3Swap");
    expect(skillTranslationKey({ name: "_shared/passkey", path: "/repo/skills/_shared/passkey.md" })).toBe("sharedPasskey");
    expect(skillTranslationKey({ name: "frontend-design", path: "" })).toBe("");
  });

  test("keeps skill summaries from metadata instead of args", () => {
    const tool: ToolCallView = {
      id: "tool-1",
      name: "Skill",
      kind: "skill",
      status: "running",
      input: {
        skill: "frontend-design",
        description: "Use this skill for frontend UI work",
        args: "Invocation args are displayed only as raw input by the renderer",
      },
    };

    expect(skillCardFromTool(tool)).toMatchObject({
      name: "frontend-design",
      path: "",
      description: "Use this skill for frontend UI work",
    });
  });
});
