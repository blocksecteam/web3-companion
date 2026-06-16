// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Copy,
  Loader2,
  RefreshCw,
  TriangleAlert,
  WalletCards,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppPageShell } from "@/components/AppPageShell";
import { EtherscanPager } from "@/components/EtherscanPager";
import { ProductSectionNav } from "@/components/ProductSectionNav";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  fetchChains,
  fetchPortfolioSnapshot,
  fetchWallets,
  summarizePortfolio,
  type ChainInfo,
  type PortfolioSnapshot,
  type Wallet,
} from "@/lib/web3-api";
import { chainDisplayName, chainOptionLabel } from "@/lib/chain-display";
import { mapWithConcurrency } from "@/lib/promise-pool";
import { cn, formatNumber, formatUsd, shortAddress } from "@/lib/utils";
import type { Page, User } from "@/types/chat";

const fallbackChains: ChainInfo[] = [
  { name: "ethereum", chainId: 1, nativeSymbol: "ETH" },
  { name: "base", chainId: 8453, nativeSymbol: "ETH" },
];

export function PortfolioPage({
  user,
  onNavigate,
  onLogout,
  onToast,
}: {
  user: User;
  onNavigate: (page: Page, sessionId?: string) => void;
  onLogout: () => void;
  onToast: (message: string) => void;
}) {
  const { i18n, t } = useTranslation();
  const locale = i18n.language.startsWith("zh") ? "zh-CN" : "en-US";
  const [wallets, setWallets] = useState<Wallet[]>([]);
  const [chains, setChains] = useState<ChainInfo[]>(fallbackChains);
  const [snapshots, setSnapshots] = useState<PortfolioSnapshot[]>([]);
  const [walletFilter, setWalletFilter] = useState("all");
  const [chainFilter, setChainFilter] = useState("all");
  const [loadingWallets, setLoadingWallets] = useState(true);
  const [loadingPortfolio, setLoadingPortfolio] = useState(false);
  const [error, setError] = useState("");
  const [tokenPage, setTokenPage] = useState(1);
  const [walletPage, setWalletPage] = useState(1);
  const tokenPageSize = 25;
  const walletPageSize = 10;
  const selectedWalletMissing = walletFilter !== "all" && !wallets.some((wallet) => wallet.walletId === walletFilter);

  const visibleWallets = useMemo(
    () => (walletFilter === "all" ? wallets : wallets.filter((wallet) => wallet.walletId === walletFilter)),
    [walletFilter, wallets],
  );
  const visibleChains = useMemo(
    () => (chainFilter === "all" ? chains : chains.filter((chain) => chain.name === chainFilter)),
    [chainFilter, chains],
  );
  const successfulSnapshots = snapshots.filter((snapshot) => !snapshot.error);
  const summary = summarizePortfolio(successfulSnapshots);
  const allBalances = successfulSnapshots
    .flatMap((snapshot) =>
      snapshot.balances.map((balance) => ({
        ...balance,
        walletId: snapshot.walletId ?? "",
        chain: snapshot.chain,
      })),
    )
    .sort((a, b) => b.balanceUsd - a.balanceUsd);
  const tokenTotalPages = Math.max(1, Math.ceil(allBalances.length / tokenPageSize));
  const displayBalances = allBalances.slice((tokenPage - 1) * tokenPageSize, tokenPage * tokenPageSize);
  const walletTotalPages = Math.max(1, Math.ceil(wallets.length / walletPageSize));
  const displayWallets = wallets.slice((walletPage - 1) * walletPageSize, walletPage * walletPageSize);

  const loadWallets = useCallback(async () => {
    setLoadingWallets(true);
    setError("");
    try {
      const [nextWallets, nextChains] = await Promise.all([
        fetchWallets(user),
        fetchChains().catch(() => {
          setError(t("portfolio.chainLoadError"));
          return fallbackChains;
        }),
      ]);
      setWallets(nextWallets);
      setWalletPage(1);
      setChains(nextChains.length ? nextChains : fallbackChains);
    } catch {
      setError(t("portfolio.walletLoadError"));
      setWallets([]);
    } finally {
      setLoadingWallets(false);
    }
  }, [t, user]);

  const loadPortfolio = useCallback(async () => {
    if (visibleWallets.length === 0) {
      setSnapshots([]);
      return;
    }
    setLoadingPortfolio(true);
    const reads = visibleWallets.flatMap((wallet) =>
      visibleChains.map((chain) => ({
        wallet,
        chain,
      })),
    );
    const next = await mapWithConcurrency(reads, 6, async ({ wallet, chain }) => {
        try {
          const snapshot = await fetchPortfolioSnapshot(user, chain.name, wallet.address);
          return { ...snapshot, walletId: wallet.walletId };
        } catch (readError) {
          return {
            walletId: wallet.walletId,
            address: wallet.address,
            chain: chain.name,
            totalUsd: 0,
            balances: [],
            error: readError instanceof Error ? readError.message : t("portfolio.localError"),
          };
        }
      });
    setSnapshots(next);
    setTokenPage(1);
    if (next.some((snapshot) => snapshot.error)) {
      setError(t("portfolio.portfolioLoadError"));
    } else {
      setError("");
    }
    setLoadingPortfolio(false);
  }, [t, user, visibleChains, visibleWallets]);

  useEffect(() => {
    void loadWallets();
  }, [loadWallets]);

  useEffect(() => {
    if (selectedWalletMissing) setWalletFilter("all");
  }, [selectedWalletMissing]);

  useEffect(() => {
    if (!loadingWallets) void loadPortfolio();
  }, [loadPortfolio, loadingWallets]);

  const copyAddress = async (address: string) => {
    try {
      await navigator.clipboard.writeText(address);
      onToast(t("portfolio.copiedAddress"));
    } catch {
      onToast(t("common.copyFailed"));
    }
  };

  return (
    <AppPageShell user={user} onNavigate={onNavigate} onLogout={onLogout}>
      <section className="mx-auto grid w-full max-w-7xl gap-6 py-8">
        <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
          <div>
            <ProductSectionNav active="portfolio" onNavigate={onNavigate} />
            <p className="mt-5 text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              {t("portfolio.eyebrow")}
            </p>
            <h1 className="mt-3 font-display text-5xl font-medium leading-tight">{t("portfolio.title")}</h1>
            <p className="mt-4 max-w-2xl leading-7 text-muted-foreground">{t("portfolio.intro")}</p>
          </div>
          <div className="flex items-start justify-end gap-2">
            <Button variant="outline" onClick={() => void loadPortfolio()} disabled={loadingPortfolio || loadingWallets}>
              {loadingPortfolio ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {t("portfolio.refresh")}
            </Button>
          </div>
        </div>

        {error && (
          <div className="flex items-start gap-3 rounded-md border border-destructive/20 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
            <div>
              <p>{error}</p>
              <p className="mt-1 text-destructive/80">
                {t("portfolio.checkProviderKeys")}{" "}
                <button
                  type="button"
                  className="underline hover:text-destructive"
                  onClick={() => onNavigate("settings")}
                >
                  {t("portfolio.goToSettings")}
                </button>
              </p>
            </div>
          </div>
        )}

        <div className="grid gap-3 md:grid-cols-4">
          <Metric label={t("portfolio.totalValue")} value={formatUsd(summary.totalUsd, locale)} strong />
          <Metric label={t("portfolio.activeWallets")} value={String(summary.walletCount || visibleWallets.length)} />
          <Metric label={t("portfolio.activeChains")} value={String(summary.chainCount || visibleChains.length)} />
          <Metric label={t("portfolio.positions")} value={String(summary.positionCount)} />
        </div>

        <div className="grid gap-4 lg:grid-cols-[20rem_1fr]">
          <aside className="space-y-4">
            <FilterPanel
              title={t("portfolio.walletSelector")}
              value={walletFilter}
              options={[{ value: "all", label: t("common.allWallets") }, ...wallets.map((wallet) => ({ value: wallet.walletId, label: wallet.walletId }))]}
              onChange={(v) => { setWalletFilter(v); setTokenPage(1); }}
            />
            <FilterPanel
              title={t("portfolio.chainSelector")}
              value={chainFilter}
              options={[{ value: "all", label: t("portfolio.allChains") }, ...chains.map((chain) => ({ value: chain.name, label: chainOptionLabel(chain, t) }))]}
              onChange={(v) => { setChainFilter(v); setTokenPage(1); }}
            />
            <div className="rounded-md border border-border bg-card p-3 shadow-line">
              <p className="mb-2 text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                {t("common.wallets")}
              </p>
              <div className="space-y-2">
                {loadingWallets ? (
                  <SkeletonLine label={t("portfolio.loadingWallets")} />
                ) : wallets.length === 0 ? (
                  <p className="text-sm leading-6 text-muted-foreground">{t("portfolio.noWalletsBody")}</p>
                ) : (
                  displayWallets.map((wallet) => (
                    <div key={wallet.walletId} className="rounded-md bg-background/70 p-3">
                      <div className="flex items-center justify-between gap-2">
                        <p className="font-medium">{wallet.walletId}</p>
                      </div>
                      <button
                        className="mt-2 inline-flex max-w-full items-center gap-2 rounded-sm text-left font-mono text-xs text-muted-foreground hover:text-foreground"
                        onClick={() => void copyAddress(wallet.address)}
                        aria-label={t("portfolio.copyAddress")}
                      >
                        <span className="truncate">{shortAddress(wallet.address)}</span>
                        <Copy className="h-3 w-3 shrink-0" />
                      </button>
                    </div>
                  ))
                )}
                <EtherscanPager
                  page={walletPage}
                  totalPages={walletTotalPages}
                  disabled={loadingWallets}
                  onFirst={() => setWalletPage(1)}
                  onPrev={() => setWalletPage((p) => Math.max(1, p - 1))}
                  onNext={() => setWalletPage((p) => Math.min(walletTotalPages, p + 1))}
                  onLast={() => setWalletPage(walletTotalPages)}
                />
              </div>
            </div>
          </aside>

          <div className="space-y-4">
            {loadingPortfolio ? (
              <div className="rounded-md border border-border bg-card p-6 shadow-line">
                <SkeletonLine label={t("portfolio.loadingPortfolio")} />
              </div>
            ) : wallets.length === 0 ? (
              <EmptyState title={t("portfolio.noWalletsTitle")} body={t("portfolio.noWalletsBody")} />
            ) : successfulSnapshots.length === 0 || summary.positionCount === 0 ? (
              <EmptyState title={t("portfolio.noBalancesTitle")} body={t("portfolio.noBalancesBody")} />
            ) : (
              <section className="rounded-md border border-border bg-card shadow-line">
                <div className="grid grid-cols-[minmax(8rem,1fr)_8rem_8rem_8rem] gap-3 border-b border-border px-4 py-3 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground max-md:hidden">
                  <span>{t("portfolio.token")}</span>
                  <span>{t("portfolio.balance")}</span>
                  <span>{t("portfolio.price")}</span>
                  <span className="text-right">{t("portfolio.value")}</span>
                </div>
                <div className="divide-y divide-border">
                  {displayBalances.map((balance) => (
                    <div key={`${balance.walletId}-${balance.chain}-${balance.tokenAddress}`} className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(8rem,1fr)_8rem_8rem_8rem] md:items-center">
                      <div className="min-w-0">
                        <p className="font-semibold">{balance.symbol || t("portfolio.unknownTokenSymbol")}</p>
                        <p className="truncate text-xs text-muted-foreground">
                          {balance.name || t("portfolio.unknownTokenName")} · {balance.walletId} · {chainDisplayName(balance.chain, t)}
                        </p>
                      </div>
                      <p className="font-mono text-sm">{formatNumber(balance.balance, locale)}</p>
                      <p className="font-mono text-sm">{formatUsd(balance.price, locale)}</p>
                      <p className="font-mono text-sm font-semibold md:text-right">{formatUsd(balance.balanceUsd, locale)}</p>
                    </div>
                  ))}
                </div>
                <EtherscanPager
                  page={tokenPage}
                  totalPages={tokenTotalPages}
                  disabled={loadingPortfolio}
                  onFirst={() => setTokenPage(1)}
                  onPrev={() => setTokenPage((p) => Math.max(1, p - 1))}
                  onNext={() => setTokenPage((p) => Math.min(tokenTotalPages, p + 1))}
                  onLast={() => setTokenPage(tokenTotalPages)}
                />
              </section>
            )}
          </div>
        </div>
      </section>
    </AppPageShell>
  );
}

function Metric({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className="rounded-md border border-border bg-card px-4 py-3 shadow-line">
      <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</p>
      <p className={cn("mt-2 font-mono text-2xl", strong && "font-semibold")}>{value}</p>
    </div>
  );
}

function FilterPanel({
  title,
  value,
  options,
  onChange,
}: {
  title: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="rounded-md border border-border bg-card p-3 shadow-line">
      <label className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
        {title}
        <div className="mt-2">
          <Select value={value} onValueChange={onChange}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </label>
    </div>
  );
}

function SkeletonLine({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-4 w-4 animate-spin" />
      {label}
    </div>
  );
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-md border border-border bg-card p-8 text-center shadow-line">
      <div className="mx-auto grid h-12 w-12 place-items-center rounded-md bg-accent text-accent-foreground">
        <WalletCards className="h-5 w-5" />
      </div>
      <h2 className="mt-4 text-xl font-semibold">{title}</h2>
      <p className="mx-auto mt-2 max-w-xl leading-7 text-muted-foreground">{body}</p>
    </div>
  );
}
