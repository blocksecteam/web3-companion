// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import i18n from "@/i18n";
import { apiURL } from "@/lib/api-base";
import { fetchWithAuth } from "@/lib/auth-session";
import { normalizeRecipientWhitelistAddresses } from "@/lib/policy-whitelist";
import type { PublicKeyCredentialRequestOptionsJSON } from "@simplewebauthn/browser";
import type { AuthenticationCredentialPayload } from "@/lib/auth-client";
import type { User } from "@/types/chat";

export type PagedResult<T> = { items: T[]; hasMore: boolean };

export type LLMProviderKind = "deepseek" | "openrouter" | "custom";

export type LLMProviderSettings = {
  provider: LLMProviderKind;
  baseUrl: string;
  modelName: string;
  apiKeyConfigured: boolean;
  apiKeyHint?: string;
  verifiedAt?: string;
  restartRequired: boolean;
  restarted: boolean;
};

export type RPCChainSettings = {
  name: string;
  chainId: number;
  nativeSymbol: string;
  rpcUrlHint?: string;
  url?: string;
  verifiedAt?: string;
};

export type RPCSettings = {
  chains: RPCChainSettings[];
};

export type SettingsWallet = {
  walletId: string;
  address: string;
  isDefault: boolean;
  createdAt?: string;
  policyInstanceId?: string;
  policyDefinitionId?: string;
  policyDefinitionVersionId?: string;
  chains: Array<{ chain: string; delegationStatus: string }>;
};

export type WalletChallenge = {
  status: "pending_user_auth";
  challengeId: string;
  signUrl: string;
  timeoutAt?: string;
  summary?: PasskeyChallengeSummary;
};

export type PasskeyChallengeStatus = "pending" | "approved" | "timeout" | "cancelled" | "used" | "completed" | string;

export type PasskeyChallengeSummary = {
  operation: string;
  title: string;
  walletId?: string;
  policyId?: string;
  details: Array<{ label: string; value: string }>;
};

export type PasskeyChallenge = {
  challengeId: string;
  type: string;
  status: PasskeyChallengeStatus;
  createdAt?: string;
  expiresAt?: string;
  summary?: PasskeyChallengeSummary;
  publicKey?: PublicKeyCredentialRequestOptionsJSON;
};

export type PolicyMode = "manual" | "auto";

export type PolicyDefinition = {
  policyDefinitionId: string;
  currentVersionId: string;
  name: string;
  isDefault: boolean;
  version: number;
  mode: PolicyMode;
  maxPerTransactionUsd: number;
  dailyTransferLimitUsd: number;
  dailyTradeLimitUsd: number;
  autoSignEnabled: boolean;
  autoSignMaxPerTxUsd: number;
  autoSignDailyLimitUsd: number;
  autoSignMaxCountDay: number;
  recipientWhitelistEnabled: boolean;
  recipientWhitelistAddrs: string[];
  createdAt?: string;
  updatedAt?: string;
};

export type PolicyUsage = {
  walletId: string;
  policyInstanceId: string;
  mode: PolicyMode;
  // 限额
  maxPerTransactionUsd: number;
  dailyTransferLimitUsd: number;
  dailyTradeLimitUsd: number;
  autoSignMaxPerTxUsd: number;
  autoSignDailyLimitUsd: number;
  autoSignMaxCountDay: number;
  // 当日用量
  dailyTransferUsedUsd: number;
  dailyTradeUsedUsd: number;
  autoSignDailyUsedUsd: number;
  autoSignCountToday: number;
  lastResetDate: string;
};

export type PolicyInput = {
  walletId: string;
  name: string;
  mode: PolicyMode;
  maxPerTransactionUsd: number;
  dailyTransferLimitUsd: number;
  dailyTradeLimitUsd: number;
  autoSignEnabled: boolean;
  autoSignMaxPerTxUsd: number;
  autoSignDailyLimitUsd: number;
  autoSignMaxCountDay: number;
  recipientWhitelistEnabled: boolean;
  recipientWhitelistAddrs: string[];
};

export type WalletExportResult = {
  status: "exported";
  walletId: string;
  address: string;
  privateKey: string;
  deleted: boolean;
};

export type WalletDeleteResult = {
  status: "deleted";
  walletId: string;
};

export async function fetchLLMProvider(user: User) {
  return normalizeLLMProvider(await apiGet<unknown>("/api/v1/user/settings/llm", user));
}

