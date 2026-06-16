// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import i18n from "@/i18n";
import { apiURL } from "@/lib/api-base";
import { fetchWithAuth } from "@/lib/auth-session";
import type { User } from "@/types/chat";

export type PagedResult<T> = { items: T[]; total: number; page: number; pageSize: number };

export type TransactionCounts = { pending: number; confirmed: number; failed: number };

export type Wallet = {
  walletId: string;
  userId?: string;
  address: string;
  createdAt?: string;
};

export type ChainInfo = {
  name: string;
  chainId: number;
  nativeSymbol: string;
};

export type TokenBalance = {
  tokenAddress: string;
  symbol: string;
  name: string;
  balance: number;
  balanceUsd: number;
  price: number;
  decimals: number;
};

export type PortfolioSnapshot = {
  walletId?: string;
  address: string;
  chain: string;
  totalUsd: number;
  balances: TokenBalance[];
  error?: string;
};

export type PortfolioSummary = {
  totalUsd: number;
  walletCount: number;
  chainCount: number;
  positionCount: number;
};

export type AuditSeverity = "INFO" | "MEDIUM" | "HIGH" | "CRITICAL" | string;

export type AuditLog = {
  eventId: string;
  eventType: string;
  severity: AuditSeverity;
  walletId: string;
  challengeId?: string;
  requestId?: string;
  detail: string;
  txHash?: string;
  createdAt: string;
  rawData: unknown;
};

export type TransactionStatus = "pending" | "confirmed" | "failed" | string;

export type TransactionRecord = {
  chain: string;
  hash: string;
  status: TransactionStatus;
  walletId?: string;
  txType?: string;
  amountUsd: number;
  policyId?: string;
  policyDecision?: string;
  policyUsageStatus?: string;
  submittedAt: string;
  blockTime?: string;
  receivedAmount?: string;
};

export async function fetchWallets(user: User): Promise<Wallet[]> {
  const response = await apiGet<{ wallets?: unknown[] }>("/api/v1/user/wallets", user);
  return (response.wallets ?? []).map(normalizeWallet);
}

export async function fetchChains(): Promise<ChainInfo[]> {
  const response = await fetch(apiURL("/api/v1/chain/supported"));
  if (!response.ok) {
    throw new Error(i18n.t("errors.chains"));
  }
  const chains = (await response.json()) as unknown[];
  return chains.map(normalizeChain);
}

export async function fetchPortfolioSnapshot(user: User, chain: string, address: string) {
  const response = await apiGet<unknown>(
    `/api/v1/portfolio/${encodeURIComponent(chain)}/${encodeURIComponent(address)}`,
    user,
  );
  return normalizePortfolioSnapshot(response);
}

export async function fetchAuditLogs(
  user: User,
  walletId?: string,
  page = 1,
  pageSize = 20,
  severity?: string,
): Promise<PagedResult<AuditLog> & { severityCounts?: Record<string, number> }> {
  const params = new URLSearchParams();
  if (walletId && walletId !== "all") params.set("wallet_id", walletId);
  if (severity && severity !== "all") params.set("severity", severity);
  params.set("offset", String((page - 1) * pageSize));
  params.set("limit", String(pageSize));
  const response = await apiGet<{ logs?: unknown[]; total?: number; severity_counts?: unknown }>(
    `/api/v1/ssm/audit?${params.toString()}`,
    user,
  );
  const result: PagedResult<AuditLog> & { severityCounts?: Record<string, number> } = {
    items: (response.logs ?? []).map(normalizeAuditLog),
    total: typeof response.total === "number" ? response.total : 0,
    page,
    pageSize,
  };
  if (page === 1 && response.severity_counts && typeof response.severity_counts === "object") {
    const sc = response.severity_counts as Record<string, unknown>;
    result.severityCounts = Object.fromEntries(
      Object.entries(sc).map(([k, v]) => [k, typeof v === "number" ? v : 0]),
    );
  }
  return result;
}

export async function fetchTransactions(
  user: User,
  page = 1,
  pageSize = 25,
): Promise<PagedResult<TransactionRecord> & { counts?: TransactionCounts }> {
  const response = await apiGet<{ transactions?: unknown[]; total?: number; counts?: unknown }>(
    `/api/v1/chain/transactions?offset=${(page - 1) * pageSize}&limit=${pageSize}`,
    user,
  );
  const result: PagedResult<TransactionRecord> & { counts?: TransactionCounts } = {
    items: (response.transactions ?? []).map(normalizeTransactionRecord),
    total: typeof response.total === "number" ? response.total : 0,
    page,
    pageSize,
  };
  if (page === 1 && response.counts && typeof response.counts === "object") {
    const c = response.counts as Record<string, unknown>;
    result.counts = {
      pending: typeof c.pending === "number" ? c.pending : 0,
      confirmed: typeof c.confirmed === "number" ? c.confirmed : 0,
      failed: typeof c.failed === "number" ? c.failed : 0,
    };
  }
  return result;
}

export function normalizeWallet(value: unknown): Wallet {
  const record = asRecord(value);
  return {
    walletId: textFrom(record.wallet_id),
    userId: textFrom(record.user_id) || undefined,
    address: textFrom(record.address),
    createdAt: textFrom(record.created_at) || undefined,
  };
}

