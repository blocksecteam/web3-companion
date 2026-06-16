// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readdir, readFile } from "node:fs/promises";
import { extname, join, relative } from "node:path";
import i18n from "@/i18n";

const sourceRoot = join(import.meta.dir);
const staticTranslationCall = /\b(?:i18n\.)?t\(\s*["']([^"'`]+)["']/g;
const hardcodedTextAttribute = /\b(?:aria-label|title|placeholder|label)=["']([A-Za-z][^"']*)["']/g;

async function sourceFiles(dir = sourceRoot): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const files = await Promise.all(entries.map(async (entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    if (![".ts", ".tsx"].includes(extname(entry.name))) return [];
    if (/\.test\.tsx?$/.test(entry.name)) return [];
    return [path];
  }));
  return files.flat();
}

async function staticTranslationKeys() {
  const files = await sourceFiles();
  const keys = new Map<string, Set<string>>();
  await Promise.all(files.map(async (file) => {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(staticTranslationCall)) {
      const [, key] = match;
      if (!key) continue;
      const owners = keys.get(key) ?? new Set<string>();
      owners.add(relative(sourceRoot, file));
      keys.set(key, owners);
    }
  }));
  return keys;
}

function flattenTranslationKeys(value: unknown, prefix = ""): string[] {
  if (Array.isArray(value)) return [prefix];
  if (!value || typeof value !== "object") return prefix ? [prefix] : [];
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    flattenTranslationKeys(child, prefix ? `${prefix}.${key}` : key),
  );
}

