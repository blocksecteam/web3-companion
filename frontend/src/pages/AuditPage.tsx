// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useState } from "react";
import { Loader2, RefreshCw, ScrollText, TriangleAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppPageShell } from "@/components/AppPageShell";
import { ProductSectionNav } from "@/components/ProductSectionNav";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EtherscanPager } from "@/components/EtherscanPager";
import { fetchAuditLogs, fetchWallets, type AuditLog, type Wallet } from "@/lib/web3-api";
import { cn, formatTime, shortAddress, shortId } from "@/lib/utils";
import type { Page, User } from "@/types/chat";

export function AuditPage({
  user,
  onNavigate,
  onLogout,
}: {
  user: User;
  onNavigate: (page: Page, sessionId?: string) => void;
  onLogout: () => void;
}) {
  const { t } = useTranslation();
  const [wallets, setWallets] = useState<Wallet[]>([]);
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [walletFilter, setWalletFilter] = useState("all");
  const [severityFilter, setSeverityFilter] = useState("all");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [severityCounts, setSeverityCounts] = useState<Record<string, number>>({});
  const pageSize = 20;
  const totalPages = total > 0 ? Math.ceil(total / pageSize) : 1;

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [nextWallets, result] = await Promise.all([
        fetchWallets(user).catch(() => []),
        fetchAuditLogs(user, walletFilter, page, pageSize, severityFilter),
      ]);
      setWallets(nextWallets);
      setLogs(result.items);
      setTotal(result.total);
      if (result.severityCounts && severityFilter === "all") setSeverityCounts(result.severityCounts);
    } catch {
      setError(t("audit.loadError"));
      setLogs([]);
    } finally {
      setLoading(false);
    }
  }, [t, user, walletFilter, severityFilter, page, pageSize]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <AppPageShell user={user} onNavigate={onNavigate} onLogout={onLogout}>
      <section className="mx-auto grid w-full max-w-7xl gap-6 py-8">
        <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
          <div>
            <ProductSectionNav active="audit" onNavigate={onNavigate} />
            <p className="mt-5 text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              {t("audit.eyebrow")}
            </p>
            <h1 className="mt-3 font-display text-5xl font-medium leading-tight">{t("audit.title")}</h1>
            <p className="mt-4 max-w-2xl leading-7 text-muted-foreground">{t("audit.intro")}</p>
          </div>
          <div className="flex items-start justify-end gap-2">
            <Button variant="outline" onClick={() => void load()} disabled={loading}>
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {t("audit.refresh")}
            </Button>
          </div>
        </div>

        {error && (
          <div className="flex items-start gap-3 rounded-md border border-destructive/20 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
            <p>{error}</p>
          </div>
        )}

        <div className="grid gap-4 lg:grid-cols-[18rem_1fr]">
          <aside className="space-y-4">
            <div className="rounded-md border border-border bg-card p-3 shadow-line">
              <label className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                {t("audit.filterWallet")}
                <div className="mt-2">
                  <Select value={walletFilter} onValueChange={(value) => { setWalletFilter(value); setPage(1); setSeverityCounts({}); }}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">{t("audit.allWallets")}</SelectItem>
                      {wallets.map((wallet) => (
                        <SelectItem key={wallet.walletId} value={wallet.walletId}>
                          {wallet.walletId}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </label>
            </div>
            <div className="rounded-md border border-border bg-card p-3 shadow-line">
              <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                {t("audit.severity")}
              </p>
              <div className="mt-3 space-y-2">
                <button
                  type="button"
                  className={cn("flex w-full items-center justify-between rounded-md px-3 py-2 text-sm transition-colors", severityFilter === "all" ? "bg-primary/10 text-primary" : "bg-background/70 hover:bg-secondary/70")}
                  onClick={() => { setSeverityFilter("all"); setPage(1); }}
                >
                  <span className="font-semibold">{t("common.all")}</span>
                  <span className="font-mono text-xs">{Object.values(severityCounts).reduce((a, b) => a + b, 0)}</span>
                </button>
                {["CRITICAL", "HIGH", "MEDIUM", "INFO"].map((severity) => (
                  <button
                    key={severity}
                    type="button"
                    className={cn("flex w-full items-center justify-between rounded-md px-3 py-2 text-sm transition-colors", severityFilter === severity ? "bg-primary/10 ring-1 ring-primary/30" : "bg-background/70 hover:bg-secondary/70")}
                    onClick={() => { setSeverityFilter(severityFilter === severity ? "all" : severity); setPage(1); }}
                  >
                    <span className={cn("font-semibold", severityTone(severity))}>{t(`audit.severities.${severity.toLowerCase()}`, { defaultValue: severity })}</span>
                    <span className="font-mono text-xs">{severityCounts[severity] ?? 0}</span>
                  </button>
                ))}
              </div>
            </div>
          </aside>

          <section className="rounded-md border border-border bg-card shadow-line">
            {loading ? (
              <div className="flex items-center gap-3 p-6 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                {t("audit.loading")}
              </div>
            ) : logs.length === 0 ? (
              <div className="p-8 text-center">
                <div className="mx-auto grid h-12 w-12 place-items-center rounded-md bg-accent text-accent-foreground">
                  <ScrollText className="h-5 w-5" />
                </div>
                <h2 className="mt-4 text-xl font-semibold">{t("audit.noLogsTitle")}</h2>
                <p className="mx-auto mt-2 max-w-xl leading-7 text-muted-foreground">{t("audit.noLogsBody")}</p>
              </div>
            ) : (
              <div className="divide-y divide-border">
                {logs.map((log) => (
                  <AuditRow key={log.eventId} log={log} />
                ))}
              </div>
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
        </div>
      </section>
    </AppPageShell>
  );
}

function AuditRow({ log }: { log: AuditLog }) {
  const { i18n, t } = useTranslation();
  const locale = i18n.language.startsWith("zh") ? "zh-CN" : "en-US";
  return (
    <details className="group">
      <summary className="grid cursor-pointer list-none gap-3 px-4 py-4 md:grid-cols-[9rem_8rem_minmax(0,1fr)_8rem] md:items-center">
        <div>
          <p className="text-xs text-muted-foreground">{t("audit.time")}</p>
          <p className="mt-1 font-mono text-sm">{formatTime(log.createdAt, locale)}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{t("audit.severity")}</p>
          <p className={cn("mt-1 text-sm font-semibold", severityTone(log.severity))}>{t(`audit.severities.${log.severity.toLowerCase()}`, { defaultValue: log.severity })}</p>
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{log.eventType}</p>
          <p className="mt-1 truncate text-sm text-muted-foreground">{log.detail}</p>
        </div>
        <div className="text-left md:text-right">
          <p className="text-xs text-muted-foreground">{t("audit.wallet")}</p>
          <p className="mt-1 font-mono text-sm">{log.walletId || t("common.unknown")}</p>
        </div>
      </summary>
      <div className="mx-4 mb-4 grid gap-2 rounded-md bg-background/70 p-3 text-sm">
        <InfoLine label={t("audit.event")} value={log.eventType} />
        <InfoLine label={t("audit.detail")} value={log.detail} />
        <InfoLine label={t("audit.wallet")} value={log.walletId || t("common.unknown")} />
        {log.txHash && <InfoLine label={t("audit.txHash")} value={shortAddress(log.txHash)} />}
        <InfoLine label={t("audit.id")} value={shortId(log.eventId)} />
        <pre className="mt-2 max-h-40 overflow-auto rounded-md bg-muted/60 px-3 py-2 font-mono text-[11px] leading-5 text-muted-foreground">
          {typeof log.rawData === "string" ? log.rawData : JSON.stringify(log.rawData, null, 2)}
        </pre>
      </div>
    </details>
  );
}

function InfoLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[8rem_1fr]">
      <span className="font-semibold text-muted-foreground">{label}</span>
      <span className="break-all font-mono text-xs">{value}</span>
    </div>
  );
}

function severityTone(severity: string) {
  if (severity === "CRITICAL") return "text-destructive";
  if (severity === "HIGH") return "text-[hsl(16_58%_41%)]";
  if (severity === "MEDIUM") return "text-[hsl(25_48%_43%)]";
  return "text-[hsl(150_23%_34%)]";
}
