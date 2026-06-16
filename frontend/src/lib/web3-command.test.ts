// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { commandSubject, parseWeb3Command, web3CommandFromTool } from "@/lib/web3-command";
import type { ToolCallView } from "@/types/chat";

describe("parseWeb3Command", () => {
  test("recognizes web3 portfolio commands", () => {
    const parsed = parseWeb3Command("web3 portfolio ethereum 0x1234");

    expect(parsed).toMatchObject({
      domain: "portfolio",
      action: "snapshot",
      chain: "ethereum",
      address: "0x1234",
      safety: "read",
    });
  });

  test("recognizes ssm delegate as a sensitive wallet action", () => {
    expect(parseWeb3Command("web3 ssm delegate --wallet main --chain ethereum")).toMatchObject({
      domain: "wallet",
      action: "delegate",
      walletId: "main",
      chain: "ethereum",
      safety: "sensitive",
    });
  });

  test("recognizes ssm delegate-estimate as a read-only wallet preview", () => {
    expect(parseWeb3Command("web3 ssm delegate-estimate --wallet main --chain polygon")).toMatchObject({
      domain: "wallet",
      action: "delegate-estimate",
      walletId: "main",
      chain: "polygon",
      safety: "read",
    });
  });

  test("recognizes context status as the readiness command", () => {
    expect(parseWeb3Command("web3 context status")).toMatchObject({
      domain: "context",
      action: "status",
      safety: "read",
    });
  });

  test("ignores trailing stderr redirection when extracting command subject", () => {
    const parsed = parseWeb3Command("web3 chain list 2>&1");

    expect(parsed).toMatchObject({
      command: "web3 chain list 2>&1",
      domain: "chain",
      action: "list",
      safety: "read",
    });
    expect(parsed ? commandSubject(parsed) : "missing").toBe("");
  });

  test("ignores other trailing shell redirections when extracting subjects", () => {
    for (const suffix of ["> /tmp/out.json", "1>/tmp/out.json", "2> /tmp/err.log", "&> /tmp/all.log"]) {
      const parsed = parseWeb3Command(`web3 chain list ${suffix}`);
      expect(parsed ? commandSubject(parsed) : "missing").toBe("");
    }
  });

  test("recognizes swap build commands as sensitive preparation", () => {
    const parsed = parseWeb3Command(
      "web3 swap build --wallet main --chain ethereum --from native --to 0xabc --amount 0.1",
    );

    expect(parsed).toMatchObject({
      domain: "swap",
      action: "build",
      walletId: "main",
      chain: "ethereum",
      amount: "0.1",
      safety: "sensitive",
    });
  });

  test("rejects non-web3 bash commands", () => {
    expect(parseWeb3Command("echo web3 portfolio ethereum 0x1234")).toBeNull();
    expect(parseWeb3Command("web3ctl portfolio ethereum 0x1234")).toBeNull();
  });
});

describe("web3CommandFromTool", () => {
  test("extracts web3 command from bash tool input", () => {
    const tool: ToolCallView = {
      id: "tool-1",
      name: "Bash",
      kind: "command",
      status: "running",
      input: { command: "web3 chain tx base 0xhash" },
    };

    expect(web3CommandFromTool(tool)).toMatchObject({
      domain: "chain",
      action: "transaction",
      chain: "base",
      hash: "0xhash",
    });
  });
});