export async function saveLLMProvider(user: User, input: { provider: LLMProviderKind; baseUrl: string; modelName: string; apiKey?: string }) {
  return normalizeLLMProvider(
    await apiJson<unknown>("/api/v1/user/settings/llm", user, "PUT", {
      provider: input.provider,
      base_url: input.baseUrl,
      model_name: input.modelName,
      api_key: input.apiKey ?? "",
    }),
  );
}

export async function verifyLLMProvider(user: User, input: { provider: LLMProviderKind; baseUrl: string; modelName: string; apiKey?: string }) {
  return normalizeLLMProvider(
    await apiJson<unknown>("/api/v1/user/settings/llm/verify", user, "POST", {
      provider: input.provider,
      base_url: input.baseUrl,
      model_name: input.modelName,
      api_key: input.apiKey ?? "",
    }),
  );
}

export async function fetchRPCSettings(user: User) {
  return normalizeRPCSettings(await apiGet<unknown>("/api/v1/user/settings/rpc", user));
}

export async function saveRPCSettings(user: User, chains: Array<{ name: string; url: string }>) {
  return normalizeRPCSettings(
    await apiJson<unknown>("/api/v1/user/settings/rpc", user, "PUT", {
      chains: rpcPayload(chains),
    }),
  );
}

export async function verifyRPCSettings(user: User, chains: Array<{ name: string; url: string }>) {
  return normalizeRPCSettings(
    await apiJson<unknown>("/api/v1/user/settings/rpc/verify", user, "POST", {
      chains: rpcPayload(chains),
    }),
  );
}

export type AddressLabelProviderKind = "disabled" | "metasleuth" | "x402";

export type ProvidersSettings = {
  portalsApiKeyConfigured: boolean;
  portalsApiKeyHint?: string;
  metaSleuthApiKeyConfigured: boolean;
  metaSleuthApiKeyHint?: string;
  addressLabelProvider: AddressLabelProviderKind;
};

export async function fetchProviders(user: User): Promise<ProvidersSettings> {
  return normalizeProviders(await apiGet<unknown>("/api/v1/user/settings/providers", user));
}

export async function saveProviders(
  user: User,
  input: { portalsApiKey?: string; metaSleuthApiKey?: string; addressLabelProvider?: string },
): Promise<ProvidersSettings> {
  return normalizeProviders(
    await apiJson<unknown>("/api/v1/user/settings/providers", user, "PUT", {
      portals_api_key: input.portalsApiKey ?? "",
      metasleuth_api_key: input.metaSleuthApiKey ?? "",
      address_label_provider: input.addressLabelProvider ?? "",
    }),
  );
}

function normalizeProviders(value: unknown): ProvidersSettings {
  const r = asRecord(value);
  const provider = textFrom(r.address_label_provider);
  return {
    portalsApiKeyConfigured: Boolean(r.portals_api_key_configured),
    portalsApiKeyHint: textFrom(r.portals_api_key_hint) || undefined,
    metaSleuthApiKeyConfigured: Boolean(r.metasleuth_api_key_configured),
    metaSleuthApiKeyHint: textFrom(r.metasleuth_api_key_hint) || undefined,
    addressLabelProvider: (provider === "metasleuth" || provider === "x402" ? provider : "disabled") as AddressLabelProviderKind,
  };
}

export async function verifyPortalsKey(user: User, apiKey?: string): Promise<{ status: string }> {
  return apiJson("/api/v1/user/settings/providers/verify-portals", user, "POST", {
    api_key: apiKey ?? "",
  });
}

export async function verifyMetaSleuthKey(user: User, apiKey?: string): Promise<{ status: string }> {
  return apiJson("/api/v1/user/settings/providers/verify-metasleuth", user, "POST", {
    api_key: apiKey ?? "",
  });
}



export async function fetchSSMStatus(user: User): Promise<{ encryptionMode: string }> {
  const response = asRecord(await apiGet<unknown>("/api/v1/user/ssm-status", user));
  return { encryptionMode: typeof response.encryption_mode === "string" ? response.encryption_mode : "unknown" };
}

