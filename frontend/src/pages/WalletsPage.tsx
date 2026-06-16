// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  BarChart2,
  Check,
  Copy,
  Download,
  Loader2,
  Lock,
  Plus,
  RefreshCw,
  Save,
  ShieldAlert,
  ShieldCheck,
  Star,
  Trash2,
  WalletCards,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppPageShell } from "@/components/AppPageShell";
import { ProductSectionNav } from "@/components/ProductSectionNav";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  confirmWalletDelete,
  confirmWalletExport,
  confirmWalletPolicyBinding,
  createSettingsWallet,
  fetchPasskeyChallenge,
  fetchAllPolicyDefinitions,
  fetchSSMStatus,
  fetchSettingsWallets,
  requestWalletPolicyBinding,
  requestWalletDelete,
  setDefaultWallet,
  requestWalletExport,
  fetchWalletPolicyUsage,
  type PolicyDefinition,
  type PolicyUsage,
  type SettingsWallet,
} from "@/lib/user-api";
import { chainDisplayName } from "@/lib/chain-display";
import { fetchChains, type ChainInfo } from "@/lib/web3-api";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	clearPasskeyAction,
	loadPasskeyAction,
	restoreDecisionForStatus,
} from "@/lib/passkey-action-store";
import { messageFromApiError } from "@/lib/client-utils";
import { formatTime, shortAddress } from "@/lib/utils";
import { shouldWarnBeforeLeavingWalletAction } from "@/lib/client-utils";
import { MAX_NAME_LENGTH } from "@/lib/client-utils";
import { InlineError, WalletPasskeyActionModal } from "./wallets/wallet-ui";
import { messageFrom, policyName, saveWalletPasskeyAction, type WalletAction } from "./wallets/wallet-model";
import type { Page, User } from "@/types/chat";

