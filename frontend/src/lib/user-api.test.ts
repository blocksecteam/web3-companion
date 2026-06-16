// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import {
  normalizeLLMProvider,
  normalizePolicyDefinition,
  normalizeRPCSettings,
  normalizeSettingsWallet,
} from "@/lib/user-api";

describe("normalizeLLMProvider", () => {
  test("normalizes deployment BYOK model settings", () => {
    const provider = normalizeLLMProvider({
      provider: "deepseek",
      base_url: "https://api.deepseek.com/anthropic",
      model_name: "deepseek-v4-pro",
      api_key_configured: true,
      api_key_hint: "sk...1234",
    });

    expect(provider.provider).toBe("deepseek");
    expect(provider.baseUrl).toBe("https://api.deepseek.com/anthropic");
    expect(provider.modelName).toBe("deepseek-v4-pro");
    expect(provider.apiKeyConfigured).toBe(true);
    expect(provider.apiKeyHint).toBe("sk...1234");
  });

  test("normalizes missing BYOK model key status independently from key hint", () => {
    const provider = normalizeLLMProvider({
      provider: "deepseek",
      base_url: "https://api.deepseek.com/anthropic",
      model_name: "deepseek-v4-pro",
      api_key_configured: false,
    });

    expect(provider.apiKeyConfigured).toBe(false);
    expect(provider.apiKeyHint).toBeUndefined();
  });

  test("normalizes provider save restart flags and key hints", () => {
    const provider = normalizeLLMProvider({
      provider: "openrouter",
      base_url: "https://openrouter.ai/api/v1",
      model_name: "anthropic/claude-sonnet-4.5",
      api_key_hint: "sk-o...cret",
      restart_required: true,
      restarted: true,
    });

    expect(provider.provider).toBe("openrouter");
    expect(provider.baseUrl).toBe("https://openrouter.ai/api/v1");
    expect(provider.modelName).toBe("anthropic/claude-sonnet-4.5");
    expect(provider.apiKeyHint).toBe("sk-o...cret");
    expect(provider.restartRequired).toBe(true);
    expect(provider.restarted).toBe(true);
  });
});

describe("normalizeRPCSettings", () => {
  test("normalizes chain RPC settings", () => {
    expect(normalizeRPCSettings({
      chains: [
        {
          name: "ethereum",
          chain_id: 1,
          native_symbol: "ETH",
          rpc_url_hint: "https://...abcd",
          verified_at: "2026-05-22T00:00:00Z",
        },
      ],
    })).toEqual({
      chains: [
        {
          name: "ethereum",
          chainId: 1,
          nativeSymbol: "ETH",
          rpcUrlHint: "https://...abcd",
          verifiedAt: "2026-05-22T00:00:00Z",
        },
      ],
    });
  });
});

describe("normalizeSettingsWallet", () => {
  test("normalizes wallet metadata", () => {
    const wallet = normalizeSettingsWallet({
      wallet_id: "main",
      address: "0xabc",
      policy_definition_id: "pdef_default",
      created_at: "2026-05-15T00:00:00Z",
      chains: [
        { chain: "ethereum", delegation_status: "delegated" },
        { chain: "base", delegation_status: "pending_delegation" },
      ],
    });

    expect(wallet.walletId).toBe("main");
    expect(wallet.address).toBe("0xabc");
    expect(wallet.chains).toEqual([
      { chain: "ethereum", delegationStatus: "delegated" },
      { chain: "base", delegationStatus: "pending_delegation" },
    ]);
    expect(wallet.policyDefinitionId).toBe("pdef_default");
    expect(wallet.createdAt).toBe("2026-05-15T00:00:00Z");
  });
});

describe("normalizePolicyDefinition", () => {
  test("normalizes definition metadata and current version fields", () => {
    const definition = normalizePolicyDefinition({
      policy_definition_id: "pdef_1",
      current_version_id: "pver_2",
      name: "default",
      is_default: true,
      version: 2,
      mode: "manual",
      max_per_transaction_usd: "100",
      recipient_whitelist_addrs: ["0xabc"],
      created_at: "2026-05-15T00:00:00Z",
      updated_at: "2026-05-16T00:00:00Z",
    });

    expect(definition.policyDefinitionId).toBe("pdef_1");
    expect(definition.currentVersionId).toBe("pver_2");
    expect(definition.name).toBe("default");
    expect(definition.isDefault).toBe(true);
    expect(definition.version).toBe(2);
    expect(definition.maxPerTransactionUsd).toBe(100);
    expect(definition.recipientWhitelistAddrs).toEqual(["0xabc"]);
    expect(definition.createdAt).toBe("2026-05-15T00:00:00Z");
  });
});