export async function fetchSettingsWallets(user: User, offset = 0): Promise<PagedResult<SettingsWallet>> {
  const response = asRecord(await apiGet<unknown>(`/api/v1/user/wallets?offset=${offset}`, user));
  const wallets = Array.isArray(response.wallets) ? response.wallets : [];
  return {
    items: wallets.map(normalizeSettingsWallet),
    hasMore: response.has_more === true,
  };
}

export type PolicySummary = { manual: number; auto: number; defaultName: string };

export async function fetchPolicyDefinitions(
  user: User,
  offset = 0,
): Promise<{ items: PolicyDefinition[]; hasMore: boolean; summary?: PolicySummary }> {
  const response = asRecord(await apiGet<unknown>(`/api/v1/user/policy-definitions?offset=${offset}`, user));
  const definitions = Array.isArray(response.policy_definitions) ? response.policy_definitions : [];
  const result: { items: PolicyDefinition[]; hasMore: boolean; summary?: PolicySummary } = {
    items: definitions.map(normalizePolicyDefinition),
    hasMore: response.has_more === true,
  };
  if (offset === 0 && response.summary && typeof response.summary === "object") {
    const s = asRecord(response.summary);
    result.summary = {
      manual: typeof s.manual === "number" ? s.manual : 0,
      auto: typeof s.auto === "number" ? s.auto : 0,
      defaultName: (typeof s.default_name === "string" && s.default_name) ? s.default_name : "default",
    };
  }
  return result;
}

export async function fetchAllPolicyDefinitions(user: User): Promise<PolicyDefinition[]> {
  const response = asRecord(await apiGet<unknown>(`/api/v1/user/policy-definitions?limit=999&offset=0`, user));
  const definitions = Array.isArray(response.policy_definitions) ? response.policy_definitions : [];
  return definitions.map(normalizePolicyDefinition);
}

export async function fetchWalletPolicyUsage(user: User, walletId: string): Promise<PolicyUsage> {
  const r = asRecord(await apiGet<unknown>(`/api/v1/user/wallets/${encodeURIComponent(walletId)}/policy-usage`, user));
  return {
    walletId: String(r.wallet_id ?? ""),
    policyInstanceId: String(r.policy_instance_id ?? ""),
    mode: (r.mode === "auto" ? "auto" : "manual") as PolicyMode,
    maxPerTransactionUsd: Number(r.max_per_transaction_usd ?? 0),
    dailyTransferLimitUsd: Number(r.daily_transfer_limit_usd ?? 0),
    dailyTradeLimitUsd: Number(r.daily_trade_limit_usd ?? 0),
    autoSignMaxPerTxUsd: Number(r.auto_sign_max_per_tx_usd ?? 0),
    autoSignDailyLimitUsd: Number(r.auto_sign_daily_limit_usd ?? 0),
    autoSignMaxCountDay: Number(r.auto_sign_max_count_day ?? 0),
    dailyTransferUsedUsd: Number(r.daily_transfer_used_usd ?? 0),
    dailyTradeUsedUsd: Number(r.daily_trade_used_usd ?? 0),
    autoSignDailyUsedUsd: Number(r.auto_sign_daily_used_usd ?? 0),
    autoSignCountToday: Number(r.auto_sign_count_today ?? 0),
    lastResetDate: String(r.last_reset_date ?? ""),
  };
}

export async function requestPolicyDefinitionCreate(user: User, input: PolicyInput) {
  return normalizeWalletChallenge(
    await apiJson<unknown>("/api/v1/user/policy-definitions", user, "POST", policyPayload(input)),
  );
}

export async function confirmPolicyDefinitionCreate(user: User, input: PolicyInput, challengeId: string) {
  return normalizePolicyDefinition(
    await apiJson<unknown>("/api/v1/user/policy-definitions", user, "POST", {
      ...policyPayload(input),
      challenge_id: challengeId,
    }),
  );
}

export async function requestPolicyDefinitionUpdate(user: User, definitionId: string, input: PolicyInput) {
  return normalizeWalletChallenge(
    await apiJson<unknown>(`/api/v1/user/policy-definitions/${encodeURIComponent(definitionId)}`, user, "PUT", policyPayload(input)),
  );
}

export async function confirmPolicyDefinitionUpdate(user: User, definitionId: string, input: PolicyInput, challengeId: string) {
  return normalizePolicyDefinition(
    await apiJson<unknown>(`/api/v1/user/policy-definitions/${encodeURIComponent(definitionId)}`, user, "PUT", {
      ...policyPayload(input),
      challenge_id: challengeId,
    }),
  );
}

