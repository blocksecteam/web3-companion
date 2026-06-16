// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { existsSync } from "node:fs";
import { cp, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { delimiter, dirname, join, relative, resolve, sep } from "node:path";
import { query } from "@anthropic-ai/claude-agent-sdk";
import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { nanoid } from "nanoid";
import { claudeConfigPath, claudeSettingsPath, hasModelAuth } from "./model-auth";
import { MessageProjector } from "./message-projector";
import { normalizeSDKMessageForArchive, shouldArchiveSDKMessage } from "./sdk-archive";
import { createPasskeyRequestHook } from "./passkey-request-hook";
import { runSecurityAgent } from "./security-agent";
import { createSecurityReviewHook, parseSecurityAgentConfig } from "./security-review-hook";
import { SessionStore } from "./session-store";

type Emit = (event: unknown) => void;

type RunOptions = {
  sessionId: string;
  prompt: string;
  store: SessionStore;
  emit: Emit;
  signal: AbortSignal;
};

const rootDir = resolve(".");

export async function prepareClaudeHome() {
  const claudeConfigDir = resolveClaudeConfigDir();
  const backendRoot = resolveBackendRoot();
  const backendPromptPath = resolvePromptPath(backendRoot);
  const backendSkillsDir = resolveRepoSkillsDir(backendRoot);

  process.env.CLAUDE_CONFIG_DIR = claudeConfigDir;
  delete process.env.ANTHROPIC_SMALL_FAST_MODEL;

  await mkdir(claudeConfigDir, { recursive: true });
  await ensureClaudeSettings();
  await ensureClaudeRuntimeConfig();

  if (existsSync(backendSkillsDir)) {
    const targetSkillsDir = resolve(claudeConfigDir, "skills");
    await syncSkills(backendSkillsDir, targetSkillsDir);
  }

  const agentGuide = await safeRead(backendPromptPath);
  await writeFile(
    resolve(claudeConfigDir, "CLAUDE.md"),
    `${agentGuide}\n\nUse the web3 CLI by running commands that begin with "web3". Do not use absolute paths for the CLI. Do not change cwd. Ask for explicit confirmation before any signing or write action.\n`,
  );
}

async function ensureClaudeSettings() {
  const target = claudeSettingsPath();
  const existing = await readJsonObject(target);
  const existingEnv = objectOrEmpty(existing.env);
  const fallbackEnv = {
    ANTHROPIC_BASE_URL: stringOr(existingEnv.ANTHROPIC_BASE_URL, "https://api.deepseek.com/anthropic"),
    ANTHROPIC_MODEL: stringOr(existingEnv.ANTHROPIC_MODEL, "deepseek-v4-pro"),
    ANTHROPIC_DEFAULT_OPUS_MODEL: stringOr(existingEnv.ANTHROPIC_DEFAULT_OPUS_MODEL, "deepseek-v4-pro"),
    ANTHROPIC_DEFAULT_SONNET_MODEL: stringOr(existingEnv.ANTHROPIC_DEFAULT_SONNET_MODEL, "deepseek-v4-pro"),
    ANTHROPIC_DEFAULT_HAIKU_MODEL: stringOr(existingEnv.ANTHROPIC_DEFAULT_HAIKU_MODEL, "deepseek-v4-pro"),
    CLAUDE_CODE_SUBAGENT_MODEL: stringOr(existingEnv.CLAUDE_CODE_SUBAGENT_MODEL, "deepseek-v4-pro"),
    CLAUDE_CODE_EFFORT_LEVEL: stringOr(existingEnv.CLAUDE_CODE_EFFORT_LEVEL, "max"),
  };
  await writeJson(target, {
    ...existing,
    $schema: "https://json.schemastore.org/claude-code-settings.json",
    model: stringOr(existing.model, stringOr(existingEnv.ANTHROPIC_MODEL, "deepseek-v4-pro")),
    effortLevel: stringOr(existing.effortLevel, "xhigh"),
    env: {
      ...existingEnv,
      ...fallbackEnv,
    },
    permissions: {
      ...objectOrEmpty(existing.permissions),
      allow: web3ClaudeAllowRules(),
    },
  });
}

async function ensureClaudeRuntimeConfig() {
  const claudeConfigDir = resolveClaudeConfigDir();
  const agentWorkdir = resolveAgentWorkdir();
  const target = claudeConfigPath();
  if (existsSync(target)) return;

  await writeJson(target, {
    hasCompletedOnboarding: true,
    projects: {
      [agentWorkdir]: {
        allowedTools: web3ClaudeAllowRules(),
        hasTrustDialogAccepted: true,
        hasCompletedProjectOnboarding: true,
      },
    },
  });
}

async function writeJson(path: string, value: unknown) {
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
}

async function readJsonObject(path: string): Promise<Record<string, unknown>> {
  if (!existsSync(path)) return {};
  try {
    const data = await readFile(path, "utf8");
    const parsed = JSON.parse(data) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function objectOrEmpty(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function stringOr(value: unknown, fallback: string) {
  return typeof value === "string" && value.trim() ? value : fallback;
}

export async function runCompanion(options: RunOptions) {
  if (!hasModelAuth()) {
    throw new Error("Model authentication is not configured for the companion server.");
  }

  await runClaudeCompanion(options);
}

async function runClaudeCompanion({ sessionId, prompt, store, emit, signal }: RunOptions) {
  const agentWorkdir = resolveAgentWorkdir();
  const claudeExecutablePath = process.env.CLAUDE_CODE_EXECUTABLE
    ? resolve(process.env.CLAUDE_CODE_EXECUTABLE)
    : undefined;
  const session = store.ensureSession(sessionId);
  const appendSystemPrompt = await buildSystemPrompt();
  const projector = new MessageProjector(sessionId, nanoid);

  const result = query({
    prompt,
    options: {
      cwd: agentWorkdir,
      sessionId: session?.hasClaudeSession ? undefined : sessionId,
      resume: session?.hasClaudeSession ? sessionId : undefined,
      forkSession: false,
      includePartialMessages: true,
      pathToClaudeCodeExecutable: claudeExecutablePath,
      maxTurns: 50,
      includeHookEvents: true,
      settingSources: ["user", "project"],
      systemPrompt: {
        type: "preset",
        preset: "claude_code",
        append: appendSystemPrompt,
      },
      tools: { type: "preset", preset: "claude_code" },
      allowedTools: ["Read", "Grep", "Glob", "Bash"],
      disallowedTools: ["Write", "Edit", "MultiEdit", "NotebookEdit"],
      permissionMode: "bypassPermissions",
      allowDangerouslySkipPermissions: true,
      canUseTool: async (toolName, input) => {
        if (["Read", "Grep", "Glob"].includes(toolName)) {
          return { behavior: "allow" };
        }

        if (toolName === "Bash") {
          const command = typeof input.command === "string" ? input.command.trim() : "";
          if (isAllowedWeb3Command(command)) {
            return { behavior: "allow" };
          }
          return {
            behavior: "deny",
            message: "Only the fixed web3 CLI may be executed by this companion server.",
          };
        }

        return {
          behavior: "deny",
          message: `${toolName} is not enabled in the Web3 Companion server.`,
        };
      },
      hooks: {
        PostToolUse: [{
          matcher: "Bash",
          hooks: [
            createPasskeyRequestHook({
              sessionId,
              emit: (event) => emit(event),
              store,
            }),
            createSecurityReviewHook({
              sessionId,
              emit: (event) => emit(event),
              store,
              config: parseSecurityAgentConfig(process.env.WEB3_SECURITY_AGENT_CONFIG),
              runSecurityAgent: (check, { signal, sessionLanguage }) =>
                runSecurityAgent({ check, signal, sessionLanguage }),
            }),
          ],
        }],
      },
      env: buildClaudeEnv(),
      abortController: abortControllerFromSignal(signal),
    },
  });

  try {
    for await (const message of result) {
      if (signal.aborted) break;
      const sdkMessage = normalizeSDKMessageForArchive(sessionId, message as SDKMessage, nanoid());

      if (sdkMessage.type === "stream_event") {
        const partial = projector.projectStream(sdkMessage as never);
        if (partial) emit(partial);
        continue;
      }

      if (shouldArchiveSDKMessage(sdkMessage)) {
        store.addRawSDKMessage(sessionId, sdkMessage);
      }

      if (sdkMessage.type === "assistant") {
        const chatMessage = projector.projectAssistant(sdkMessage);
        if (!chatMessage) continue;
        const saved = store.addMessage(sessionId, chatMessage);
        emit({ type: "message", sessionId, message: saved });
        for (const tool of saved.tools ?? []) {
          emit({ type: "tool", sessionId, messageId: saved.id, tool });
        }
        continue;
      }

      if (sdkMessage.type === "tool_progress") {
        const progress = projector.projectToolProgress(sdkMessage);
        if (progress) {
          emit({ type: "tool", sessionId, messageId: progress.messageId, tool: progress.tool });
        }
        continue;
      }

      if (sdkMessage.type === "system" && isSecurityHookLifecycleMessage(sdkMessage)) {
        continue;
      }

      if (sdkMessage.type === "user") {
        continue;
      }

      if (sdkMessage.type === "result") {
        if ((sdkMessage as { is_error?: boolean }).is_error) {
          const errors = (sdkMessage as { errors?: string[] }).errors?.join("\n") || "Agent run failed.";
          emit({ type: "error", sessionId, message: errors });
        }
        store.markClaudeSession(sessionId);
      }
    }
  } finally {
    result.close();
  }
}

function isSecurityHookLifecycleMessage(message: SDKMessage) {
  const record = message as unknown as Record<string, unknown>;
  return (
    record.subtype === "hook_started" ||
    record.subtype === "hook_progress" ||
    record.subtype === "hook_response"
  );
}

export function buildClaudeEnv() {
  const claudeConfigDir = resolveClaudeConfigDir();
  const backendCliPath = resolveWeb3CLIPath();
  const web3BinDir = backendCliPath ? dirname(backendCliPath) : undefined;
  const env: Record<string, string | undefined> = {
    CLAUDE_CONFIG_DIR: claudeConfigDir,
    HOME: process.env.HOME,
    USER: process.env.USER,
    LOGNAME: process.env.LOGNAME,
    TMPDIR: process.env.TMPDIR,
    PATH: web3BinDir ? withPathEntry(process.env.PATH ?? "", web3BinDir) : process.env.PATH,
    WEB3_API_URL: process.env.WEB3_API_URL,
    WEB3_CLI_PATH: backendCliPath || undefined,
    WEB3_COMPANION_ROOT: rootDir,
  };
  delete env.ANTHROPIC_SMALL_FAST_MODEL;
  return env;
}

function web3ClaudeAllowRules() {
  return [
    "Bash(web3)",
    "Bash(web3 *)",
  ];
}

async function buildSystemPrompt() {
  return buildSystemPromptFromPaths({
    backendRoot: resolveBackendRoot(),
    agentWorkdir: resolveAgentWorkdir(),
    claudeConfigDir: resolveClaudeConfigDir(),
    backendCliPath: resolveWeb3CLIPath(),
  });
}

async function buildSystemPromptFromPaths({
  backendRoot,
  agentWorkdir,
  claudeConfigDir,
  backendCliPath,
}: {
  backendRoot: string;
  agentWorkdir: string;
  claudeConfigDir: string;
  backendCliPath: string;
}) {
  const backendPromptPath = resolvePromptPath(backendRoot);
  const guide = await safeRead(backendPromptPath);
  const cliNote = backendCliPath && existsSync(backendCliPath)
    ? `The fixed web3 CLI is on PATH. Use commands beginning with "web3"; never use absolute paths for the CLI and never execute arbitrary shell commands.`
    : "The backend web3 CLI binary was not found yet. Explain that backend CLI integration is pending when a user asks for live execution.";

  return `${guide}

Frontend companion server constraints:
- Fixed cwd: ${agentWorkdir}
- CLAUDE_CONFIG_DIR: ${claudeConfigDir}
- Backend skills are synchronized into ${resolve(claudeConfigDir, "skills")} from the repo skills directory at startup.
- ${cliNote}
- Multiple sessions may exist, but the server only permits one running session at a time.
- Never sign, broadcast, delete wallets, change policies, or perform irreversible actions without explicit user confirmation.`;
}

export function isAllowedWeb3Command(command: string) {
  if (hasShellControlSyntax(command)) return false;
  const normalized = command.replace(/\s+/g, " ");
  if (normalized === "web3") return true;
  if (!normalized.startsWith("web3 ")) return false;
  const tokens = normalized.split(" ");
  if ((tokens[1] === "swap" || tokens[1] === "bridge") && tokens[2] === "execute") return false;
  if (tokens[1] !== "ssm") return true;
  if (tokens[2] === "policies" || tokens[2] === "audit") return false;
  if (tokens[2] === "wallets" || tokens[2] === "webauthn") return false;
  return true;
}

function hasShellControlSyntax(command: string) {
  return /[\r\n;&|<>`$]/.test(command);
}

function withPathEntry(pathValue: string, entry: string) {
  const parts = pathValue.split(delimiter).filter(Boolean);
  return [entry, ...parts.filter((part) => part !== entry)].join(delimiter);
}

export function resolveWeb3CLIPath(env: NodeJS.ProcessEnv = process.env) {
  const explicit = stringOr(env.WEB3_CLI_PATH, "");
  if (explicit) return resolve(explicit);

  const pathValue = env.PATH ?? "";
  for (const dir of pathValue.split(delimiter).filter(Boolean)) {
    const candidate = resolve(dir, "web3");
    if (existsSync(candidate)) return candidate;
  }

  const packaged = resolve(resolveBackendRoot(env), "dist/web3");
  if (existsSync(packaged)) return packaged;
  return "";
}

function resolveBackendRoot(env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.WEB3_BACKEND_ROOT ?? ".");
}

function resolveAgentWorkdir(env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.WEB3_AGENT_WORKDIR ?? ".");
}

function resolveClaudeConfigDir(env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.CLAUDE_CONFIG_DIR ?? "dist/home");
}

function resolveRepoSkillsDir(backendRoot: string, env: NodeJS.ProcessEnv = process.env) {
  return resolve(env.WEB3_REPO_SKILLS_DIR ?? resolve(backendRoot, "skills"));
}

function resolvePromptPath(backendRoot: string) {
  return resolve(backendRoot, "companion/web3_companion.md");
}

async function syncSkills(sourceDir: string, targetDir: string) {
  if (await directoriesMatch(sourceDir, targetDir)) return false;
  await rm(targetDir, { recursive: true, force: true });
  await cp(sourceDir, targetDir, {
    recursive: true,
    force: true,
  });
  return true;
}

async function directoriesMatch(leftDir: string, rightDir: string) {
  if (!existsSync(leftDir) || !existsSync(rightDir)) return false;
  const [leftEntries, rightEntries] = await Promise.all([
    listDirectoryEntries(leftDir),
    listDirectoryEntries(rightDir),
  ]);
  if (leftEntries.length !== rightEntries.length) return false;
  for (let index = 0; index < leftEntries.length; index += 1) {
    const left = leftEntries[index];
    const right = rightEntries[index];
    if (left !== right) return false;
    if (!left.endsWith("/")) {
      const [leftData, rightData] = await Promise.all([
        readFile(join(leftDir, fromPortablePath(left))),
        readFile(join(rightDir, fromPortablePath(right))),
      ]);
      if (!leftData.equals(rightData)) return false;
    }
  }
  return true;
}

async function listDirectoryEntries(root: string, current = root): Promise<string[]> {
  const entries = await readdir(current, { withFileTypes: true });
  entries.sort((left, right) => left.name.localeCompare(right.name));
  const out: string[] = [];
  for (const entry of entries) {
    const absolute = join(current, entry.name);
    const portable = toPortablePath(relative(root, absolute));
    if (entry.isDirectory()) {
      out.push(`${portable}/`);
      out.push(...await listDirectoryEntries(root, absolute));
      continue;
    }
    if (entry.isFile()) {
      out.push(portable);
    }
  }
  return out;
}

function toPortablePath(path: string) {
  return path.split(sep).join("/");
}

function fromPortablePath(path: string) {
  return path.split("/").join(sep);
}

function abortControllerFromSignal(signal: AbortSignal) {
  const controller = new AbortController();
  if (signal.aborted) {
    controller.abort();
  } else {
    signal.addEventListener("abort", () => controller.abort(), { once: true });
  }
  return controller;
}

async function safeRead(path: string) {
  try {
    return await readFile(path, "utf8");
  } catch {
    return "";
  }
}

export const resolvePromptPathForTest = resolvePromptPath;
export const syncSkillsForTest = syncSkills;

export function buildSystemPromptForTest({
  backendRoot = resolveBackendRoot(),
  agentWorkdir = resolveAgentWorkdir(),
  claudeConfigDir = resolveClaudeConfigDir(),
  backendCliPath = "",
}: Partial<{
  backendRoot: string;
  agentWorkdir: string;
  claudeConfigDir: string;
  backendCliPath: string;
}> = {}) {
  return buildSystemPromptFromPaths({ backendRoot, agentWorkdir, claudeConfigDir, backendCliPath });
}
