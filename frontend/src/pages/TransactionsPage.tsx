// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useState } from "react";
import { EtherscanPager } from "@/components/EtherscanPager";
import { Copy, ExternalLink, Loader2, RefreshCw, ScrollText, TriangleAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppPageShell } from "@/components/AppPageShell";
import { ProductSectionNav } from "@/components/ProductSectionNav";
import { Button } from "@/components/ui/button";
import {
  fetchTransactions,
  phalconTransactionUrl,
  type TransactionRecord,
  type TransactionStatus,
} from "@/lib/web3-api";
import { chainDisplayName } from "@/lib/chain-display";
import { cn, formatDateTime, formatUsd, shortAddress } from "@/lib/utils";
import type { Page, User } from "@/types/chat";

export function TransactionsPage({
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
  const [transactions, setTransactions] = useState<TransactionRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [counts, setCounts] = useState({ pending: 0, confirmed: 0, failed: 0 });
  const pageSize = 25;
  const totalPages = total > 0 ? Math.ceil(total / pageSize) : 1;

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await fetchTransactions(user, page);
      setTransactions(result.items);
      setTotal(result.total);
      if (result.counts) setCounts(result.counts);
    } catch {
      setError(t("transactions.loadError"));
      setTransactions([]);
    } finally {
      setLoading(false);
    }
  }, [t, user, page]);

  useEffect(() => {
    void load();
  }, [load]);

  const copyHash = async (hash: string) => {
    try {
      await navigator.clipboard.writeText(hash);
      onToast(t("transactions.copiedHash"));
    } catch {
      onToast(t("common.copyFailed"));
    }
  };

  return (
    <AppPageShell user={user} onNavigate={onNavigate} onLogout={onLogout}>
      <section className="mx-auto grid w-full max-w-7xl gap-6 py-8">
        <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
          <div>
            <ProductSectionNav active="transactions" onNavigate={onNavigate} />
            <p className="mt-5 text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              {t("transactions.eyebrow")}
            </p>
            <h1 className="mt-3 font-display text-5xl font-medium leading-tight">{t("transactions.title")}</h1>
            <p className="mt-4 max-w-2xl leading-7 text-muted-foreground">{t("transactions.intro")}</p>
          </div>
          <div className="flex items-start justify-end gap-2">
            <Button variant="outline" onClick={() => void load()} disabled={loading}>
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {t("transactions.refresh")}
            </Button>
          </div>
        </div>

        {error && (
          <div className="flex items-start gap-3 rounded-md border border-destructive/20 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
            <p>{error}</p>
          </div>
        )}

        <div className="grid gap-3 md:grid-cols-3">
          <StatusMetric label={t("transactions.pending")} value={counts.pending} status="pending" />
          <StatusMetric label={t("transactions.confirmed")} value={counts.confirmed} status="confirmed" />
          <StatusMetric label={t("transactions.failed")} value={counts.failed} status="failed" />
        </div>

        <section className="overflow-hidden rounded-md border border-border bg-card shadow-line">
          {loading ? (
            <div className="flex items-center gap-3 p-6 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              {t("transactions.loading")}
            </div>
          ) : transactions.length === 0 ? (
            <div className="p-8 text-center">
              <div className="mx-auto grid h-12 w-12 place-items-center rounded-md bg-accent text-accent-foreground">
                <ScrollText className="h-5 w-5" />
              </div>
              <h2 className="mt-4 text-xl font-semibold">{t("transactions.noTransactionsTitle")}</h2>
              <p className="mx-auto mt-2 max-w-xl leading-7 text-muted-foreground">
                {t("transactions.noTransactionsBody")}
              </p>
            </div>
          ) : (
            <>
              <div className="grid min-w-[76rem] grid-cols-[6rem_minmax(12rem,1fr)_9rem_8rem_9rem_8rem_8rem_9rem] gap-3 border-b border-border px-4 py-3 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground max-xl:hidden">
                <span>{t("transactions.chain")}</span>
                <span>{t("transactions.hash")}</span>
                <span>{t("transactions.wallet")}</span>
                <span>{t("transactions.type")}</span>
                <span>{t("transactions.policy")}</span>
                <span>{t("transactions.submittedAt")}</span>
                <span>{t("transactions.blockTime")}</span>
                <span>{t("transactions.status")}</span>
              </div>
              <div className="divide-y divide-border">
                {transactions.map((tx) => (
                  <TransactionRow key={`${tx.chain}-${tx.hash}`} tx={tx} locale={locale} onCopy={copyHash} />
                ))}
              </div>
            </>
          )}
          <EtherscanPager
            page={page}
            totalPages={totalPages}
            disabled={loading}
            onFirst={() => { setPage(1); }}
            onPrev={() => { setPage((p) => Math.max(1, p - 1)); }}
            onNext={() => { setPage((p) => Math.min(totalPages, p + 1)); }}
            onLast={() => { setPage(totalPages); }}
          />
        </section>
      </section>
    </AppPageShell>
  );
}