export async function requestPolicyDefinitionDelete(user: User, definitionId: string) {
  return normalizeWalletChallenge(
    await apiJson<unknown>(`/api/v1/user/policy-definitions/${encodeURIComponent(definitionId)}`, user, "DELETE", {}),
  );
}

export async function confirmPolicyDefinitionDelete(user: User, definitionId: string, challengeId: string) {
  return apiJson<unknown>(
    `/api/v1/user/policy-definitions/${encodeURIComponent(definitionId)}?challenge_id=${encodeURIComponent(challengeId)}`,
    user,
    "DELETE",
    {},
  );
}

export async function setDefaultPolicyDefinition(user: User, definitionId: string): Promise<void> {
    await apiJson<unknown>(
        `/api/v1/user/policy-definitions/${encodeURIComponent(definitionId)}/set-default`,
        user,
        "PATCH",
        {},
    );
}

export async function setDefaultWallet(user: User, walletId: string): Promise<void> {
    await apiJson<unknown>(
        `/api/v1/user/wallets/${encodeURIComponent(walletId)}/set-default`,
        user,
        "PATCH",
        {},
    );
}

export async function requestWalletPolicyBinding(user: User, walletId: string, policyDefinitionId: string) {
  return normalizeWalletChallenge(
    await apiJson<unknown>(`/api/v1/user/wallets/${encodeURIComponent(walletId)}/policy-binding`, user, "PUT", {
      policy_definition_id: policyDefinitionId,
    }),
  );
}

export async function confirmWalletPolicyBinding(user: User, walletId: string, policyDefinitionId: string, challengeId: string) {
  return asRecord(
    await apiJson<unknown>(`/api/v1/user/wallets/${encodeURIComponent(walletId)}/policy-binding`, user, "PUT", {
      policy_definition_id: policyDefinitionId,
      challenge_id: challengeId,
    }),
  );
}

export async function createSettingsWallet(user: User, walletId: string) {
  return normalizeSettingsWallet(
    await apiJson<unknown>("/api/v1/user/wallets", user, "POST", {
      wallet_id: walletId,
    }),
  );
}

export async function requestWalletDelete(user: User, walletId: string, confirmation: string) {
  return normalizeWalletChallenge(
    await apiJson<unknown>(`/api/v1/user/wallets/${encodeURIComponent(walletId)}`, user, "DELETE", {
      confirmation,
    }),
  );
}

export async function confirmWalletDelete(user: User, walletId: string, confirmation: string, challengeId: string) {
  return normalizeWalletDeleteResult(
    await apiJson<unknown>(
      `/api/v1/user/wallets/${encodeURIComponent(walletId)}?challenge_id=${encodeURIComponent(challengeId)}`,
      user,
      "DELETE",
      { confirmation },
    ),
  );
}

export async function requestWalletExport(user: User, walletId: string) {
  return normalizeWalletChallenge(
    await apiJson<unknown>(`/api/v1/user/wallets/${encodeURIComponent(walletId)}/export`, user, "POST", {}),
  );
}

export async function confirmWalletExport(user: User, walletId: string, challengeId: string) {
  return normalizeWalletExportResult(
    await apiJson<unknown>(
      `/api/v1/user/wallets/${encodeURIComponent(walletId)}/export?challenge_id=${encodeURIComponent(challengeId)}`,
      user,
      "POST",
      {},
    ),
  );
}

export async function fetchPasskeyChallenge(user: User, challengeId: string) {
  return normalizePasskeyChallenge(await apiGet<unknown>(`/api/v1/ssm/webauthn/challenge/${encodeURIComponent(challengeId)}`, user));
}

export async function cancelPasskeyChallenge(user: User, challengeId: string) {
  return apiJson<unknown>(`/api/v1/ssm/webauthn/challenge/${encodeURIComponent(challengeId)}/cancel`, user, "POST", {});
}

export async function fetchPublicPasskeyChallenge(challengeId: string) {
  const response = await fetch(apiURL(`/auth/challenge/${encodeURIComponent(challengeId)}`));
  if (!response.ok) throw await apiError(response);
  return normalizePasskeyChallenge(await response.json());
}

