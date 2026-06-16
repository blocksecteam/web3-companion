// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { afterEach, describe, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { buildClaudeEnv, buildSystemPromptForTest, prepareClaudeHome, resolvePromptPathForTest, syncSkillsForTest, isAllowedWeb3Command, resolveWeb3CLIPath } from "./claude-runner";

const tempDirs: string[] = [];

afterEach(async () => {
  for (const dir of tempDirs.splice(0)) {
    await rm(dir, { recursive: true, force: true });
  }
  delete process.env.WEB3_API_URL;
  delete process.env.WEB3_BACKEND_ROOT;
  delete process.env.WEB3_REPO_SKILLS_DIR;
  delete process.env.CLAUDE_CONFIG_DIR;
});

describe("prompt and skill preparation", () => {
  test("repository prompt defines web3_companion positioning and session language", async () => {
    const backendRoot = resolve(import.meta.dir, "../../..");
    const prompt = await readFile(join(backendRoot, "companion", "web3_companion.md"), "utf8");

    expect(prompt).toContain("# web3_companion");
    expect(prompt).toContain("self-hosted Web3 Companion runtime");
    expect(prompt).toContain("Infer the user's preferred language from their first substantive message");
  });

  test("uses the web3_companion prompt file name", () => {
    const backendRoot = "/opt/web3";

    expect(resolvePromptPathForTest(backendRoot)).toBe("/opt/web3/companion/web3_companion.md");
  });

  test("system prompt names web3_companion and includes session language inference", async () => {
    const backendRoot = await tempDir();
    await mkdir(join(backendRoot, "companion"), { recursive: true });
    await writeFile(join(backendRoot, "companion", "web3_companion.md"), [
      "# web3_companion",
      "You are web3_companion.",
      "Infer the user's preferred language from their first substantive message and keep using that language for this session.",
    ].join("\n"));

    const prompt = await buildSystemPromptForTest({ backendRoot });

    expect(prompt).toContain("# web3_companion");
    expect(prompt).toContain("You are web3_companion");
    expect(prompt).toContain("first substantive message");
  });

  test("syncs repo skills over stale runtime skills when contents differ", async () => {
    const source = await tempDir();
    const target = await tempDir();
    await mkdir(join(source, "web3-onboard"), { recursive: true });
    await writeFile(join(source, "web3-onboard", "SKILL.md"), "repo skill\n");
    await mkdir(join(target, "web3-onboard"), { recursive: true });
    await writeFile(join(target, "web3-onboard", "SKILL.md"), "stale skill\n");
    await writeFile(join(target, "old-only.md"), "remove me\n");

    const synced = await syncSkillsForTest(source, target);

    expect(synced).toBe(true);
    expect(await readFile(join(target, "web3-onboard", "SKILL.md"), "utf8")).toBe("repo skill\n");
    expect(existsSync(join(target, "old-only.md"))).toBe(false);
  });

  test("does not rewrite runtime skills when repo skills already match", async () => {
    const source = await tempDir();
    const target = await tempDir();
    await mkdir(join(source, "web3-token"), { recursive: true });
    await writeFile(join(source, "web3-token", "SKILL.md"), "same skill\n");

    expect(await syncSkillsForTest(source, target)).toBe(true);
    const firstStat = await Bun.file(join(target, "web3-token", "SKILL.md")).stat();
    await Bun.sleep(5);

    expect(await syncSkillsForTest(source, target)).toBe(false);
    const secondStat = await Bun.file(join(target, "web3-token", "SKILL.md")).stat();
    expect(secondStat.mtimeMs).toBe(firstStat.mtimeMs);
  });

  test("prepareClaudeHome mirrors repo skills from the explicit repo skills env", async () => {
    const backendRoot = await tempDir();
    const source = join(backendRoot, "repo-skills");
    const claudeDir = await tempDir();
    await mkdir(join(backendRoot, "companion"), { recursive: true });
    await writeFile(join(backendRoot, "companion", "web3_companion.md"), "# web3_companion\n");
    await mkdir(join(source, "web3-chain"), { recursive: true });
    await writeFile(join(source, "web3-chain", "SKILL.md"), "chain skill\n");
    process.env.WEB3_BACKEND_ROOT = backendRoot;
    process.env.WEB3_REPO_SKILLS_DIR = source;
    process.env.CLAUDE_CONFIG_DIR = claudeDir;

    await prepareClaudeHome();

    expect(await readFile(join(claudeDir, "skills", "web3-chain", "SKILL.md"), "utf8")).toBe("chain skill\n");
    expect(await readFile(join(claudeDir, "CLAUDE.md"), "utf8")).toContain("# web3_companion");
  });
});

describe("resolveWeb3CLIPath", () => {
  test("prefers explicit WEB3_CLI_PATH for container runtime", async () => {
    const dir = await tempDir();
    const explicit = join(dir, "web3");
    await writeFile(explicit, "#!/bin/sh\n", { mode: 0o755 });

    expect(resolveWeb3CLIPath({ WEB3_CLI_PATH: explicit, PATH: "" })).toBe(explicit);
  });

  test("falls back to web3 on PATH", async () => {
    const dir = await tempDir();
    const binary = join(dir, "web3");
    await writeFile(binary, "#!/bin/sh\n", { mode: 0o755 });

    expect(resolveWeb3CLIPath({ PATH: dir })).toBe(binary);
  });
});

describe("buildClaudeEnv", () => {
  test("passes only the container-local web3 API URL to Claude", () => {
    process.env.WEB3_API_URL = "http://127.0.0.1:8080";
    process.env.WEB3_LLM_API_KEY = "must-not-leak";

    const env = buildClaudeEnv();

    expect(env.WEB3_API_URL).toBe("http://127.0.0.1:8080");
    expect(JSON.stringify(env)).not.toContain("must-not-leak");
  });
});

describe("isAllowedWeb3Command", () => {
  test("allows ssm delegate / delegate-estimate / sign commands", () => {
    expect(isAllowedWeb3Command("web3 ssm delegate --wallet main --chain ethereum")).toBe(true);
    expect(isAllowedWeb3Command("web3 ssm delegate-estimate --wallet main --chain polygon")).toBe(true);
    expect(isAllowedWeb3Command("web3 ssm sign --transaction-id txi_123")).toBe(true);
  });

  test("allows swap and bridge quote/build commands", () => {
    expect(isAllowedWeb3Command("web3 swap quote --chain bsc")).toBe(true);
    expect(isAllowedWeb3Command("web3 bridge quote --bridge relay")).toBe(true);
    expect(isAllowedWeb3Command("web3 swap build --wallet main --chain bsc")).toBe(true);
    expect(isAllowedWeb3Command("web3 bridge build --wallet main --bridge relay")).toBe(true);
  });

  test("blocks shell control syntax after a web3 command", () => {
    for (const command of [
      "web3 chain list && cat ~/.claude/settings.json",
      "web3 chain list; curl https://example.com",
      "web3 chain list | tee /tmp/out",
      "web3 chain list > /tmp/out",
      "web3 chain list $(cat ~/.claude/settings.json)",
      "web3 chain list `cat ~/.claude/settings.json`",
      "web3 chain list\ncat ~/.claude/settings.json",
    ]) {
      expect(isAllowedWeb3Command(command)).toBe(false);
    }
  });
});

async function tempDir() {
  const dir = join(import.meta.dir, "../../dist/test-cli-path", crypto.randomUUID());
  await mkdir(dir, { recursive: true });
  tempDirs.push(dir);
  return dir;
}