function TransactionRow({ tx, locale, onCopy }: { tx: TransactionRecord; locale: string; onCopy: (hash: string) => void }) {
  const { t } = useTranslation();
  const phalconUrl = phalconTransactionUrl(tx);

  return (
    <div className="grid gap-3 px-4 py-4 xl:min-w-[76rem] xl:grid-cols-[6rem_minmax(12rem,1fr)_9rem_8rem_9rem_8rem_8rem_9rem] xl:items-center">
      <div>
        <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.chain")}</p>
        <p className="font-semibold">{tx.chain ? chainDisplayName(tx.chain, t) : t("common.unknown")}</p>
      </div>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.hash")}</p>
        <div className="mt-1 flex min-w-0 items-center gap-2 xl:mt-0">
          <span className="min-w-0 truncate font-mono text-sm">{shortAddress(tx.hash)}</span>
          <button
            type="button"
            className="grid h-8 w-8 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label={t("transactions.copyHash")}
            onClick={() => onCopy(tx.hash)}
          >
            <Copy className="h-4 w-4" />
          </button>
          {phalconUrl && (
            <a
              className="grid h-8 w-8 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              href={phalconUrl}
              target="_blank"
              rel="noreferrer"
              aria-label={t("transactions.openPhalcon")}
            >
              <ExternalLink className="h-4 w-4" />
            </a>
          )}
        </div>
      </div>
      <div>
        <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.wallet")}</p>
        <p className="font-mono text-sm">{tx.walletId || t("common.unknown")}</p>
      </div>
      <div>
        <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.type")}</p>
        <p className="text-sm font-semibold">
          {tx.txType === "bridge_destination" ? "bridge" : (tx.txType || t("common.unknown"))}
        </p>
        {tx.txType === "bridge_destination" ? (
          tx.receivedAmount && (
            <p className="mt-1 font-mono text-xs text-muted-foreground">{tx.receivedAmount}</p>
          )
        ) : (
          tx.amountUsd > 0 && <p className="mt-1 font-mono text-xs text-muted-foreground">{formatUsd(tx.amountUsd, locale)}</p>
        )}
      </div>
      <div>
        <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.policy")}</p>
        <p className="font-mono text-xs">{tx.policyId ? shortAddress(tx.policyId) : t("common.unknown")}</p>
        {tx.policyDecision && <p className="mt-1 text-xs text-muted-foreground">{tx.policyDecision} · {tx.policyUsageStatus || t("common.unknown")}</p>}
      </div>
      {tx.txType === "bridge_destination" ? (
        <div>
          <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.submittedAt")}</p>
          <p className="font-mono text-sm">-</p>
        </div>
      ) : (
        <TimeCell label={t("transactions.submittedAt")} value={tx.submittedAt} />
      )}
      <TimeCell label={t("transactions.blockTime")} value={tx.blockTime} empty={t("transactions.notConfirmed")} />
      <div>
        <p className="text-xs text-muted-foreground xl:hidden">{t("transactions.status")}</p>
        <StatusPill status={tx.status} />
      </div>
    </div>
  );
}

function TimeCell({ label, value, empty }: { label: string; value?: string; empty?: string }) {
  const { i18n } = useTranslation();
  const locale = i18n.language.startsWith("zh") ? "zh-CN" : "en-US";
  return (
    <div>
      <p className="text-xs text-muted-foreground xl:hidden">{label}</p>
      <p className="font-mono text-sm">{value ? formatDateTime(value, locale) : empty ?? ""}</p>
    </div>
  );
}

function StatusMetric({ label, value, status }: { label: string; value: number; status: TransactionStatus }) {
  return (
    <div className="rounded-md border border-border bg-card px-4 py-3 shadow-line">
      <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</p>
      <div className="mt-2 flex items-center justify-between gap-3">
        <p className="font-mono text-2xl font-semibold">{value}</p>
        <StatusDot status={status} />
      </div>
    </div>
  );
}

function StatusPill({ status }: { status: TransactionStatus }) {
  const { t } = useTranslation();
  return (
    <span
      className={cn(
        "inline-flex h-7 items-center gap-2 rounded-full px-2.5 text-xs font-semibold",
        statusClass(status),
      )}
    >
      <StatusDot status={status} />
      {t(`transactions.statuses.${status}`, { defaultValue: status || t("common.unknown") })}
    </span>
  );
}

function StatusDot({ status }: { status: TransactionStatus }) {
  return <span className={cn("h-2 w-2 rounded-full", dotClass(status))} />;
}

function statusClass(status: TransactionStatus) {
  if (status === "confirmed") return "bg-accent text-accent-foreground";
  if (status === "failed") return "bg-destructive/10 text-destructive";
  return "bg-[hsl(33_48%_88%)] text-[hsl(26_38%_28%)]";
}

function dotClass(status: TransactionStatus) {
  if (status === "confirmed") return "bg-[hsl(151_24%_38%)]";
  if (status === "failed") return "bg-destructive";
  return "bg-[hsl(27_48%_43%)]";
}
