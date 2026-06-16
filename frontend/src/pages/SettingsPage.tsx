// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useState } from "react";
import { BadgeCheck, KeyRound, Loader2, Network, PlugZap, RefreshCw, RotateCcw, ShieldAlert, ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppPageShell } from "@/components/AppPageShell";
import { ProductSectionNav } from "@/components/ProductSectionNav";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  fetchLLMProvider,
  fetchRPCSettings,
  fetchProviders,
  saveLLMProvider,
  saveRPCSettings,
  saveProviders,
  verifyLLMProvider,
  verifyPortalsKey,
  verifyMetaSleuthKey,
  verifyRPCSettings,
  type AddressLabelProviderKind,
  type LLMProviderKind,
  type LLMProviderSettings,
  type ProvidersSettings,
  type RPCSettings,
} from "@/lib/user-api";
import { chainDisplayName } from "@/lib/chain-display";
import { formatTime } from "@/lib/utils";
import { MAX_API_KEY_LENGTH } from "@/lib/client-utils";
import type { Page, User } from "@/types/chat";

type RPCDraft = Record<string, string>;

export function SettingsPage({
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
  const [provider, setProvider] = useState<LLMProviderSettings | null>(null);
  const [providerChoice, setProviderChoice] = useState<LLMProviderKind>("deepseek");
  const [baseUrl, setBaseUrl] = useState("");
  const [modelName, setModelName] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [providerBusy, setProviderBusy] = useState(false);
  const [providerError, setProviderError] = useState("");
  const [rpc, setRPC] = useState<RPCSettings>({ chains: [] });
  const [rpcDraft, setRPCDraft] = useState<RPCDraft>({});
  const [rpcDirty, setRPCDirty] = useState<Record<string, boolean>>({});
  const [rpcBusy, setRPCBusy] = useState(false);
  const [rpcError, setRPCError] = useState("");
  const [providers, setProviders] = useState<ProvidersSettings | null>(null);
  const [portalsKey, setPortalsKey] = useState("");
  const [metaSleuthKey, setMetaSleuthKey] = useState("");
  const [providersBusy, setProvidersBusy] = useState(false);
  const [providersError, setProvidersError] = useState("");
  const [portalsVerified, setPortalsVerified] = useState<boolean | null>(null);
  const [msVerified, setMsVerified] = useState<boolean | null>(null);
  const [addressLabelChoice, setAddressLabelChoice] = useState<AddressLabelProviderKind>("disabled");

  const loadProvider = useCallback(async () => {
    setProviderError("");
    try {
      const next = await fetchLLMProvider(user);
      setProvider(next);
      setProviderChoice(next.provider);
      setBaseUrl(next.baseUrl);
      setModelName(next.modelName);
    } catch (error) {
      setProviderError(messageFrom(error, t("settings.providerLoadError")));
    }
  }, [t, user]);

  const loadRPC = useCallback(async () => {
    setRPCError("");
    try {
      const next = await fetchRPCSettings(user);
      setRPC(next);
      setRPCDraft(Object.fromEntries(next.chains.map((chain) => [chain.name, chain.url ?? ""])));
      setRPCDirty({});
    } catch (error) {
      setRPCError(messageFrom(error, t("settings.rpcLoadError")));
    }
  }, [t, user]);

  const loadProvidersData = useCallback(async () => {
    setProvidersError("");
    try {
      const next = await fetchProviders(user);
      setProviders(next);
      setAddressLabelChoice(next.addressLabelProvider);
    } catch (error) {
      setProvidersError(messageFrom(error, t("settings.providersLoadError")));
    }
  }, [t, user]);

  useEffect(() => {
    void loadProvider();
    void loadRPC();
    void loadProvidersData();
  }, [loadProvider, loadRPC, loadProvidersData]);

  const saveProviderChoice = async () => {
    if (!baseUrl.trim() || !modelName.trim()) {
      setProviderError(t("settings.modelFieldsRequired"));
      return;
    }
    if (!apiKey.trim() && !provider?.apiKeyConfigured) {
      setProviderError(t("settings.apiKeyRequired"));
      return;
    }
    setProviderBusy(true);
    setProviderError("");
    try {
      const next = await saveLLMProvider(user, { provider: providerChoice, baseUrl, modelName, apiKey });
      setProvider(next);
      setProviderChoice(next.provider);
      setBaseUrl(next.baseUrl);
      setModelName(next.modelName);
      setApiKey("");
      onToast(next.restarted ? t("settings.providerSavedRestarted") : t("settings.providerSaved"));
    } catch (error) {
      setProviderError(messageFrom(error, t("settings.providerSaveError")));
    } finally {
      setProviderBusy(false);
    }
  };

  const verifyProvider = async () => {
    if (!baseUrl.trim() || !modelName.trim()) {
      setProviderError(t("settings.modelFieldsRequired"));
      return;
    }
    if (!apiKey.trim() && !provider?.apiKeyConfigured) {
      setProviderError(t("settings.apiKeyRequired"));
      return;
    }
    setProviderBusy(true);
    setProviderError("");
    try {
      const next = await verifyLLMProvider(user, { provider: providerChoice, baseUrl, modelName, apiKey });
      setProvider(next);
      setProviderChoice(next.provider);
      setBaseUrl(next.baseUrl);
      setModelName(next.modelName);
      setApiKey("");
      onToast(t("settings.providerVerified"));
    } catch (error) {
      setProviderError(messageFrom(error, t("settings.providerVerifyError")));
    } finally {
      setProviderBusy(false);
    }
  };

  const saveRPC = async () => {
    const payload = rpcPayloadFromDraft(rpc, rpcDraft, rpcDirty);
    setRPCBusy(true);
    setRPCError("");
    try {
      const next = await saveRPCSettings(user, payload);
      setRPC(next);
      setRPCDraft(Object.fromEntries(next.chains.map((chain) => [chain.name, chain.url ?? ""])));
      setRPCDirty({});
      onToast(t("settings.rpcSaved"));
    } catch (error) {
      setRPCError(messageFrom(error, t("settings.rpcSaveError")));
    } finally {
      setRPCBusy(false);
    }
  };

  const verifyRPC = async () => {
    const payload = rpcPayloadFromDraft(rpc, rpcDraft, rpcDirty);
    setRPCBusy(true);
    setRPCError("");
    try {
      const next = await verifyRPCSettings(user, payload);
      setRPC(next);
      setRPCDraft(Object.fromEntries(next.chains.map((chain) => [chain.name, chain.url ?? ""])));
      setRPCDirty({});
      onToast(t("settings.rpcVerified"));
    } catch (error) {
      setRPCError(messageFrom(error, t("settings.rpcVerifyError")));
    } finally {
      setRPCBusy(false);
    }
  };

  const verifyPortals = async () => {
    setProvidersBusy(true);
    setProvidersError("");
    try {
      await verifyPortalsKey(user, portalsKey.trim() || undefined);
      setPortalsVerified(true);
    } catch (error) {
      setPortalsVerified(false);
      setProvidersError(messageFrom(error, t("settings.portalsVerifyError")));
    } finally {
      setProvidersBusy(false);
    }
  };

  const verifyMS = async () => {
    setProvidersBusy(true);
    setProvidersError("");
    try {
      await verifyMetaSleuthKey(user, metaSleuthKey.trim() || undefined);
      setMsVerified(true);
    } catch (error) {
      setMsVerified(false);
      setProvidersError(messageFrom(error, t("settings.msVerifyError")));
    } finally {
      setProvidersBusy(false);
    }
  };

  const saveProvidersData = async (toastKey = "settings.providersSaved") => {
    setProvidersBusy(true);
    setProvidersError("");
    try {
      const next = await saveProviders(user, {
        portalsApiKey: portalsKey.trim() || undefined,
        metaSleuthApiKey: metaSleuthKey.trim() || undefined,
        addressLabelProvider: addressLabelChoice,
      });
      setProviders(next);
      setPortalsKey("");
      setMetaSleuthKey("");
      onToast(t(toastKey));
    } catch (error) {
      setProvidersError(messageFrom(error, t("settings.providersSaveError")));
    } finally {
      setProvidersBusy(false);
    }
  };

  return (
    <AppPageShell user={user} onNavigate={onNavigate} onLogout={onLogout}>
      <section className="mx-auto grid w-full max-w-7xl gap-6 py-8">
        <ProductSectionNav active="settings" onNavigate={onNavigate} />
        <div className="grid gap-6 lg:grid-cols-[1fr_auto]">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">{t("settings.eyebrow")}</p>
            <h1 className="mt-3 font-display text-5xl font-medium leading-tight">{t("settings.title")}</h1>
            <p className="mt-4 max-w-2xl leading-7 text-muted-foreground">{t("settings.intro")}</p>
          </div>
          <div className="flex items-start justify-end gap-2">
            <Button variant="outline" onClick={() => { void loadProvider(); void loadRPC(); }}>
              <RefreshCw className="h-4 w-4" />
              {t("common.refresh")}
            </Button>
          </div>
        </div>

        <section id="settings-security-review" className="rounded-md border border-primary/20 bg-card shadow-line">
          <div className="space-y-5 p-5 md:p-6">
            <div className="flex items-start gap-3">
              <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
                <ShieldCheck className="h-4 w-4" />
              </div>
              <div>
                <p className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">{t("settings.securityReviewEyebrow")}</p>
                <h2 className="mt-1 text-lg font-semibold">{t("settings.securityReviewTitle")}</h2>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">{t("settings.securityReviewBody")}</p>
              </div>
            </div>

            <div className="space-y-3">
              <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{t("settings.addressLabelTitle")}</p>
              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="radio" name="addressLabel" value="disabled" checked={addressLabelChoice === "disabled"} onChange={() => setAddressLabelChoice("disabled")} />
                  {t("settings.labelDisabled")}
                </label>
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="radio" name="addressLabel" value="metasleuth" checked={addressLabelChoice === "metasleuth"} onChange={() => setAddressLabelChoice("metasleuth")} />
                  {t("settings.labelMetaSleuth")}
                  {addressLabelChoice === "metasleuth" && !providers?.metaSleuthApiKeyConfigured && (
                    <span className="text-xs text-destructive">{t("settings.keyNotConfigured")}</span>
                  )}
                </label>
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="radio" name="addressLabel" value="x402" checked={addressLabelChoice === "x402"} onChange={() => setAddressLabelChoice("x402")} />
                  {t("settings.labelX402")}
                </label>
              </div>

              {addressLabelChoice === "metasleuth" && (
                <div className="space-y-2 rounded-md border border-border bg-background/50 p-3">
                  <Label className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    {t("settings.metaSleuthKeyLabel")}
                  </Label>
                  <Input
                    type="password"
                    value={metaSleuthKey}
                    placeholder={providers?.metaSleuthApiKeyConfigured ? providers.metaSleuthApiKeyHint ?? "••••••••" : t("settings.metaSleuthKeyPlaceholder")}
                    onChange={(e) => setMetaSleuthKey(e.target.value)}
                    maxLength={MAX_API_KEY_LENGTH}
                  />
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={providersBusy || (!metaSleuthKey.trim() && !providers?.metaSleuthApiKeyConfigured)}
                      onClick={() => void verifyMS()}
                    >
                      {providersBusy ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : msVerified ? (
                        <BadgeCheck className="h-3.5 w-3.5 text-green-600" />
                      ) : (
                        <PlugZap className="h-3.5 w-3.5" />
                      )}
                      {t("settings.verifyMetaSleuth")}
                    </Button>
                    <span className="text-xs text-muted-foreground">
                      {providers?.metaSleuthApiKeyConfigured ? t("settings.keyConfigured") : t("settings.keyNotConfigured")}
                    </span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    <a href="https://docs.metasleuth.io/blocksec-aml-api/introduction" target="_blank" rel="noopener noreferrer" className="underline hover:text-foreground">
                      {t("settings.metaSleuthDoc")}
                    </a>
                  </p>
                </div>
              )}

              <Button size="sm" onClick={() => void saveProvidersData("settings.securityReviewSaved")} disabled={providersBusy}>
                {providersBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
                {t("settings.saveSecurityReview")}
              </Button>
            </div>
          </div>
        </section>

        <section id="settings-provider" className="rounded-md border border-border bg-card shadow-line">
          <div className="border-b border-border px-5 py-4">
            <div className="flex items-center gap-2">
              <PlugZap className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-lg font-semibold">{t("settings.providerSection")}</h2>
            </div>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t("settings.providerBody")}</p>
          </div>

          <div className="space-y-5 p-5">
            <div className="flex flex-wrap items-center gap-4 rounded-md bg-background/70 px-4 py-3">
              <div className="flex items-center gap-3">
                <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{t("settings.currentProvider")}</p>
                <p className="text-lg font-semibold">{provider ? t(`settings.providers.${provider.provider}`) : t("common.loading")}</p>
              </div>
              {provider?.apiKeyHint && (
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <KeyRound className="h-3.5 w-3.5" />
                  {t("settings.keyHint", { hint: provider.apiKeyHint })}
                </p>
              )}
              {provider?.verifiedAt && (
                <p className="text-xs text-muted-foreground">{t("settings.verifiedAt", { time: formatTime(provider.verifiedAt, locale) })}</p>
              )}
              {provider && !provider.apiKeyConfigured && (
                <div className="flex items-center gap-1.5 rounded-md border border-destructive/20 bg-destructive/10 px-2.5 py-1 text-xs text-destructive">
                  <ShieldAlert className="h-3.5 w-3.5 shrink-0" />
                  <p>{t("settings.apiKeyMissing")}</p>
                </div>
              )}
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                {t("settings.providerSource")}
                <div className="mt-2">
                  <Select value={providerChoice} onValueChange={(value) => setProviderChoice(value as LLMProviderKind)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="deepseek">{t("settings.providers.deepseek")}</SelectItem>
                      <SelectItem value="openrouter">{t("settings.providers.openrouter")}</SelectItem>
                      <SelectItem value="custom">{t("settings.providers.custom")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </label>
              <TextField label={t("settings.modelName")} value={modelName} onChange={setModelName} placeholder={t("settings.modelNamePlaceholder")} />
              <TextField label={t("settings.baseUrl")} value={baseUrl} onChange={setBaseUrl} placeholder={t("settings.baseUrlPlaceholder")} className="md:col-span-2" />
              <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground md:col-span-2">
                {t("settings.apiKey")}
                <Input
                  value={apiKey}
                  onChange={(event) => setApiKey(event.target.value)}
                  type="password"
                  className="mt-2"
                  placeholder={provider?.apiKeyHint ? t("settings.apiKeySavedPlaceholder", { hint: provider.apiKeyHint }) : t("settings.apiKeyPlaceholder")}
                  maxLength={MAX_API_KEY_LENGTH}
                />
              </label>
              <div className="md:col-span-2">
                {providerError && <InlineError message={providerError} />}
                <div className="mt-4 flex flex-wrap gap-2">
                  <Button onClick={() => void saveProviderChoice()} disabled={providerBusy}>
                    {providerBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
                    {t("settings.saveProvider")}
                  </Button>
                  <Button variant="outline" onClick={() => void verifyProvider()} disabled={providerBusy}>
                    <BadgeCheck className="h-4 w-4" />
                    {t("settings.verifyProvider")}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section id="settings-rpc" className="rounded-md border border-border bg-card shadow-line">
          <div className="border-b border-border px-5 py-4">
            <div className="flex items-center gap-2">
              <Network className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-lg font-semibold">{t("settings.rpcSection")}</h2>
            </div>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t("settings.rpcBody")}</p>
          </div>
          <div className="grid gap-4 p-5">
            {rpc.chains.map((chain) => (
              <div key={chain.name} className="grid gap-3 rounded-md border border-border bg-background/60 p-4 md:grid-cols-[10rem_1fr_12rem] md:items-center">
                <div>
                  <p className="font-semibold">{chainDisplayName(chain.name, t)}</p>
                  <p className="text-xs text-muted-foreground">{chain.nativeSymbol} · {chain.chainId}</p>
                </div>
                <Input
                  value={rpcDraft[chain.name] ?? ""}
                  placeholder={chain.rpcUrlHint || t("settings.rpcUrlPlaceholder")}
                  onChange={(event) => {
                    setRPCDraft((current) => ({ ...current, [chain.name]: event.target.value }));
                    setRPCDirty((current) => ({ ...current, [chain.name]: true }));
                  }}
                />
                <p className="text-xs text-muted-foreground">
                  {chain.verifiedAt ? t("settings.verifiedAt", { time: formatTime(chain.verifiedAt, locale) }) : t("settings.notVerified")}
                </p>
              </div>
            ))}
            {rpcError && <InlineError message={rpcError} />}
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => void saveRPC()} disabled={rpcBusy || rpc.chains.length === 0}>
                {rpcBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
                {t("settings.saveRPC")}
              </Button>
              <Button variant="outline" onClick={() => void verifyRPC()} disabled={rpcBusy || rpc.chains.length === 0}>
                <BadgeCheck className="h-4 w-4" />
                {t("settings.verifyRPC")}
              </Button>
            </div>
          </div>
        </section>

        <section id="settings-providers" className="rounded-md border border-border bg-card shadow-line">
          <div className="space-y-5 p-5 md:p-6">
            <div className="flex items-start gap-3">
              <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-accent text-accent-foreground">
                <KeyRound className="h-4 w-4" />
              </div>
              <div>
                <h2 className="text-lg font-semibold">{t("settings.providersTitle")}</h2>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">{t("settings.providersBody")}</p>
              </div>
            </div>

            <div className="space-y-4">
              <div className="space-y-2">
                <Label className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                  {t("settings.portalsKeyLabel")}
                </Label>
                <Input
                  type="password"
                  value={portalsKey}
                  placeholder={providers?.portalsApiKeyConfigured ? providers.portalsApiKeyHint ?? "••••••••" : t("settings.portalsKeyPlaceholder")}
                  onChange={(e) => setPortalsKey(e.target.value)}
                  maxLength={MAX_API_KEY_LENGTH}
                />
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={providersBusy || (!portalsKey.trim() && !providers?.portalsApiKeyConfigured)}
                    onClick={() => void verifyPortals()}
                  >
                    {providersBusy ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : portalsVerified ? (
                      <BadgeCheck className="h-3.5 w-3.5 text-green-600" />
                    ) : (
                      <PlugZap className="h-3.5 w-3.5" />
                    )}
                    {t("settings.verifyPortals")}
                  </Button>
                  <span className="text-xs text-muted-foreground">
                    {providers?.portalsApiKeyConfigured ? t("settings.keyConfigured") : t("settings.keyNotConfigured")}
                  </span>
                </div>
                <p className="text-xs text-muted-foreground">
                  <a
                    href="https://build.portals.fi/"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline hover:text-foreground"
                  >
                    {t("settings.portalsDoc")}
                  </a>
                </p>
              </div>
            </div>

            {providersError && <InlineError message={providersError} />}
            <Button onClick={() => void saveProvidersData()} disabled={providersBusy || !portalsKey.trim()}>
              {providersBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              {t("settings.saveProviders")}
            </Button>
          </div>
        </section>
      </section>
    </AppPageShell>
  );
}

function TextField({
  label,
  value,
  onChange,
  placeholder,
  className = "",
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  className?: string;
}) {
  return (
    <label className={`block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground ${className}`}>
      {label}
      <Input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="mt-2"
        placeholder={placeholder}
      />
    </label>
  );
}

function rpcPayloadFromDraft(rpc: RPCSettings, draft: RPCDraft, dirty: Record<string, boolean>) {
  return rpc.chains
    .filter((chain) => dirty[chain.name])
    .map((chain) => ({ name: chain.name, url: draft[chain.name] ?? "" }));
}

function InlineError({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
      <p>{message}</p>
    </div>
  );
}

function messageFrom(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}
