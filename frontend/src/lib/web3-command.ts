// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { ToolCallKind, ToolCallView } from "@/types/chat";

export type Web3CommandDomain =
  | "context"
  | "wallet"
  | "portfolio"
  | "swap"
  | "bridge"
  | "token"
  | "chain"
  | "security"
  | "audit"
  | "policy"
  | "unknown";

export type Web3CommandSafety = "read" | "prepare" | "sensitive";

export type ParsedWeb3Command = {
  command: string;
  args: string[];
  domain: Web3CommandDomain;
  action: string;
  kind: ToolCallKind;
  safety: Web3CommandSafety;
  flags: Record<string, string | boolean>;
  chain?: string;
  walletId?: string;
  address?: string;
  hash?: string;
  amount?: string;
  protocol?: string;
};

export type Web3ToolView = ParsedWeb3Command;

const domainKind: Record<Web3CommandDomain, ToolCallKind> = {
  context: "wallet",
  wallet: "wallet",
  portfolio: "portfolio",
  swap: "swap",
  bridge: "bridge",
  token: "token",
  unknown: "tool",
  chain: "chain",
  security: "security",
  audit: "wallet",
  policy: "wallet",
};

export function web3CommandFromTool(tool: ToolCallView): Web3ToolView | null {
  const input = asRecord(tool.input);
  const command = textFrom(input.command);
  const parsed = parseWeb3Command(command);
  if (!parsed) return null;
  return parsed;
}

export function parseWeb3Command(command: string): ParsedWeb3Command | null {
  const normalized = command.replace(/\s+/g, " ").trim();
  if (!normalized || normalized !== "web3" && !normalized.startsWith("web3 ")) return null;

  const args = stripTrailingShellRedirections(tokenize(normalized)).slice(1);
  const { flags, positional } = parseFlags(args);
  const section = positional[0] ?? "";
  const second = positional[1] ?? "";
  let domain: Web3CommandDomain = "unknown";
  let action = second || "run";
  let safety: Web3CommandSafety = "read";

  if (section === "context") {
    domain = "context";
    action = second || "status";
    safety = "read";
  } else if (section === "auth") {
    return null;
  } else if (section === "portfolio") {
    domain = "portfolio";
    action = "snapshot";
  } else if (section === "ssm") {
    if (second === "wallets") {
      return null;
    } else if (second === "delegate") {
      domain = "wallet";
      action = "delegate";
      safety = "sensitive";
    } else if (second === "delegate-estimate") {
      domain = "wallet";
      action = "delegate-estimate";
      safety = "read";
    } else if (second === "sign") {
      domain = "wallet";
      action = "sign";
      safety = "sensitive";
    } else if (second === "sign-status") {
      domain = "wallet";
      action = "sign-status";
    } else if (second === "policies") {
      return null;
    } else if (second === "audit") {
      return null;
    } else if (second === "webauthn") {
      return null;
    }
  } else if (section === "swap") {
    domain = "swap";
    action = second || "quote";
    safety = action === "build" ? "sensitive" : action === "quote" ? "prepare" : "read";
  } else if (section === "bridge") {
    domain = "bridge";
    action = second || "quote";
    safety = action === "build" ? "sensitive" : action === "quote" ? "prepare" : "read";
  } else if (section === "token") {
    domain = "token";
    action = second || "search";
  } else if (section === "chain") {
    domain = "chain";
    action = normalizeChainAction(second);
    safety = action === "broadcast" || action === "transfer" ? "sensitive" : action === "estimate" ? "prepare" : "read";
  } else if (section === "security") {
    domain = "security";
    action = second || "check";
    safety = action === "override" ? "sensitive" : "prepare";
  }

  return {
    command: normalized,
    args,
    domain,
    action,
    kind: domainKind[domain],
    safety,
    flags,
    chain: chainFrom(domain, action, positional, flags),
    walletId: walletFrom(domain, positional, flags),
    address: addressFrom(domain, positional, flags),
    hash: hashFrom(domain, action, positional, flags),
    amount: textFrom(flags.amount),
    protocol: textFrom(flags.protocol) || textFrom(flags.bridge),
  };
}