export function normalizeChain(value: unknown): ChainInfo {
  const record = asRecord(value);
  return {
    name: textFrom(record.name),
    chainId: numberFrom(record.chain_id),
    nativeSymbol: textFrom(record.native_symbol) || "ETH",
  };
}

export function normalizePortfolioSnapshot(value: unknown): PortfolioSnapshot {
  const record = asRecord(value);
  const balances = Array.isArray(record.balances) ? record.balances : [];
  return {
    address: textFrom(record.address),
    chain: textFrom(record.chain),
    totalUsd: numberFrom(record.total_usd),
    balances: balances.map(normalizeTokenBalance).sort((a, b) => b.balanceUsd - a.balanceUsd),
  };
}

export function summarizePortfolio(snapshots: PortfolioSnapshot[]): PortfolioSummary {
  const successful = snapshots.filter((snapshot) => !snapshot.error);
  return {
    totalUsd: successful.reduce((sum, snapshot) => sum + snapshot.totalUsd, 0),
    walletCount: new Set(successful.map((snapshot) => snapshot.walletId || snapshot.address)).size,
    chainCount: new Set(successful.map((snapshot) => snapshot.chain)).size,
    positionCount: successful.reduce((sum, snapshot) => sum + snapshot.balances.length, 0),
  };
}

export function normalizeAuditLog(value: unknown): AuditLog {
  const record = asRecord(value);
  const rawData = record.data;
  const detail = extractAuditDetail(rawData);
  return {
    eventId: textFrom(record.event_id),
    eventType: textFrom(record.event_type),
    severity: textFrom(record.severity),
    walletId: textFrom(record.wallet_id),
    challengeId: textFrom(record.challenge_id) || undefined,
    requestId: textFrom(record.request_id) || undefined,
    detail,
    txHash: extractTxHash(detail),
    createdAt: textFrom(record.created_at),
    rawData,
  };
}

export function normalizeTransactionRecord(value: unknown): TransactionRecord {
  const record = asRecord(value);
  return {
    chain: textFrom(record.chain),
    hash: textFrom(record.hash),
    status: textFrom(record.status) || "pending",
    walletId: textFrom(record.wallet_id) || undefined,
    txType: textFrom(record.tx_type) || undefined,
    amountUsd: numberFrom(record.amount_usd),
    policyId: textFrom(record.policy_id) || undefined,
    policyDecision: textFrom(record.policy_decision) || undefined,
    policyUsageStatus: textFrom(record.policy_usage_status) || undefined,
    submittedAt: textFrom(record.submitted_at),
    blockTime: textFrom(record.confirmed_at) || undefined,
    receivedAmount: typeof record.received_amount === "string" && record.received_amount.trim() ? record.received_amount.trim() : undefined,
  };
}

export function phalconTransactionUrl(tx: Pick<TransactionRecord, "chain" | "hash">) {
  const chain = tx.chain.toLowerCase();
  if (!["ethereum", "bsc", "base", "polygon"].includes(chain)) return null;
  const phalconChain = chain === "ethereum" ? "eth" : chain;
  return `https://app.blocksec.com/phalcon/explorer/tx/${encodeURIComponent(phalconChain)}/${encodeURIComponent(tx.hash)}`;
}

export function extractAuditDetail(data: unknown): string {
  const parsed = typeof data === "string" ? parseJson(data) ?? data : data;
  if (typeof parsed === "string") return parsed;
  const record = asRecord(parsed);
  return (
    textFrom(record.detail) ||
    textFrom(record.reason) ||
    textFrom(record.message) ||
    textFrom(record.status) ||
    JSON.stringify(parsed)
  );
}

function normalizeTokenBalance(value: unknown): TokenBalance {
  const record = asRecord(value);
  return {
    tokenAddress: textFrom(record.tokenAddress) || textFrom(record.address),
    // Leave symbol/name blank when the backend omits them; the renderer
    // (PortfolioPage) substitutes a localized fallback via t(). Hardcoding
    // an English fallback here would leak into Chinese sessions because
    // this lib file has no useTranslation() context.
    symbol: textFrom(record.tokenSymbol) || textFrom(record.symbol),
    name: textFrom(record.tokenName) || textFrom(record.name),
    balance: numberFrom(record.balance),
    balanceUsd: numberFrom(record.balanceUSD) || numberFrom(record.balance_usd),
    price: numberFrom(record.price),
    decimals: numberFrom(record.decimals),
  };
}

async function apiGet<T>(path: string, user: User): Promise<T> {
  const response = await fetchWithAuth(user, path);
  if (!response.ok) {
    throw await apiError(response);
  }
  return response.json() as Promise<T>;
}

async function apiError(response: Response) {
  let message = i18n.t("errors.requestFailed");
  try {
    const data = (await response.json()) as { error?: { message?: string } };
    message = data.error?.message ?? message;
  } catch {
    message = response.statusText || message;
  }
  const error = new Error(message) as Error & { status?: number };
  error.status = response.status;
  return error;
}

function extractTxHash(detail: string) {
  return detail.match(/0x[a-fA-F0-9]{6,64}/)?.[0];
}

function parseJson(value: string) {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return null;
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
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