export async function submitPasskeyConfirmation(assertion: AuthenticationCredentialPayload) {
  const response = await fetch(apiURL("/auth/confirm"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify(assertion),
  });
  if (!response.ok) throw await apiError(response);
  return response.json() as Promise<unknown>;
}

export function normalizeLLMProvider(value: unknown): LLMProviderSettings {
  const record = asRecord(value);
  return {
    provider: providerFrom(record.provider),
    baseUrl: textFrom(record.base_url),
    modelName: textFrom(record.model_name),
    apiKeyConfigured: Boolean(record.api_key_configured),
    apiKeyHint: textFrom(record.api_key_hint) || undefined,
    verifiedAt: textFrom(record.verified_at) || undefined,
    restartRequired: Boolean(record.restart_required),
    restarted: Boolean(record.restarted),
  };
}

export function normalizeRPCSettings(value: unknown): RPCSettings {
  const record = asRecord(value);
  const chains = Array.isArray(record.chains) ? record.chains : [];
  return { chains: chains.map(normalizeRPCChain).filter((chain) => chain.name) };
}

export function normalizeSettingsWallet(value: unknown): SettingsWallet {
  const record = asRecord(value);
  const rawChains = Array.isArray(record.chains) ? record.chains : [];
  const chains = rawChains.map((c: unknown) => {
    const r = asRecord(c);
    return { chain: textFrom(r.chain), delegationStatus: textFrom(r.delegation_status) };
  });
  return {
    walletId: textFrom(record.wallet_id),
    address: textFrom(record.address),
    isDefault: Boolean(record.is_default),
    createdAt: textFrom(record.created_at) || undefined,
    policyInstanceId: textFrom(record.policy_instance_id) || undefined,
    policyDefinitionId: textFrom(record.policy_definition_id) || undefined,
    policyDefinitionVersionId: textFrom(record.policy_definition_version_id) || undefined,
    chains,
  };
}

export function normalizePolicyDefinition(value: unknown): PolicyDefinition {
  const record = asRecord(value);
  return {
    policyDefinitionId: textFrom(record.policy_definition_id),
    currentVersionId: textFrom(record.current_version_id),
    name: textFrom(record.name) || "default",
    isDefault: Boolean(record.is_default),
    version: Math.trunc(numberFrom(record.version)),
    mode: policyModeFrom(record.mode),
    maxPerTransactionUsd: numberFrom(record.max_per_transaction_usd),
    dailyTransferLimitUsd: numberFrom(record.daily_transfer_limit_usd),
    dailyTradeLimitUsd: numberFrom(record.daily_trade_limit_usd),
    autoSignEnabled: Boolean(record.auto_sign_enabled),
    autoSignMaxPerTxUsd: numberFrom(record.auto_sign_max_per_tx_usd),
    autoSignDailyLimitUsd: numberFrom(record.auto_sign_daily_limit_usd),
    autoSignMaxCountDay: Math.trunc(numberFrom(record.auto_sign_max_count_day)),
    recipientWhitelistEnabled: Boolean(record.recipient_whitelist_enabled),
    recipientWhitelistAddrs: stringArrayFrom(record.recipient_whitelist_addrs),
    createdAt: textFrom(record.created_at) || undefined,
    updatedAt: textFrom(record.updated_at) || undefined,
  };
}

function normalizeWalletChallenge(value: unknown): WalletChallenge {
  const record = asRecord(value);
  return {
    status: "pending_user_auth",
    challengeId: textFrom(record.challenge_id),
    signUrl: textFrom(record.sign_url),
    timeoutAt: textFrom(record.timeout_at) || undefined,
    summary: normalizeChallengeSummary(record.summary),
  };
}

function normalizePasskeyChallenge(value: unknown): PasskeyChallenge {
  const record = asRecord(value);
  return {
    challengeId: textFrom(record.challenge_id),
    type: textFrom(record.type),
    status: textFrom(record.status) || "pending",
    createdAt: textFrom(record.created_at) || undefined,
    expiresAt: textFrom(record.expires_at) || undefined,
    summary: normalizeChallengeSummary(record.summary),
    publicKey: asPublicKeyOptions(record.public_key),
  };
}