describe("frontend copy", () => {
  test("keeps Chinese and English translation trees aligned", () => {
    const zhKeys = flattenTranslationKeys(i18n.getDataByLanguage("zh")?.translation).sort();
    const enKeys = flattenTranslationKeys(i18n.getDataByLanguage("en")?.translation).sort();

    expect(zhKeys).toEqual(enKeys);
  });

  test("covers dynamic translation key families", () => {
    const dynamicKeys = [
      ...["complete", "running", "queued"].map((state) => `landing.states.${state}`),
      ...["deepseek", "openrouter", "custom"].map((provider) => `settings.providers.${provider}`),
      ...["pending", "confirmed", "failed"].map((status) => `transactions.statuses.${status}`),
      ...["timeout", "cancelled", "used", "completed", "unavailable"].map((status) => `passkeyModal.status.${status}`),
      ...[
        "delete_wallet",
        "export_wallet",
        "bind_wallet_policy",
        "create_policy_definition",
        "update_policy_definition",
        "delete_policy_definition",
        "sign",
        "delegate_wallet",
        "security_override",
        "confirm_operation",
      ].map((operation) => `passkeyModal.operations.${operation}`),
      ...[
        "wallet",
        "policy",
        "type",
        "chain",
        "to",
        "amountUsd",
        "reason",
        "securityVerdict",
        "securityWarning",
        "securityBlock",
        "policyName",
        "mode",
        "challenge",
      ].map((label) => `passkeyModal.detailLabels.${label}`),
      ...[
        "web3Portfolio",
        "web3Swap",
        "web3Bridge",
        "web3Token",
        "web3Chain",
        "web3Security",
        "web3Ssm",
        "web3Onboard",
        "sharedPasskey",
        "sharedPreflight",
        "sharedAmountResolution",
      ].flatMap((skill) => [`tool.skills.${skill}.title`, `tool.skills.${skill}.description`]),
      ...["auth", "context", "wallet", "portfolio", "swap", "bridge", "token", "chain", "security", "audit", "policy", "unknown"].map(
        (domain) => `web3.domains.${domain}`,
      ),
      ...[
        "snapshot",
        "list",
        "create",
        "info",
        "delete",
        "export",
        "delegate",
        "sign",
        "sign-status",
        "quote",
        "build",
        "execute",
        "protocols",
        "gas",
        "transaction",
        "broadcast",
        "transfer",
        "check",
        "search",
        "status",
        "run",
      ].map((action) => `web3.actions.${action}`),
      "policies.modeManual",
      "policies.modeAuto",
      "policies.modeManualRow",
      "policies.modeAutoRow",
      "policies.recipientWhitelistMultipleAddresses",
      "policies.recipientWhitelistInvalidLine",
      "streamdown.openExternalLink",
      "streamdown.externalLinkWarning",
      "streamdown.close",
      "streamdown.copyLink",
      "streamdown.copied",
      "streamdown.openLink",
    ];
    const missing = dynamicKeys
      .flatMap((key) => ["zh", "en"].filter((lng) => !i18n.exists(key, { lng })).map((lng) => `${lng}:${key}`))
      .sort();

    expect(missing).toEqual([]);
  });

  test("all static translation calls resolve in every language", async () => {
    const keys = await staticTranslationKeys();
    const missing = [...keys.entries()]
      .flatMap(([key, owners]) =>
        ["zh", "en"]
          .filter((lng) => !i18n.exists(key, { lng }))
          .map((lng) => `${lng}:${key} (${[...owners].sort().join(", ")})`),
      )
      .sort();

    expect(missing).toEqual([]);
  });

  test("does not hardcode translatable text attributes", async () => {
    const files = await sourceFiles();
    const hardcoded = (await Promise.all(files.map(async (file) => {
      const source = await readFile(file, "utf8");
      return [...source.matchAll(hardcodedTextAttribute)].map((match) =>
        `${relative(sourceRoot, file)}: ${match[0]}`,
      );
    }))).flat().sort();

    expect(hardcoded).toEqual([]);
  });

  test("keeps the updated Chinese landing wording", () => {
    i18n.changeLanguage("zh");

    expect(i18n.t("landing.tagline")).toBe("你在Web3世界里的小助手");
    expect(i18n.t("landing.title")).toBe("你的专属Web3助手");
    expect(i18n.t("landing.body")).toBe("自然表达意图，逐步检查工具调用，只在真正理解后批准链上动作。");
    expect(i18n.t("landing.create")).toBe("创建账户");
    expect(i18n.t("landing.draftLabel")).toBe("助手结论");
    expect(i18n.t("landing.draft")).toBe(
      "我找到了将USDC从Ethereum跨链到Base的最佳路径，安全检查已通过，请使用Passkey确认并广播交易。",
    );
  });

  test("keeps the optimized Chinese Web3 companion wording", () => {
    i18n.changeLanguage("zh");

    expect(i18n.t("chat.emptyBadge")).toBe("自然语言表述意图，Passkey验证交易。");
    expect(i18n.t("chat.newConversation")).toBe("新的Web3操作");
    expect(i18n.t("chat.emptyTitle")).toBe("想在Web3世界里做什么？");
    expect(i18n.t("chat.emptyBody")).toBe(
      "用自然语言表达意图，如查询资产、转账、代币兑换或者跨链，助手将理解意图并帮你完成操作，敏感操作需要你的认证。",
    );
    expect(i18n.t("chat.placeholder")).toBe("让助手检查、报价、模拟，或准备链上操作...");
    expect(i18n.t("chat.passkeyAuthenticatedMessage")).toBe("Passkey 已认证");
    expect(i18n.t("chat.suggestedPrompts", { returnObjects: true })).toEqual([
      "我在Web3里有什么资产？",
      "将0.1 ETH兑换为USDT",
      "帮我找到从Ethereum到Base跨链USDT的最佳路径",
      "我最近给谁转过账？",
    ]);
    expect(i18n.t("portfolio.intro")).toBe("按钱包和链查看余额和代币明细。");
    expect(i18n.t("portfolio.fundingBody", { count: 1 })).toBe(
      "1 个钱包首次操作前需要足够的原生代币以支付Gas费用。",
    );
    expect(i18n.t("portfolio.totalValue")).toBe("总资产估值");
    expect(i18n.t("transactions.intro")).toBe("通过助手发送的所有交易。");
    expect(i18n.t("landing.previewSteps.portfolioBody")).toBe("检查余额和代币明细");
    expect(i18n.t("settings.walletBody")).toBe(
      "创建、导出和删除托管钱包，并为每个钱包绑定一个策略定义。部分敏感操作需要Passkey认证确认。",
    );
    expect(i18n.t("policies.currentPoliciesBody")).toBe(
      "这里显示所有策略定义。默认策略不能删除，修改策略定义会重置所有绑定钱包的使用额度。",
    );
  });

  test("has Chinese copy for direct passkey registration", () => {
    i18n.changeLanguage("zh");

    expect(i18n.t("auth.heroTitle")).toBe("邮箱找到账号，Passkey确认身份。");
    expect(i18n.t("auth.steps.register")).toBe("注册");
    expect(i18n.t("auth.emailReadyTitle")).toBe("可以创建 Passkey");
    expect(i18n.t("auth.registerFallback")).toBe("无法开始注册。");
    expect(i18n.t("auth.registerStarted")).toBe("账号已创建，请继续绑定 Passkey。");
  });

  test("uses concise English auth copy", () => {
    i18n.changeLanguage("en");

    expect(i18n.t("auth.title")).toBe("Sign in");
    expect(i18n.t("landing.loginPasskey")).toBe("Login with Passkey");
    expect(i18n.t("auth.heroTitle")).toBe("Your email finds the account. Passkey confirms it is you.");
    expect(i18n.t("auth.heroBody")).toContain("bind a Passkey");
    expect(i18n.t("auth.passkeyLoginTitle")).toBe("Scan your Passkey");
    expect(i18n.t("confirm.usePasskey")).toBe("Use Passkey");
    expect(i18n.t("chat.emptyBadge")).toBe("State your intent naturally. Verify transactions with Passkey.");
    expect(i18n.t("chat.passkeyAuthenticatedMessage")).toBe("Passkey Authenticated");
    expect(i18n.t("passkeyAction.confirming")).toBe("Completing operation...");
  });

  test("localizes web3 skill card copy as skills", () => {
    i18n.changeLanguage("zh");
    expect(i18n.t("tool.skills.web3Swap.title")).toBe("兑换技能");
    expect(i18n.t("tool.skills.web3Ssm.title")).toBe("签名技能");
    expect(i18n.t("tool.skills.sharedPasskey.title")).toBe("Passkey 共用技能");
    expect(i18n.t("tool.skills.web3Swap.title")).not.toContain("助手");

    i18n.changeLanguage("en");
    expect(i18n.t("tool.skills.web3Swap.title")).toBe("Swap skill");
    expect(i18n.t("tool.skills.sharedPasskey.title")).toBe("Shared Passkey skill");
  });

  test("localizes passkey summary copy shown in modals", () => {
    i18n.changeLanguage("zh");
    expect(i18n.t("passkeyModal.eyebrow")).toBe("安全确认");
    expect(i18n.t("passkeyModal.operation")).toBe("待批准操作");
    expect(i18n.t("passkeyModal.operations.delete_wallet")).toBe("删除钱包");
    expect(i18n.t("passkeyModal.operations.sign")).toBe("批准签名");
    expect(i18n.t("passkeyModal.detailLabels.securityVerdict")).toBe("安全结论");
    expect(i18n.t("passkeyModal.detailLabels.amountUsd")).toBe("USD 金额");

    i18n.changeLanguage("en");
    expect(i18n.t("passkeyModal.eyebrow")).toBe("Security confirmation");
    expect(i18n.t("passkeyModal.operation")).toBe("Pending approval");
    expect(i18n.t("passkeyModal.operations.delete_wallet")).toBe("Delete wallet");
    expect(i18n.t("passkeyModal.detailLabels.securityVerdict")).toBe("Security verdict");
  });

  test("localizes streamdown external link modal copy", () => {
    i18n.changeLanguage("zh");
    expect(i18n.t("streamdown.openExternalLink")).toBe("打开外部链接？");
    expect(i18n.t("streamdown.externalLinkWarning")).toBe("你即将访问外部网站。");
    expect(i18n.t("streamdown.copyLink")).toBe("复制链接");
    expect(i18n.t("streamdown.openLink")).toBe("打开链接");

    i18n.changeLanguage("en");
    expect(i18n.t("streamdown.openExternalLink")).toBe("Open external link?");
    expect(i18n.t("streamdown.externalLinkWarning")).toBe("You're about to visit an external website.");
    expect(i18n.t("streamdown.copyLink")).toBe("Copy link");
    expect(i18n.t("streamdown.openLink")).toBe("Open link");
  });

  test("uses one generating label for chat run status", () => {
    i18n.changeLanguage("zh");
    expect(i18n.t("chat.generating")).toBe("生成中");
    expect(i18n.t("chat.agentRunning")).toBe("生成中");
    expect(i18n.t("chat.streaming")).toBe("生成中");

    i18n.changeLanguage("en");
    expect(i18n.t("chat.generating")).toBe("Generating");
    expect(i18n.t("chat.agentRunning")).toBe("Generating");
    expect(i18n.t("chat.streaming")).toBe("Generating");
  });

  test("names supported chains through i18n", () => {
    i18n.changeLanguage("zh");
    expect(i18n.t("chains.ethereum")).toBe("Ethereum");
    expect(i18n.t("chains.base")).toBe("Base");
    expect(i18n.t("chains.polygon")).toBe("Polygon");
    expect(i18n.t("chains.bsc")).toBe("Binance Smart Chain");

    i18n.changeLanguage("en");
    expect(i18n.t("chains.ethereum")).toBe("Ethereum");
    expect(i18n.t("chains.base")).toBe("Base");
    expect(i18n.t("chains.polygon")).toBe("Polygon");
    expect(i18n.t("chains.bsc")).toBe("Binance Smart Chain");
  });

  test("does not use quiet sign-in wording", () => {
    i18n.changeLanguage("zh");
    expect(i18n.t("auth.title")).toBe("登录");
    expect(i18n.t("auth.title")).not.toContain("安静");

    i18n.changeLanguage("en");
    expect(i18n.t("auth.title")).toBe("Sign in");
    expect(i18n.t("auth.title").toLowerCase()).not.toContain("quietly");
  });
});
