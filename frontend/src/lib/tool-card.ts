// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { web3CommandFromTool } from "@/lib/web3-command";
import type { ToolCallView } from "@/types/chat";

export type ToolCardKind = "web3-cli" | "web3-skill" | "generic";

export type SkillCardView = {
  name: string;
  path: string;
  description: string;
};

export function toolCardKind(tool: ToolCallView): ToolCardKind {
  if (web3CommandFromTool(tool)) return "web3-cli";
  if (skillCardFromTool(tool)) return "web3-skill";
  return "generic";
}

export function skillCardFromTool(tool: ToolCallView): SkillCardView | null {
  const input = asRecord(tool.input);
  const explicitSkill = textFrom(input.skill) || textFrom(input.name);
  const name = textFrom(tool.name);
  if (name.toLowerCase().includes("skill")) {
    const path = skillPathFromInput(input);
    return {
      name: explicitSkill || skillNameFromPath(path) || prettifyToolName(name) || "Skill",
      path,
      description: textFrom(input.description),
    };
  }

  const path = skillPathFromInput(input);
  if (!isSkillMarkdownPath(path)) return null;
  return {
    name: explicitSkill || skillNameFromPath(path) || "Skill",
    path,
    description: textFrom(input.description),
  };
}

function skillPathFromInput(input: Record<string, unknown>) {
  return textFrom(input.file_path) || textFrom(input.path) || textFrom(input.uri);
}

function isSkillMarkdownPath(path: string) {
  return /(?:^|[\\/])SKILL\.md$/i.test(path) || /(?:^|[\\\/])skills[\\\/]_shared[\\\/][^\\\/]+\.md$/i.test(path);
}

function skillNameFromPath(path: string) {
  if (!path) return "";
  const parts = path.split(/[\\/]/).filter(Boolean);
  const skillFileIndex = parts.findIndex((part) => part.toLowerCase() === "skill.md");
  if (skillFileIndex > 0) return parts[skillFileIndex - 1];
  const sharedIndex = parts.findIndex((part) => part === "_shared");
  const sharedFile = sharedIndex >= 0 ? parts[sharedIndex + 1] : "";
  if (sharedFile?.toLowerCase().endsWith(".md")) return `_shared/${sharedFile.replace(/\.md$/i, "")}`;
  return "";
}

export function skillTranslationKey(skill: Pick<SkillCardView, "name" | "path">) {
  const id = skill.name || skillNameFromPath(skill.path);
  switch (id) {
    case "web3-portfolio":
      return "web3Portfolio";
    case "web3-swap":
      return "web3Swap";
    case "web3-bridge":
      return "web3Bridge";
    case "web3-token":
      return "web3Token";
    case "web3-chain":
      return "web3Chain";
    case "web3-security":
      return "web3Security";
    case "web3-ssm":
      return "web3Ssm";
    case "web3-onboard":
      return "web3Onboard";
    case "_shared/passkey":
      return "sharedPasskey";
    case "_shared/preflight":
      return "sharedPreflight";
    case "_shared/amount-resolution":
      return "sharedAmountResolution";
    case "_shared/security-handling":
      return "sharedSecurityHandling";
    case "_shared/broadcast-output":
      return "sharedBroadcastOutput";
    case "_shared/error-codes":
      return "sharedErrorCodes";
    case "_shared/intent-expiry":
      return "sharedIntentExpiry";
    case "_shared/slippage":
      return "sharedSlippage";
    case "_shared/tags":
      return "sharedTags";
    case "_shared/token-resolution":
      return "sharedTokenResolution";
    default:
      return "";
  }
}

function prettifyToolName(name: string) {
  const cleaned = name.replace(/[_-]+/g, " ").trim();
  return cleaned ? cleaned.replace(/\b\w/g, (char) => char.toUpperCase()) : "";
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function textFrom(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}
