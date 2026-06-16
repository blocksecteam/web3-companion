// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import {
  extractAuditDetail,
  fetchChains,
  phalconTransactionUrl,
  normalizeAuditLog,
  normalizePortfolioSnapshot,
  normalizeTransactionRecord,
  summarizePortfolio,
} from "@/lib/web3-api";

describe("normalizePortfolioSnapshot", () => {
  test("normalizes backend and docs token shapes", () => {
    const snapshot = normalizePortfolioSnapshot({
      chain: "ethereum",
      address: "0xwallet",
      total_usd: 12.5,
      balances: [
        {
          address: "0xtoken",
          symbol: "USDC",
          name: "USD Coin",
          balance: 10,
          balanceUSD: 10,
          price: 1,
          decimals: 6,
        },
        {
          tokenAddress: "0xeth",
          tokenSymbol: "ETH",
          tokenName: "Ether",
          balance: 0.001,
          balanceUSD: 2.5,
          price: 2500,
          decimals: 18,
        },
      ],
    });

    expect(snapshot.totalUsd).toBe(12.5);
    expect(snapshot.balances.map((balance) => balance.symbol)).toEqual(["USDC", "ETH"]);
    expect(snapshot.balances[0].tokenAddress).toBe("0xtoken");
  });

  test("summarizes multiple wallets and chains", () => {
    const summary = summarizePortfolio([
      {
        walletId: "main",
        address: "0x1",
        chain: "ethereum",
        totalUsd: 10,
        balances: [{ tokenAddress: "native", symbol: "ETH", name: "Ether", balance: 1, balanceUsd: 10, price: 10, decimals: 18 }],
      },
      {
        walletId: "safe",
        address: "0x2",
        chain: "base",
        totalUsd: 5,
        balances: [],
      },
    ]);

    expect(summary.totalUsd).toBe(15);
    expect(summary.walletCount).toBe(2);
    expect(summary.chainCount).toBe(2);
    expect(summary.positionCount).toBe(1);
  });
});

describe("normalizeAuditLog", () => {
  test("parses detail JSON and extracts transaction hashes", () => {
    const log = normalizeAuditLog({
      event_id: "evt_1",
      event_type: "SIGN",
      severity: "INFO",
      wallet_id: "main",
      data: "{\"detail\":\"signed approved transaction 0xabc123\"}",
      created_at: "2026-05-14T12:00:00Z",
    });

    expect(log.detail).toBe("signed approved transaction 0xabc123");
    expect(log.txHash).toBe("0xabc123");
    expect(log.walletId).toBe("main");
  });

  test("extractAuditDetail handles plain strings and records", () => {
    expect(extractAuditDetail("{\"detail\":\"wallet created\"}")).toBe("wallet created");
    expect(extractAuditDetail({ reason: "policy_rejected" })).toBe("policy_rejected");
    expect(extractAuditDetail("raw event")).toBe("raw event");
  });
});

describe("normalizeTransactionRecord", () => {
  test("normalizes transaction timestamps and builds Phalcon URLs", () => {
    const tx = normalizeTransactionRecord({
      chain: "base",
      hash: "0x2222222222222222222222222222222222222222222222222222222222222222",
      status: "confirmed",
      submitted_at: "2026-05-15T12:00:00Z",
      confirmed_at: "2026-05-15T12:01:30Z",
    });

    expect(tx.chain).toBe("base");
    expect(tx.status).toBe("confirmed");
    expect(tx.submittedAt).toBe("2026-05-15T12:00:00Z");
    expect(tx.blockTime).toBe("2026-05-15T12:01:30Z");
    expect(phalconTransactionUrl(tx)).toBe(
      "https://app.blocksec.com/phalcon/explorer/tx/base/0x2222222222222222222222222222222222222222222222222222222222222222",
    );
  });

  test("uses eth slug for ethereum chain in Phalcon URL", () => {
    const tx = normalizeTransactionRecord({
      chain: "ethereum",
      hash: "0x3333333333333333333333333333333333333333333333333333333333333333",
      status: "confirmed",
      submitted_at: "2026-05-15T12:00:00Z",
    });
    expect(phalconTransactionUrl(tx)).toBe(
      "https://app.blocksec.com/phalcon/explorer/tx/eth/0x3333333333333333333333333333333333333333333333333333333333333333",
    );
  });

  test("returns no Phalcon URL for unsupported chains", () => {
    const tx = normalizeTransactionRecord({
      chain: "arbitrum",
      hash: "0xabc",
      status: "pending",
      submitted_at: "2026-05-15T12:00:00Z",
    });

    expect(phalconTransactionUrl(tx)).toBeNull();
  });
});

describe("fetchChains", () => {
  test("loads supported chains from the backend supported endpoint", async () => {
    const previousFetch = globalThis.fetch;
    const seen: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      seen.push(String(input));
      return new Response(JSON.stringify([{ name: "base", chain_id: 8453, native_symbol: "ETH" }]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const chains = await fetchChains();

      expect(seen).toEqual(["/api/v1/chain/supported"]);
      expect(chains).toEqual([{ name: "base", chainId: 8453, nativeSymbol: "ETH" }]);
    } finally {
      globalThis.fetch = previousFetch;
    }
  });
});
