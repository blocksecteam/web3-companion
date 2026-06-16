// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

type JsonObject = Record<string, unknown>;

export function claudeConfigPath() {
  return resolve(claudeConfigDir(), ".claude.json");
}

export function claudeSettingsPath() {
  return resolve(claudeConfigDir(), "settings.json");
}

export function hasModelAuth() {
  return hasSettingsModelAuth(claudeSettingsPath());
}

function claudeConfigDir() {
  return resolve(process.env.CLAUDE_CONFIG_DIR ?? resolve(process.env.HOME ?? ".", ".claude"));
}

function hasSettingsModelAuth(path: string) {
  if (!existsSync(path)) return false;

  try {
    const raw = readFileSync(path, "utf8").trim();
    if (!raw) return false;

    const parsed = JSON.parse(raw) as unknown;
    if (!isObject(parsed) || !isObject(parsed.env)) return false;
    return Boolean(parsed.env.ANTHROPIC_AUTH_TOKEN || parsed.env.ANTHROPIC_API_KEY);
  } catch {
    return false;
  }
}

function isObject(value: unknown): value is JsonObject {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