function normalizeChallengeSummary(value: unknown): PasskeyChallengeSummary | undefined {
  const record = asRecord(value);
  const operation = textFrom(record.operation);
  const title = textFrom(record.title);
  if (!operation && !title) return undefined;
  const details = Array.isArray(record.details)
    ? record.details
        .map((detail) => {
          const row = asRecord(detail);
          return { label: textFrom(row.label), value: textFrom(row.value) };
        })
        .filter((detail) => detail.label && detail.value)
    : [];
  return {
    operation,
    title: title || operation,
    walletId: textFrom(record.wallet_id) || undefined,
    policyId: textFrom(record.policy_id) || undefined,
    details,
  };
}

function asPublicKeyOptions(value: unknown): PublicKeyCredentialRequestOptionsJSON | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  return value as PublicKeyCredentialRequestOptionsJSON;
}

function normalizeWalletExportResult(value: unknown): WalletExportResult {
  const record = asRecord(value);
  return {
    status: "exported",
    walletId: textFrom(record.wallet_id),
    address: textFrom(record.address),
    privateKey: textFrom(record.private_key),
    deleted: Boolean(record.deleted),
  };
}

function normalizeWalletDeleteResult(value: unknown): WalletDeleteResult {
  const record = asRecord(value);
  return {
    status: "deleted",
    walletId: textFrom(record.wallet_id),
  };
}

function normalizeRPCChain(value: unknown): RPCChainSettings {
  const record = asRecord(value);
  return {
    name: textFrom(record.name),
    chainId: Math.trunc(numberFrom(record.chain_id)),
    nativeSymbol: textFrom(record.native_symbol),
    rpcUrlHint: textFrom(record.rpc_url_hint) || undefined,
    url: textFrom(record.url) || undefined,
    verifiedAt: textFrom(record.verified_at) || undefined,
  };
}

function policyPayload(input: PolicyInput) {
  return {
    wallet_id: input.walletId,
    name: input.name,
    mode: input.mode,
    max_per_transaction_usd: input.maxPerTransactionUsd,
    daily_transfer_limit_usd: input.dailyTransferLimitUsd,
    daily_trade_limit_usd: input.dailyTradeLimitUsd,
    auto_sign_enabled: input.autoSignEnabled,
    auto_sign_max_per_tx_usd: input.autoSignMaxPerTxUsd,
    auto_sign_daily_limit_usd: input.autoSignDailyLimitUsd,
    auto_sign_max_count_day: input.autoSignMaxCountDay,
    recipient_whitelist_enabled: input.recipientWhitelistEnabled,
    recipient_whitelist_addrs: normalizeRecipientWhitelistAddresses(input.recipientWhitelistAddrs),
  };
}

function rpcPayload(chains: Array<{ name: string; url: string }>) {
  return Object.fromEntries(
    chains
      .map((chain) => [chain.name.trim().toLowerCase(), { url: chain.url.trim() }] as const)
      .filter(([name, chain]) => name && chain.url),
  );
}

async function apiGet<T>(path: string, user: User): Promise<T> {
  const response = await fetchWithAuth(user, path);
  if (!response.ok) throw await apiError(response);
  return response.json() as Promise<T>;
}

async function apiJson<T>(path: string, user: User, method: string, body: unknown): Promise<T> {
  const response = await fetchWithAuth(user, path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await apiError(response);
  return response.json() as Promise<T>;
}

async function apiError(response: Response) {
  let message = i18n.t("errors.requestFailed");
  let code = "";
  try {
    const data = (await response.json()) as { error?: { code?: string; message?: string }; total_usd?: number };
    code = data.error?.code ?? "";
    message = data.error?.message ?? message;
  } catch {
    message = response.statusText || message;
  }
  const error = new Error(message) as Error & { status?: number; code?: string };
  error.status = response.status;
  error.code = code;
  return error;
}

function providerFrom(value: unknown): LLMProviderKind {
  if (value === "openrouter" || value === "custom") return value;
  return "deepseek";
}

function policyModeFrom(value: unknown): PolicyMode {
  return value === "auto" ? "auto" : "manual";
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function stringArrayFrom(value: unknown) {
  if (Array.isArray(value)) return value.map(textFrom).filter(Boolean);
  if (typeof value === "string" && value.trim()) {
    try {
      const parsed = JSON.parse(value) as unknown;
      if (Array.isArray(parsed)) return parsed.map(textFrom).filter(Boolean);
    } catch {
      return value.split(",").map(textFrom).filter(Boolean);
    }
  }
  return [];
}

function textFrom(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function numberFrom(value: unknown) {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}