export function WalletsPage({
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
  const [wallets, setWallets] = useState<SettingsWallet[]>([]);
  const [walletOffset, setWalletOffset] = useState(0);
  const [walletHasMore, setWalletHasMore] = useState(false);
  const [loadingMoreWallets, setLoadingMoreWallets] = useState(false);
  const [walletBusy, setWalletBusy] = useState(false);
  const [busyDefaultId, setBusyDefaultId] = useState("");
  const [walletError, setWalletError] = useState("");
  const [newWalletId, setNewWalletId] = useState("");
  const [definitions, setDefinitions] = useState<PolicyDefinition[]>([]);
  const [bindingDrafts, setBindingDrafts] = useState<Record<string, string>>({});
  const [busyPolicyBindingId, setBusyPolicyBindingId] = useState("");
  const [walletAction, setWalletAction] = useState<WalletAction | null>(null);
  const [policyUsages, setPolicyUsages] = useState<Record<string, PolicyUsage | null | "loading">>({});
  const [supportedChains, setSupportedChains] = useState<ChainInfo[]>([]);
  const [encryptionMode, setEncryptionMode] = useState<string | null>(null);
  const selectedWallet = useMemo(
    () => wallets.find((wallet) => wallet.walletId === walletAction?.walletId),
    [walletAction?.walletId, wallets],
  );

  const loadWallets = useCallback(async () => {
    setWalletError("");
    try {
      const [walletResult, nextDefinitions] = await Promise.all([fetchSettingsWallets(user, 0), fetchAllPolicyDefinitions(user)]);
      setWallets(walletResult.items);
      setWalletHasMore(walletResult.hasMore);
      setWalletOffset(walletResult.items.length);
      setDefinitions(nextDefinitions);
      setBindingDrafts((current) => {
        const next = { ...current };
        for (const wallet of walletResult.items) {
          if (!next[wallet.walletId]) next[wallet.walletId] = wallet.policyDefinitionId ?? "";
        }
        return next;
      });
    } catch (error) {
      setWalletError(messageFrom(error, t("settings.walletLoadError")));
    }
  }, [t, user]);

  const loadMoreWallets = useCallback(async () => {
    setLoadingMoreWallets(true);
    try {
      const result = await fetchSettingsWallets(user, walletOffset);
      setWallets((current) => [...current, ...result.items]);
      setWalletHasMore(result.hasMore);
      setWalletOffset((prev) => prev + result.items.length);
      setBindingDrafts((current) => {
        const next = { ...current };
        for (const wallet of result.items) {
          if (!next[wallet.walletId]) next[wallet.walletId] = wallet.policyDefinitionId ?? "";
        }
        return next;
      });
    } catch {
      onToast(t("common.loadMoreFailed"));
    } finally {
      setLoadingMoreWallets(false);
    }
  }, [user, walletOffset]);

  useEffect(() => {
    void loadWallets();
    fetchChains().then(setSupportedChains).catch(() => {});
    fetchSSMStatus(user).then((s) => setEncryptionMode(s.encryptionMode)).catch(() => {});
  }, [loadWallets]);

  useEffect(() => {
    if (!shouldWarnBeforeLeavingWalletAction(walletAction)) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [walletAction]);

  useEffect(() => {
    let active = true;
    const stored = loadPasskeyAction(user.userId, "wallets");
    if (!stored) {
      return () => {
        active = false;
      };
    }
    fetchPasskeyChallenge(user, stored.challenge.challengeId)
      .then((fresh) => {
        if (!active) return;
        const decision = restoreDecisionForStatus(fresh.status);
        if (decision === "clear_and_restart") {
          clearPasskeyAction(user.userId, "wallets");
          onToast(t("passkeyModal.restoredUnavailable"));
          return;
        }
        const restoredChallenge = {
          ...stored.challenge,
          summary: fresh.summary ?? stored.challenge.summary,
          timeoutAt: fresh.expiresAt ?? stored.challenge.timeoutAt,
        };
	        if (stored.kind === "delete") {
	          setWalletAction({
	            type: "delete",
	            walletId: stored.walletId,
	            confirmation: stored.confirmation,
	            challenge: restoredChallenge,
	            restoredApproved: decision === "auto_confirm",
	          });
	        } else if (stored.kind === "export") {
	          setWalletAction({
	            type: "export",
	            walletId: stored.walletId,
	            challenge: restoredChallenge,
	            restoredApproved: decision === "auto_confirm",
	          });
	        } else if (stored.kind === "bind_policy") {
	          setWalletAction({
	            type: "bind_policy",
	            walletId: stored.walletId,
	            policyDefinitionId: stored.policyDefinitionId,
	            challenge: restoredChallenge,
	            restoredApproved: decision === "auto_confirm",
	          });
	        }
      })
      .catch(() => {
        if (!active) return;
        clearPasskeyAction(user.userId, "wallets");
      });
    return () => {
      active = false;
    };
  }, [onToast, t, user]);

  const createWallet = async () => {
    setWalletBusy(true);
    setWalletError("");
    try {
      await createSettingsWallet(user, newWalletId.trim());
      setWallets([]);
      setWalletOffset(0);
      setWalletHasMore(false);
      setNewWalletId("");
      void loadWallets();
      onToast(t("settings.walletCreatedFunding"));
    } catch (error) {
      setWalletError(messageFrom(error, t("settings.walletCreateError")));
    } finally {
      setWalletBusy(false);
    }
  };

  const startDelete = (wallet: SettingsWallet) => {
    setWalletAction({ type: "delete", walletId: wallet.walletId, confirmation: "" });
  };

  const startSetDefault = async (wallet: SettingsWallet) => {
    setBusyDefaultId(wallet.walletId);
    setWalletError("");
    try {
      await setDefaultWallet(user, wallet.walletId);
      setWallets([]);
      setWalletOffset(0);
      setWalletHasMore(false);
      void loadWallets();
      onToast(t("settings.setDefaultWalletDone"));
    } catch (err) {
      setWalletError(messageFromApiError(err, t("settings.walletLoadError"), {}));
    } finally {
      setBusyDefaultId("");
    }
  };

  const selectPolicyDefinition = (walletId: string, policyDefinitionId: string) => {
    setBindingDrafts((current) => ({ ...current, [walletId]: policyDefinitionId }));
  };

  const requestPolicyBinding = async (wallet: SettingsWallet) => {
    const policyDefinitionId = bindingDrafts[wallet.walletId] ?? wallet.policyDefinitionId ?? "";
    if (!policyDefinitionId || policyDefinitionId === wallet.policyDefinitionId) return;
    setWalletError("");
    setBusyPolicyBindingId(wallet.walletId);
    try {
      const challenge = await requestWalletPolicyBinding(user, wallet.walletId, policyDefinitionId);
      const next: WalletAction = {
        type: "bind_policy",
        walletId: wallet.walletId,
        policyDefinitionId,
        challenge,
        restoredApproved: false,
      };
      setWalletAction(next);
      saveWalletPasskeyAction(user.userId, next);
    } catch (error) {
      setWalletError(messageFrom(error, t("settings.policyBindingError")));
    } finally {
      setBusyPolicyBindingId("");
    }
  };

  const loadPolicyUsage = useCallback(async (walletId: string) => {
    if (!user) return;
    setPolicyUsages(prev => ({ ...prev, [walletId]: "loading" }));
    try {
      const usage = await fetchWalletPolicyUsage(user, walletId);
      setPolicyUsages(prev => ({ ...prev, [walletId]: usage }));
    } catch {
      setPolicyUsages(prev => ({ ...prev, [walletId]: null }));
    }
  }, [user]);

  const startExport = (wallet: SettingsWallet) => {
    setWalletAction({ type: "export", walletId: wallet.walletId });
  };

  const copySecret = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      onToast(t("common.copied"));
    } catch {
      onToast(t("common.copyFailed"));
    }
  };

  const closeWalletAction = useCallback(() => {
    if (shouldWarnBeforeLeavingWalletAction(walletAction)) {
      setWalletAction({ ...walletAction, error: t("settings.privateKeySaveRequired") } as WalletAction);
      return;
    }
    clearPasskeyAction(user.userId, "wallets");
    setWalletAction(null);
  }, [t, user.userId, walletAction]);

  const guardedNavigate = useCallback((page: Page, sessionId?: string) => {
    if (shouldWarnBeforeLeavingWalletAction(walletAction)) {
      setWalletAction({ ...walletAction, error: t("settings.privateKeySaveRequired") } as WalletAction);
      onToast(t("settings.privateKeySaveRequired"));
      return;
    }
    onNavigate(page, sessionId);
  }, [onNavigate, onToast, t, walletAction]);

  const guardedLogout = useCallback(() => {
    if (shouldWarnBeforeLeavingWalletAction(walletAction)) {
      setWalletAction({ ...walletAction, error: t("settings.privateKeySaveRequired") } as WalletAction);
      onToast(t("settings.privateKeySaveRequired"));
      return;
    }
    onLogout();
  }, [onLogout, onToast, t, walletAction]);

  const cancelWalletPasskey = useCallback(() => {
    clearPasskeyAction(user.userId, "wallets");
    setWalletAction(null);
  }, [user.userId]);

  return (
    <AppPageShell user={user} onNavigate={guardedNavigate} onLogout={guardedLogout}>
      <section className="mx-auto grid w-full max-w-7xl gap-6 py-8">
        <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
          <div>
            <ProductSectionNav active="wallets" onNavigate={guardedNavigate} />
            <p className="mt-5 text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">{t("settings.walletSection")}</p>
            <h1 className="mt-3 font-display text-5xl font-medium leading-tight">{t("settings.walletSection")}</h1>
            <p className="mt-4 max-w-2xl leading-7 text-muted-foreground">{t("settings.walletBody")}</p>
            {encryptionMode && (
              <div className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
                <Lock className="h-3 w-3" />
                {t("settings.encryptionMode")}: <span className="font-mono font-medium text-foreground">{encryptionMode === "aws_kms" ? "AWS KMS" : t("settings.encryptionLocal")}</span>
              </div>
            )}
          </div>
          <div className="flex items-start justify-end gap-2">
            <Button variant="outline" onClick={() => void loadWallets()}>
              <RefreshCw className="h-4 w-4" />
              {t("common.refresh")}
            </Button>
          </div>
        </div>

        <section className="rounded-md border border-border bg-card shadow-line">
          <div className="grid gap-4 border-b border-border px-5 py-4 md:grid-cols-[1fr_22rem] md:items-end">
            <div>
              <div className="flex items-center gap-2">
                <WalletCards className="h-4 w-4 text-muted-foreground" />
                <h2 className="text-lg font-semibold">{t("settings.walletSection")}</h2>
              </div>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t("settings.walletBody")}</p>
            </div>
            <div className="flex gap-2">
              <Input
                value={newWalletId}
                onChange={(event) => setNewWalletId(event.target.value)}
                placeholder={t("settings.walletIdPlaceholder")}
                maxLength={MAX_NAME_LENGTH}
              />
              <Button onClick={() => void createWallet()} disabled={walletBusy || !newWalletId.trim()} className="shrink-0">
                {walletBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                {t("settings.createWallet")}
              </Button>
            </div>
          </div>

          {walletError && <div className="px-5 pt-4"><InlineError message={walletError} /></div>}

          <div className="divide-y divide-border">
            {wallets.length === 0 ? (
              <div className="p-8 text-center">
                <div className="mx-auto grid h-12 w-12 place-items-center rounded-md bg-accent text-accent-foreground">
                  <WalletCards className="h-5 w-5" />
                </div>
                <h3 className="mt-4 text-xl font-semibold">{t("settings.noWalletsTitle")}</h3>
                <p className="mx-auto mt-2 max-w-xl text-sm leading-6 text-muted-foreground">{t("settings.noWalletsBody")}</p>
              </div>
            ) : (
              wallets.map((wallet) => (
                <div key={wallet.walletId} className="grid gap-4 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="font-semibold">{wallet.walletId}</p>
                      {wallet.isDefault && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground">
                          <Star className="h-3 w-3" />
                          {t("settings.alreadyDefaultWallet")}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 flex items-center gap-1">
                      <span className="font-mono text-xs text-muted-foreground">{shortAddress(wallet.address)}</span>
                      <button
                        type="button"
                        className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        aria-label={t("settings.copyAddress")}
                        onClick={() => void copySecret(wallet.address)}
                      >
                        <Copy className="h-3 w-3" />
                      </button>
                    </div>
                    {wallet.createdAt && <p className="mt-1 text-xs text-muted-foreground">{t("settings.walletCreatedAt", { time: formatTime(wallet.createdAt, locale) })}</p>}
                    <p className="mt-1 text-xs text-muted-foreground">
                      {t("settings.boundPolicy", { policy: policyName(definitions, wallet.policyDefinitionId) })}
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2 lg:justify-end">
                    <div className="flex min-w-64 gap-2">
                      <Select
                        value={bindingDrafts[wallet.walletId] ?? wallet.policyDefinitionId ?? ""}
                        onValueChange={(value) => selectPolicyDefinition(wallet.walletId, value)}
                        disabled={definitions.length === 0}
                      >
                        <SelectTrigger aria-label={t("settings.policyDefinition")}>
                          <SelectValue placeholder={t("settings.selectPolicy")} />
                        </SelectTrigger>
                        <SelectContent>
                          {definitions.map((definition) => (
                            <SelectItem key={definition.policyDefinitionId} value={definition.policyDefinitionId}>
                              {definition.name}{definition.isDefault ? ` (${t("settings.defaultPolicy")})` : ""}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Button
                        variant="outline"
                        onClick={() => void requestPolicyBinding(wallet)}
                        disabled={Boolean(busyPolicyBindingId) || !bindingDrafts[wallet.walletId] || bindingDrafts[wallet.walletId] === wallet.policyDefinitionId}
                      >
                        {busyPolicyBindingId === wallet.walletId ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                        {t("settings.bindPolicy")}
                      </Button>
                      {wallet.policyInstanceId && (
                        <DropdownMenu onOpenChange={(open) => { if (open) void loadPolicyUsage(wallet.walletId); }}>
                          <DropdownMenuTrigger asChild>
                            <Button variant="outline">
                              <BarChart2 className="h-4 w-4" />
                              {t("settings.policyUsage")}
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="start" side="bottom" className="w-96 min-h-[220px]">
                            <DropdownMenuLabel>{t("settings.policyUsageDailyLabel")}</DropdownMenuLabel>
                            <DropdownMenuSeparator />
                            {policyUsages[wallet.walletId] === "loading" && (
                              <DropdownMenuItem disabled>{t("common.loading")}...</DropdownMenuItem>
                            )}
                            {policyUsages[wallet.walletId] === null && (
                              <DropdownMenuItem disabled>{t("settings.policyUsageLoadError")}</DropdownMenuItem>
                            )}
                            {policyUsages[wallet.walletId] && policyUsages[wallet.walletId] !== "loading" && (() => {
                              const u = policyUsages[wallet.walletId] as PolicyUsage;
                              const fmt = (n: number) => {
                                if (n >= 10_000_000) return `$${n.toExponential(2)}`;
                                return `$${n.toFixed(2)}`;
                              };
                              const fmtFull = (n: number) => `$${n.toFixed(2)}`;
                              const usageColor = (used: number, limit: number) => {
                                if (limit <= 0) return "text-muted-foreground";
                                const ratio = used / limit;
                                if (ratio >= 1) return "text-destructive font-medium";
                                if (ratio >= 0.8) return "text-orange-500";
                                return "text-muted-foreground";
                              };
                              const countColor = (used: number, limit: number) => {
                                if (limit <= 0) return "text-muted-foreground";
                                const ratio = used / limit;
                                if (ratio >= 1) return "text-destructive font-medium";
                                if (ratio >= 0.8) return "text-orange-500";
                                return "text-muted-foreground";
                              };
                              return (
                                <>
                                  <DropdownMenuItem disabled className="flex justify-between">
                                    <span>{t("settings.maxPerTransaction")}</span>
                                    <span className="text-muted-foreground" title={fmtFull(u.maxPerTransactionUsd)}>{t("settings.usageLimitOnly", { value: fmt(u.maxPerTransactionUsd) })}</span>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem disabled className="flex justify-between">
                                    <span>{t("settings.dailyTransferShort")}</span>
                                    <span className={usageColor(u.dailyTransferUsedUsd, u.dailyTransferLimitUsd)} title={t("settings.usageUsedOf", { used: fmtFull(u.dailyTransferUsedUsd), limit: fmtFull(u.dailyTransferLimitUsd) })}>{t("settings.usageUsedOf", { used: fmt(u.dailyTransferUsedUsd), limit: fmt(u.dailyTransferLimitUsd) })}</span>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem disabled className="flex justify-between">
                                    <span>{t("settings.dailyTradeShort")}</span>
                                    <span className={usageColor(u.dailyTradeUsedUsd, u.dailyTradeLimitUsd)} title={t("settings.usageUsedOf", { used: fmtFull(u.dailyTradeUsedUsd), limit: fmtFull(u.dailyTradeLimitUsd) })}>{t("settings.usageUsedOf", { used: fmt(u.dailyTradeUsedUsd), limit: fmt(u.dailyTradeLimitUsd) })}</span>
                                  </DropdownMenuItem>
                                  {u.mode === "auto" && (
                                    <>
                                      <DropdownMenuSeparator />
                                      <DropdownMenuItem disabled className="flex justify-between">
                                        <span>{t("settings.autoSignMaxPerTx")}</span>
                                        <span className="text-muted-foreground" title={fmtFull(u.autoSignMaxPerTxUsd)}>{t("settings.usageLimitOnly", { value: fmt(u.autoSignMaxPerTxUsd) })}</span>
                                      </DropdownMenuItem>
                                      <DropdownMenuItem disabled className="flex justify-between">
                                        <span>{t("settings.autoSignDailyLimit")}</span>
                                        <span className={usageColor(u.autoSignDailyUsedUsd, u.autoSignDailyLimitUsd)} title={t("settings.usageUsedOf", { used: fmtFull(u.autoSignDailyUsedUsd), limit: fmtFull(u.autoSignDailyLimitUsd) })}>{t("settings.usageUsedOf", { used: fmt(u.autoSignDailyUsedUsd), limit: fmt(u.autoSignDailyLimitUsd) })}</span>
                                      </DropdownMenuItem>
                                      <DropdownMenuItem disabled className="flex justify-between">
                                        <span>{t("settings.autoSignCountTodayLabel")}</span>
                                        <span className={countColor(u.autoSignCountToday, u.autoSignMaxCountDay)}>{t("settings.autoSignCountTodayValue", { used: u.autoSignCountToday, limit: u.autoSignMaxCountDay })}</span>
                                      </DropdownMenuItem>
                                    </>
                                  )}
                                </>
                              );
                            })()}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      )}
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="outline">
                            <ShieldCheck className="h-4 w-4" />
                            {t("settings.delegationStatus")}
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start" side="bottom" className="w-64">
                          <DropdownMenuLabel>{t("settings.delegationStatusLabel")}</DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          <div className="grid grid-cols-2 gap-1 p-2">
                            {supportedChains.map((chain) => {
                              const delegation = wallet.chains.find((c) => c.chain === chain.name);
                              const isDelegated = delegation?.delegationStatus === "delegated";
                              return (
                                <div key={chain.name} className="flex items-center gap-1.5 rounded px-2 py-1 text-sm">
                                  {isDelegated
                                    ? <Check className="h-3.5 w-3.5 shrink-0 text-green-500" />
                                    : <X className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
                                  <span>{chainDisplayName(chain.name, t)}</span>
                                </div>
                              );
                            })}
                          </div>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                    <Button variant="outline" onClick={() => void startExport(wallet)}>
                      <Download className="h-4 w-4" />
                      {t("settings.exportWallet")}
                    </Button>
                    <Button variant="destructive" onClick={() => startDelete(wallet)}>
                      <Trash2 className="h-4 w-4" />
                      {t("settings.deleteWallet")}
                    </Button>
                    <Button
                      variant="outline"
                      className="min-w-[7.5rem] justify-center"
                      onClick={() => void startSetDefault(wallet)}
                      disabled={wallet.isDefault || busyDefaultId === wallet.walletId}
                    >
                      {busyDefaultId === wallet.walletId ? <Loader2 className="h-4 w-4 animate-spin" /> : <Star className="h-4 w-4" />}
                      {t(wallet.isDefault ? "settings.alreadyDefaultWallet" : "settings.setDefaultWallet")}
                    </Button>
                  </div>
                </div>
              ))
            )}
          </div>
          {walletHasMore && (
            <div className="flex justify-center py-4">
              <Button variant="outline" onClick={() => void loadMoreWallets()} disabled={loadingMoreWallets}>
                {loadingMoreWallets && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                {t("common.loadMore")}
              </Button>
            </div>
          )}
        </section>

        {walletAction && (
          <WalletPasskeyActionModal
            user={user}
            action={walletAction}
            selectedWallet={selectedWallet}
            definitions={definitions}
            onActionChange={setWalletAction}
            onCopySecret={copySecret}
            onCancel={closeWalletAction}
            onPasskeyCancel={cancelWalletPasskey}
            onChallenge={(action) => saveWalletPasskeyAction(user.userId, action)}
            onCompleted={(action, result) => {
              clearPasskeyAction(user.userId, "wallets");
              if (action.type === "export") {
                const privateKey = typeof result === "object" && result && "privateKey" in result ? String(result.privateKey) : "";
                setWalletAction({ ...action, privateKey, saved: false, restoredApproved: false });
                setWallets([]);
                setWalletOffset(0);
                setWalletHasMore(false);
                void loadWallets();
                onToast(t("settings.walletExported"));
                return;
              }
              if (action.type === "bind_policy") {
                setWallets((current) => current.map((wallet) => (
                  wallet.walletId === action.walletId
                    ? { ...wallet, policyDefinitionId: action.policyDefinitionId }
                    : wallet
                )));
                setBindingDrafts((current) => ({ ...current, [action.walletId]: action.policyDefinitionId }));
                onToast(t("settings.policyBindingSaved"));
              } else {
                setWallets([]);
                setWalletOffset(0);
                setWalletHasMore(false);
                void loadWallets();
                onToast(t("settings.walletDeleted"));
              }
              setWalletAction(null);
            }}
          />
        )}
      </section>
    </AppPageShell>
  );
}
