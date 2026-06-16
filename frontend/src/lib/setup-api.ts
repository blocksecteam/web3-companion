// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { apiURL } from "@/lib/api-base";

export type SetupStatus = {
  complete: boolean;
  setupMode: boolean;
  chains: SetupChain[];
};

export type SetupChain = {
  name: string;
  chainId: number;
  nativeSymbol: string;
};

export type SetupSecrets = {
  jwtSecret: string;
  ssmKey: string;
  message: string;
};

export type SetupRPCChainResponse = {
  name: string;
  chainId: number;
  nativeSymbol: string;
  rpcUrlHint?: string;
  verifiedAt?: string;
};

export type SetupRPCResponse = {
  chains: SetupRPCChainResponse[];
};

export type SetupLLMResponse = {
  provider: string;
  baseUrl: string;
  modelName: string;
  apiKeyConfigured: boolean;
  apiKeyHint?: string;
  verifiedAt?: string;
};

export async function fetchSetupStatus(): Promise<SetupStatus> {
  const r = await fetch(apiURL("/setup/status"));
  if (!r.ok) throw new Error("Failed to fetch setup status");
  const data = await r.json();
  return {
    complete: Boolean(data.complete),
    setupMode: Boolean(data.setup_mode),
    chains: Array.isArray(data.chains) ? data.chains.map(normalizeChain) : [],
  };
}

export async function fetchSetupSecrets(): Promise<SetupSecrets> {
  const r = await fetch(apiURL("/setup/secrets"));
  if (!r.ok) throw await setupError(r);
  const data = await r.json();
  return {
    jwtSecret: String(data.jwt_secret ?? ""),
    ssmKey: String(data.ssm_key ?? ""),
    message: String(data.message ?? ""),
  };
}

export async function saveSetupRPC(
  chains: Record<string, { url: string }>,
): Promise<SetupRPCResponse> {
  const data = await setupPost<{ chains?: unknown[] }>("/setup/rpc", { chains });
  return normalizeRPCResponse(data);
}

export async function verifySetupRPC(
  chains: Record<string, { url: string }>,
): Promise<SetupRPCResponse> {
  const data = await setupPost<{ chains?: unknown[] }>("/setup/rpc/verify", { chains });
  return normalizeRPCResponse(data);
}

export async function saveSetupLLM(input: {
  provider: string;
  baseUrl: string;
  modelName: string;
  apiKey: string;
}): Promise<SetupLLMResponse> {
  const data = await setupPost<Record<string, unknown>>("/setup/llm", {
    provider: input.provider,
    base_url: input.baseUrl,
    model_name: input.modelName,
    api_key: input.apiKey,
  });
  return normalizeLLMResponse(data);
}

export async function verifySetupLLM(input: {
  provider: string;
  baseUrl: string;
  modelName: string;
  apiKey: string;
}): Promise<SetupLLMResponse> {
  const data = await setupPost<Record<string, unknown>>("/setup/llm/verify", {
    provider: input.provider,
    base_url: input.baseUrl,
    model_name: input.modelName,
    api_key: input.apiKey,
  });
  return normalizeLLMResponse(data);
}

export type SetupProvidersResponse = {
  portalsApiKeyConfigured: boolean;
  portalsApiKeyHint?: string;
  metaSleuthApiKeyConfigured: boolean;
  metaSleuthApiKeyHint?: string;
};

export async function saveSetupProviders(input: {
  portalsApiKey: string;
  metaSleuthApiKey: string;
  addressLabelProvider?: string;
}): Promise<SetupProvidersResponse> {
  const data = await setupPost<Record<string, unknown>>("/setup/providers", {
    portals_api_key: input.portalsApiKey,
    metasleuth_api_key: input.metaSleuthApiKey,
    address_label_provider: input.addressLabelProvider ?? "disabled",
  });
  return {
    portalsApiKeyConfigured: Boolean(data.portals_api_key_configured),
    portalsApiKeyHint: data.portals_api_key_hint ? String(data.portals_api_key_hint) : undefined,
    metaSleuthApiKeyConfigured: Boolean(data.metasleuth_api_key_configured),
    metaSleuthApiKeyHint: data.metasleuth_api_key_hint ? String(data.metasleuth_api_key_hint) : undefined,
  };
}

export async function completeSetup(): Promise<{ status: string; message: string }> {
  return setupPost("/setup/complete", {});
}

async function setupPost<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(apiURL(path), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw await setupError(r);
  return r.json() as Promise<T>;
}

async function setupError(response: Response): Promise<Error> {
  let message = `Request failed (${response.status})`;
  try {
    const data = (await response.json()) as { error?: { message?: string } };
    if (data.error?.message) message = data.error.message;
  } catch {
    // ignore parse errors
  }
  return new Error(message);
}

function normalizeChain(value: unknown): SetupChain {
  const r = (value && typeof value === "object" ? value : {}) as Record<string, unknown>;
  return {
    name: String(r.name ?? ""),
    chainId: Number(r.chain_id ?? 0),
    nativeSymbol: String(r.native_symbol ?? ""),
  };
}

function normalizeRPCResponse(data: { chains?: unknown[] }): SetupRPCResponse {
  return {
    chains: Array.isArray(data.chains)
      ? data.chains.map((c) => {
          const r = (c && typeof c === "object" ? c : {}) as Record<string, unknown>;
          return {
            name: String(r.name ?? ""),
            chainId: Number(r.chain_id ?? 0),
            nativeSymbol: String(r.native_symbol ?? ""),
            rpcUrlHint: r.rpc_url_hint ? String(r.rpc_url_hint) : undefined,
            verifiedAt: r.verified_at ? String(r.verified_at) : undefined,
          };
        })
      : [],
  };
}

function normalizeLLMResponse(data: Record<string, unknown>): SetupLLMResponse {
  return {
    provider: String(data.provider ?? ""),
    baseUrl: String(data.base_url ?? ""),
    modelName: String(data.model_name ?? ""),
    apiKeyConfigured: Boolean(data.api_key_configured),
    apiKeyHint: data.api_key_hint ? String(data.api_key_hint) : undefined,
    verifiedAt: data.verified_at ? String(data.verified_at) : undefined,
  };
}