export function commandLabel(command: ParsedWeb3Command) {
  const domain = command.domain === "unknown" ? "Web3" : titleCase(command.domain);
  const action = command.action === "run" ? "" : titleCase(command.action);
  return [domain, action].filter(Boolean).join(" ");
}

export function commandSubject(command: ParsedWeb3Command) {
  return command.walletId || command.chain || command.address || command.hash || command.protocol || "";
}

function normalizeChainAction(action: string) {
  if (action === "tx") return "transaction";
  return action || "gas";
}

function chainFrom(
  domain: Web3CommandDomain,
  action: string,
  positional: string[],
  flags: Record<string, string | boolean>,
) {
  const flagChain =
    textFrom(flags.chain) || textFrom(flags["from-chain"]) || textFrom(flags["to-chain"]) || "";
  if (flagChain) return flagChain;
  if (domain === "portfolio") return positional[1];
  if (domain === "chain") return positional[2];
  return "";
}

function walletFrom(
  domain: Web3CommandDomain,
  positional: string[],
  flags: Record<string, string | boolean>,
) {
  const flagWallet = textFrom(flags.wallet);
  if (flagWallet) return flagWallet;
  if (domain === "wallet" && positional[1] === "wallets") return positional[3];
  if (domain === "policy") return textFrom(flags.wallet);
  return "";
}

function addressFrom(
  domain: Web3CommandDomain,
  positional: string[],
  flags: Record<string, string | boolean>,
) {
  if (domain === "portfolio") return positional[2];
  return (
    textFrom(flags.address) ||
    textFrom(flags["from-address"]) ||
    textFrom(flags.from) ||
    textFrom(flags.owner) ||
    ""
  );
}

function hashFrom(
  domain: Web3CommandDomain,
  action: string,
  positional: string[],
  flags: Record<string, string | boolean>,
) {
  if (domain === "chain" && action === "transaction") return positional[3];
  return textFrom(flags.hash) || textFrom(flags.tx_hash) || textFrom(flags["tx-hash"]) || "";
}

function parseFlags(args: string[]) {
  const flags: Record<string, string | boolean> = {};
  const positional: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    const value = args[index];
    if (!value.startsWith("--")) {
      positional.push(value);
      continue;
    }
    const flag = value.slice(2);
    const equalsIndex = flag.indexOf("=");
    if (equalsIndex >= 0) {
      flags[flag.slice(0, equalsIndex)] = flag.slice(equalsIndex + 1);
      continue;
    }
    const next = args[index + 1];
    if (next && !next.startsWith("-")) {
      flags[flag] = next;
      index += 1;
    } else {
      flags[flag] = true;
    }
  }
  return { flags, positional };
}

function tokenize(command: string) {
  const tokens: string[] = [];
  const regex = /"([^"]*)"|'([^']*)'|(\S+)/g;
  let match: RegExpExecArray | null;
  while ((match = regex.exec(command)) !== null) {
    tokens.push(match[1] ?? match[2] ?? match[3] ?? "");
  }
  return tokens;
}

function stripTrailingShellRedirections(tokens: string[]) {
  let end = tokens.length;
  while (end > 0) {
    if (end > 1 && isShellRedirectionOperator(tokens[end - 2])) {
      end -= 2;
      continue;
    }
    if (isShellRedirection(tokens[end - 1])) {
      end -= 1;
      continue;
    }
    break;
  }
  return tokens.slice(0, end);
}

function isShellRedirection(token: string) {
  return (
    isShellRedirectionOperator(token) ||
    /^(?:\d*)?(?:>>?|<<?).+$/.test(token) ||
    /^(?:\d*)?>&\d+$/.test(token) ||
    /^&>.+$/.test(token)
  );
}

function isShellRedirectionOperator(token: string) {
  return /^(?:\d*)?(?:>>?|<<?)$/.test(token) || /^&>$/.test(token);
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function textFrom(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function titleCase(value: string) {
  return value.replace(/-/g, " ").replace(/\b\w/g, (char) => char.toUpperCase());
}
