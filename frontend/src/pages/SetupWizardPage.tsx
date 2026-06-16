// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useState } from "react";
import type React from "react";
import { useTranslation } from "react-i18next";
import {
  BadgeCheck,
  Check,
  ChevronRight,
  ChevronLeft,
  Copy,
  Download,
  Fingerprint,
  KeyRound,
  Loader2,
  MailCheck,
  Network,
  PlugZap,
  Rocket,
  ShieldAlert,
  ShieldCheck,
} from "lucide-react";
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
import { LanguageToggle } from "@/components/LanguageToggle";
import {
  fetchSetupSecrets,
  saveSetupRPC,
  saveSetupLLM,
  saveSetupProviders,
  verifySetupRPC,
  verifySetupLLM,
  completeSetup,
  type SetupChain,
  type SetupSecrets,
} from "@/lib/setup-api";
import {
  registerEmail,
  setupPasskey,
  verifyPasskey,
  claimPasskeyResult,
  loginOptions,
  loginVerify,
  userFromRegistration,
  userFromLogin,
} from "@/lib/auth-client";
import { createPasskeyCredential, getPasskeyAssertion } from "@/lib/passkeys";
import { setCurrentUser } from "@/lib/current-user";
import type { User } from "@/types/chat";
import logoUrl from "@/assets/logo.png";

const STEPS = ["welcome", "chains", "llm", "providers", "account", "complete"] as const;
type Step = (typeof STEPS)[number];

export function SetupWizardPage({
  chains,
  onComplete,
}: {
  chains: SetupChain[];
  onComplete: () => void;
}) {
  const { t } = useTranslation();
  const [step, setStep] = useState<Step>("welcome");
  const stepIndex = STEPS.indexOf(step);

  const [secrets, setSecrets] = useState<SetupSecrets | null>(null);
  const [secretsSaved, setSecretsSaved] = useState(false);

  const defaultRPCs: Record<string, string> = {
    ethereum: "https://eth.rpc.blocksec.com",
    bsc: "https://bsc.rpc.blocksec.com",
    base: "https://developer-access-mainnet.base.org",
    polygon: "https://rpc-mainnet.matic.quiknode.pro",
  };

  const [enabledChains, setEnabledChains] = useState<Record<string, boolean>>(() => {
    const m: Record<string, boolean> = {};
    for (const c of chains) m[c.name] = true;
    return m;
  });
  const [rpcUrls, setRpcUrls] = useState<Record<string, string>>(() => {
    const m: Record<string, string> = {};
    for (const c of chains) m[c.name] = defaultRPCs[c.name] ?? "";
    return m;
  });
  const [rpcVerified, setRpcVerified] = useState<Record<string, boolean>>({});
  const [rpcBusy, setRpcBusy] = useState(false);

  const [llmProvider, setLlmProvider] = useState("deepseek");
  const [llmBaseUrl, setLlmBaseUrl] = useState("https://api.deepseek.com/anthropic");
  const [llmModel, setLlmModel] = useState("deepseek-v4-pro");
  const [llmApiKey, setLlmApiKey] = useState("");
  const [llmVerified, setLlmVerified] = useState(false);
  const [llmBusy, setLlmBusy] = useState(false);

  const [portalsKey, setPortalsKey] = useState("");
  const [metaSleuthKey, setMetaSleuthKey] = useState("");
  const [addressLabelChoice, setAddressLabelChoice] = useState<"disabled" | "metasleuth" | "x402">("disabled");
  const [providersBusy, setProvidersBusy] = useState(false);

  const [email, setEmail] = useState("");
  const [accountUser, setAccountUser] = useState<User | null>(null);
  const [authBusy, setAuthBusy] = useState(false);
  const [authPhase, setAuthPhase] = useState<"email" | "registering" | "confirming" | "done">("email");

  const [error, setError] = useState("");
  const [completeBusy, setCompleteBusy] = useState(false);

  const loadSecrets = useCallback(async () => {
    try {
      setSecrets(await fetchSetupSecrets());
    } catch (e) {
      setError(messageFrom(e, t("setup.secretsLoadError")));
    }
  }, [t]);

  useEffect(() => {
    if (step === "welcome" && !secrets) void loadSecrets();
  }, [step, secrets, loadSecrets]);

  const goNext = () => {
    const idx = STEPS.indexOf(step);
    if (idx < STEPS.length - 1) {
      setError("");
      setStep(STEPS[idx + 1]);
    }
  };
  const goBack = () => {
    const idx = STEPS.indexOf(step);
    if (idx > 0) {
      setError("");
      setStep(STEPS[idx - 1]);
    }
  };

  const enabledChainCount = Object.values(enabledChains).filter(Boolean).length;

  const handleVerifyRPC = async (chainName: string) => {
    setError("");
    setRpcBusy(true);
    try {
      await verifySetupRPC({ [chainName]: { url: rpcUrls[chainName] } });
      setRpcVerified((prev) => ({ ...prev, [chainName]: true }));
    } catch (e) {
      setRpcVerified((prev) => ({ ...prev, [chainName]: false }));
      setError(messageFrom(e, t("setup.rpcFailed")));
    } finally {
      setRpcBusy(false);
    }
  };

  const handleSaveRPC = async () => {
    setError("");
    setRpcBusy(true);
    try {
      const chainsToSave: Record<string, { url: string }> = {};
      for (const c of chains) {
        if (enabledChains[c.name] && rpcUrls[c.name].trim()) {
          chainsToSave[c.name] = { url: rpcUrls[c.name].trim() };
        }
      }
      if (Object.keys(chainsToSave).length === 0) {
        setError(t("setup.atLeastOneChain"));
        return;
      }
      await saveSetupRPC(chainsToSave);
      goNext();
    } catch (e) {
      setError(messageFrom(e, t("setup.rpcSaveError")));
    } finally {
      setRpcBusy(false);
    }
  };

  const handleVerifyLLM = async () => {
    setError("");
    setLlmBusy(true);
    try {
      await verifySetupLLM({
        provider: llmProvider,
        baseUrl: llmBaseUrl,
        modelName: llmModel,
        apiKey: llmApiKey,
      });
      setLlmVerified(true);
    } catch (e) {
      setLlmVerified(false);
      setError(messageFrom(e, t("setup.llmVerifyFailed")));
    } finally {
      setLlmBusy(false);
    }
  };

  const handleSaveLLM = async () => {
    setError("");
    setLlmBusy(true);
    try {
      await saveSetupLLM({
        provider: llmProvider,
        baseUrl: llmBaseUrl,
        modelName: llmModel,
        apiKey: llmApiKey,
      });
      goNext();
    } catch (e) {
      setError(messageFrom(e, t("setup.llmSaveError")));
    } finally {
      setLlmBusy(false);
    }
  };

  const handleSaveProviders = async () => {
    setError("");
    setProvidersBusy(true);
    try {
      await saveSetupProviders({
        portalsApiKey: portalsKey.trim(),
        metaSleuthApiKey: metaSleuthKey.trim(),
        addressLabelProvider: addressLabelChoice,
      });
      goNext();
    } catch (e) {
      setError(messageFrom(e, t("setup.providersSaveError")));
    } finally {
      setProvidersBusy(false);
    }
  };

  const handleCreateAccount = async (e: React.FormEvent) => {
    e.preventDefault();
    const normalizedEmail = email.trim().toLowerCase();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)) {
      setError(t("setup.validEmailRequired"));
      return;
    }
    setError("");
    setAuthBusy(true);
    setAuthPhase("registering");
    try {
      const reg = await registerEmail(normalizedEmail);
      const pk = await setupPasskey(reg.temp_token);
      const credential = await createPasskeyCredential(pk.challenge_id, pk.public_key);
      await verifyPasskey(credential);
      const claimed = await claimPasskeyResult({
        temp_token: reg.temp_token,
        challenge_id: pk.challenge_id,
      });
      const pendingUser = userFromRegistration(normalizedEmail, claimed);

      setAuthPhase("confirming");
      const opts = await loginOptions(normalizedEmail);
      const assertion = await getPasskeyAssertion(opts.challenge_id, opts.public_key);
      const loginResp = await loginVerify(assertion);
      const user = userFromLogin(loginResp);
      setCurrentUser(user);
      setAccountUser(user);
      setAuthPhase("done");
    } catch (err) {
      setError(messageFrom(err, t("setup.accountCreateFailed")));
      setAuthPhase("email");
    } finally {
      setAuthBusy(false);
    }
  };

  const handleComplete = async () => {
    setError("");
    setCompleteBusy(true);
    try {
      await completeSetup();
      onComplete();
    } catch (e) {
      setError(messageFrom(e, t("setup.completeFailed")));
    } finally {
      setCompleteBusy(false);
    }
  };

  const stepLabels: Record<Step, string> = {
    welcome: t("setup.step1"),
    chains: t("setup.step2"),
    llm: t("setup.step3"),
    providers: t("setup.step4"),
    account: t("setup.step5"),
    complete: t("setup.step6"),
  };

  return (
    <div className="paper-grain min-h-screen text-foreground">
      <div className="relative z-10 min-h-screen">
        <header className="flex items-center justify-between border-b border-border px-6 py-4">
          <div className="flex items-center gap-2">
            <img src={logoUrl} alt="Web3 Companion" className="h-8" />
          </div>
          <LanguageToggle compact />
        </header>

        <div className="mx-auto max-w-2xl px-6 py-8">
          {/* Step indicator */}
          <nav className="mb-8 flex items-center justify-center gap-1" aria-label={t("setup.stepsNavLabel")}>
            {STEPS.map((s, i) => (
              <div key={s} className="flex items-center gap-1">
                <div
                  className={`flex h-8 items-center gap-1.5 rounded-full px-3 text-sm font-medium transition-colors ${
                    i < stepIndex
                      ? "bg-primary/10 text-primary"
                      : i === stepIndex
                        ? "bg-primary text-primary-foreground"
                        : "bg-muted text-muted-foreground"
                  }`}
                >
                  {i < stepIndex ? (
                    <Check className="h-3.5 w-3.5" />
                  ) : (
                    <span className="text-xs">{i + 1}</span>
                  )}
                  <span className="hidden sm:inline">{stepLabels[s]}</span>
                </div>
                {i < STEPS.length - 1 && (
                  <ChevronRight className="h-4 w-4 text-muted-foreground/50" />
                )}
              </div>
            ))}
          </nav>

          {error && (
            <div className="mb-6 flex items-start gap-2 rounded-md border border-destructive/20 bg-destructive/10 px-4 py-3 text-sm text-destructive">
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <p>{error}</p>
            </div>
          )}

          {/* Step 1: Welcome */}
          {step === "welcome" && (
            <section className="space-y-6">
              <div>
                <h1 className="font-display text-3xl font-medium">{t("setup.welcomeTitle")}</h1>
                <p className="mt-3 leading-7 text-muted-foreground">{t("setup.welcomeBody")}</p>
              </div>

              <div className="rounded-md border border-border bg-card p-5 shadow-line">
                <div className="flex items-start gap-3">
                  <ShieldCheck className="mt-0.5 h-5 w-5 text-primary" />
                  <div className="flex-1">
                    <h2 className="font-semibold">{t("setup.secretsTitle")}</h2>
                    <p className="mt-1 text-sm text-muted-foreground">{t("setup.secretsBody")}</p>
                  </div>
                </div>

                {secrets ? (
                  <div className="mt-4 space-y-3">
                    <SecretField label={t("setup.jwtSecretLabel")} value={secrets.jwtSecret} />
                    <SecretField label={t("setup.ssmKeyLabel")} value={secrets.ssmKey} />
                    <Button
                      variant="outline"
                      className="w-full"
                      onClick={() => downloadSecrets(secrets, t)}
                    >
                      <Download className="h-4 w-4" />
                      {t("setup.downloadSecrets")}
                    </Button>
                  </div>
                ) : (
                  <div className="mt-4 flex items-center gap-2 text-sm text-muted-foreground">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    {t("common.loading")}
                  </div>
                )}

                <div className="mt-4 rounded-md bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
                  {t("setup.secretsWarning")}
                </div>

                <label className="mt-4 flex cursor-pointer items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={secretsSaved}
                    onChange={(e) => setSecretsSaved(e.target.checked)}
                    className="rounded border-border"
                  />
                  {t("setup.secretsSaved")}
                </label>
              </div>

              <div className="flex justify-end">
                <Button
                  variant="ink"
                  size="lg"
                  disabled={!secretsSaved || !secrets}
                  onClick={goNext}
                >
                  {t("setup.next")}
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </section>
          )}

          {/* Step 2: Chains & RPC */}
          {step === "chains" && (
            <section className="space-y-6">
              <div>
                <h1 className="font-display text-3xl font-medium">{t("setup.chainsTitle")}</h1>
                <p className="mt-3 leading-7 text-muted-foreground">{t("setup.chainsBody")}</p>
              </div>

              <div className="space-y-3">
                {chains.map((chain) => (
                  <div
                    key={chain.name}
                    className="rounded-md border border-border bg-card p-4 shadow-line"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <Network className="h-5 w-5 text-muted-foreground" />
                        <div>
                          <p className="font-medium capitalize">{chain.name}</p>
                          <p className="text-xs text-muted-foreground">
                            Chain ID: {chain.chainId} &middot; {chain.nativeSymbol}
                          </p>
                        </div>
                      </div>
                      <label className="flex items-center gap-2 text-sm">
                        <input
                          type="checkbox"
                          checked={enabledChains[chain.name] ?? false}
                          onChange={(e) => {
                            setEnabledChains((prev) => ({
                              ...prev,
                              [chain.name]: e.target.checked,
                            }));
                            if (!e.target.checked) {
                              setRpcVerified((prev) => {
                                const next = { ...prev };
                                delete next[chain.name];
                                return next;
                              });
                            }
                          }}
                          className="rounded border-border"
                        />
                        {t("setup.enableChain")}
                      </label>
                    </div>

                    {enabledChains[chain.name] && (
                      <div className="mt-3 flex gap-2">
                        <Input
                          value={rpcUrls[chain.name] ?? ""}
                          placeholder={t("setup.rpcPlaceholder")}
                          onChange={(e) => {
                            setRpcUrls((prev) => ({ ...prev, [chain.name]: e.target.value }));
                            setRpcVerified((prev) => {
                              const next = { ...prev };
                              delete next[chain.name];
                              return next;
                            });
                          }}
                        />
                        <Button
                          variant="outline"
                          size="default"
                          disabled={!rpcUrls[chain.name]?.trim() || rpcBusy}
                          onClick={() => handleVerifyRPC(chain.name)}
                        >
                          {rpcBusy ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : rpcVerified[chain.name] ? (
                            <BadgeCheck className="h-4 w-4 text-green-600" />
                          ) : (
                            <PlugZap className="h-4 w-4" />
                          )}
                          {t("setup.verifyRpc")}
                        </Button>
                      </div>
                    )}
                  </div>
                ))}
              </div>

              <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-4 py-3 text-sm text-muted-foreground">
                <p className="font-medium text-amber-600 dark:text-amber-400">{t("setup.rpcTraceWarningTitle")}</p>
                <p className="mt-1">{t("setup.rpcTraceWarningBody")}</p>
              </div>

              <div className="flex justify-between">
                <Button variant="ghost" size="lg" onClick={goBack}>
                  <ChevronLeft className="h-4 w-4" />
                  {t("setup.back")}
                </Button>
                <Button
                  variant="ink"
                  size="lg"
                  disabled={enabledChainCount === 0 || rpcBusy}
                  onClick={handleSaveRPC}
                >
                  {rpcBusy && <Loader2 className="h-4 w-4 animate-spin" />}
                  {t("setup.next")}
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </section>
          )}

          {/* Step 3: LLM Provider */}
          {step === "llm" && (
            <section className="space-y-6">
              <div>
                <h1 className="font-display text-3xl font-medium">{t("setup.llmTitle")}</h1>
                <p className="mt-3 leading-7 text-muted-foreground">{t("setup.llmBody")}</p>
              </div>

              <div className="rounded-md border border-border bg-card p-5 shadow-line">
                <div className="space-y-4">
                  <div className="space-y-2">
                    <Label>{t("setup.llmProviderLabel")}</Label>
                    <Select
                      value={llmProvider}
                      onValueChange={(v) => {
                        setLlmProvider(v);
                        setLlmVerified(false);
                        if (v === "deepseek") {
                          setLlmBaseUrl("https://api.deepseek.com/anthropic");
                          setLlmModel("deepseek-v4-pro");
                        } else if (v === "openrouter") {
                          setLlmBaseUrl("https://openrouter.ai/api/v1");
                          setLlmModel("anthropic/claude-sonnet-4");
                        } else {
                          setLlmBaseUrl("");
                          setLlmModel("");
                        }
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="deepseek">DeepSeek</SelectItem>
                        <SelectItem value="openrouter">OpenRouter</SelectItem>
                        <SelectItem value="custom">{t("setup.llmCustom")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-2">
                    <Label>{t("setup.llmBaseUrlLabel")}</Label>
                    <Input
                      value={llmBaseUrl}
                      placeholder={t("setup.llmBaseUrlPlaceholder")}
                      onChange={(e) => {
                        setLlmBaseUrl(e.target.value);
                        setLlmVerified(false);
                      }}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label>{t("setup.llmModelLabel")}</Label>
                    <Input
                      value={llmModel}
                      placeholder={t("setup.llmModelPlaceholder")}
                      onChange={(e) => {
                        setLlmModel(e.target.value);
                        setLlmVerified(false);
                      }}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label>{t("setup.llmApiKeyLabel")}</Label>
                    <Input
                      type="password"
                      value={llmApiKey}
                      placeholder={t("setup.llmApiKeyPlaceholder")}
                      onChange={(e) => {
                        setLlmApiKey(e.target.value);
                        setLlmVerified(false);
                      }}
                    />
                  </div>

                  <Button
                    variant="outline"
                    disabled={!llmBaseUrl.trim() || !llmApiKey.trim() || llmBusy}
                    onClick={handleVerifyLLM}
                  >
                    {llmBusy ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : llmVerified ? (
                      <BadgeCheck className="h-4 w-4 text-green-600" />
                    ) : (
                      <PlugZap className="h-4 w-4" />
                    )}
                    {t("setup.llmVerify")}
                  </Button>
                </div>
              </div>

              <div className="flex justify-between">
                <Button variant="ghost" size="lg" onClick={goBack}>
                  <ChevronLeft className="h-4 w-4" />
                  {t("setup.back")}
                </Button>
                <Button
                  variant="ink"
                  size="lg"
                  disabled={llmBusy}
                  onClick={handleSaveLLM}
                >
                  {llmBusy && <Loader2 className="h-4 w-4 animate-spin" />}
                  {t("setup.next")}
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </section>
          )}

          {/* Step 4: Security Review + Data Source */}
          {step === "providers" && (
            <section className="space-y-6">
              <div>
                <h1 className="font-display text-3xl font-medium">{t("setup.securityAndDataTitle")}</h1>
                <p className="mt-3 leading-7 text-muted-foreground">{t("setup.securityAndDataBody")}</p>
                <p className="mt-2 text-sm text-muted-foreground/70">{t("setup.providersOptional")}</p>
              </div>

              {/* Security Review */}
              <div className="rounded-md border border-primary/20 bg-card p-5 shadow-line">
                <h3 className="text-sm font-semibold">{t("setup.securityReviewLabel")}</h3>
                <p className="mt-1 text-xs text-muted-foreground">{t("setup.addressLabelBody")}</p>
                <div className="mt-3 space-y-1.5">
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="setupAddressLabel" value="disabled" checked={addressLabelChoice === "disabled"} onChange={() => setAddressLabelChoice("disabled")} />
                    {t("setup.labelDisabled")}
                  </label>
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="setupAddressLabel" value="metasleuth" checked={addressLabelChoice === "metasleuth"} onChange={() => setAddressLabelChoice("metasleuth")} />
                    {t("setup.labelMetaSleuth")}
                  </label>
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="setupAddressLabel" value="x402" checked={addressLabelChoice === "x402"} onChange={() => setAddressLabelChoice("x402")} />
                    {t("setup.labelX402")}
                  </label>
                </div>
                {addressLabelChoice === "metasleuth" && (
                  <div className="mt-3 space-y-2">
                    <Label>{t("setup.metaSleuthKeyLabel")}</Label>
                    <Input
                      type="password"
                      value={metaSleuthKey}
                      placeholder={t("setup.metaSleuthKeyPlaceholder")}
                      onChange={(e) => setMetaSleuthKey(e.target.value)}
                    />
                    <p className="text-xs text-muted-foreground">
                      {t("setup.metaSleuthKeyHint")}{" "}
                      <a href={t("setup.metaSleuthKeyDoc")} target="_blank" rel="noopener noreferrer" className="underline text-primary hover:text-primary/80">
                        {t("setup.metaSleuthKeyDocLabel")}
                      </a>
                    </p>
                  </div>
                )}
              </div>

              {/* Data Source */}
              <div className="rounded-md border border-border bg-card p-5 shadow-line">
                <h3 className="text-sm font-semibold">{t("setup.dataSourceLabel")}</h3>
                <p className="mt-1 text-xs text-muted-foreground">{t("setup.portalsKeyHint")}</p>
                <div className="mt-3 space-y-2">
                  <Label>{t("setup.portalsKeyLabel")}</Label>
                  <Input
                    type="password"
                    value={portalsKey}
                    placeholder={t("setup.portalsKeyPlaceholder")}
                    onChange={(e) => setPortalsKey(e.target.value)}
                  />
                </div>
              </div>

              <div className="flex justify-between">
                <Button variant="ghost" size="lg" onClick={goBack}>
                  <ChevronLeft className="h-4 w-4" />
                  {t("setup.back")}
                </Button>
                {portalsKey.trim() || metaSleuthKey.trim() || addressLabelChoice !== "disabled" ? (
                  <Button
                    variant="ink"
                    size="lg"
                    disabled={providersBusy}
                    onClick={handleSaveProviders}
                  >
                    {providersBusy && <Loader2 className="h-4 w-4 animate-spin" />}
                    {t("setup.next")}
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                ) : (
                  <Button variant="outline" size="lg" onClick={goNext}>
                    {t("setup.skip")}
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                )}
              </div>
            </section>
          )}

          {/* Step 5: Create Account */}
          {step === "account" && (
            <section className="space-y-6">
              <div>
                <h1 className="font-display text-3xl font-medium">{t("setup.accountTitle")}</h1>
                <p className="mt-3 leading-7 text-muted-foreground">{t("setup.accountBody")}</p>
              </div>

              {authPhase === "email" && (
                <form
                  className="rounded-md border border-border bg-card p-5 shadow-line"
                  onSubmit={handleCreateAccount}
                >
                  <div className="flex items-start gap-3">
                    <MailCheck className="mt-0.5 h-5 w-5 text-primary" />
                    <div className="flex-1 space-y-4">
                      <div className="space-y-2">
                        <Label htmlFor="setup-email">{t("setup.emailLabel")}</Label>
                        <Input
                          id="setup-email"
                          type="email"
                          autoFocus
                          value={email}
                          placeholder={t("setup.emailPlaceholder")}
                          disabled={authBusy}
                          onChange={(e) => {
                            setEmail(e.target.value);
                            setError("");
                          }}
                        />
                      </div>
                      <Button type="submit" variant="ink" size="lg" disabled={authBusy || !email.trim()}>
                        {authBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}
                        {t("setup.createAccount")}
                      </Button>
                    </div>
                  </div>
                </form>
              )}

              {(authPhase === "registering" || authPhase === "confirming") && (
                <div className="rounded-md border border-border bg-card p-5 shadow-line">
                  <div className="flex items-center gap-3">
                    <Fingerprint className="h-5 w-5 animate-pulse text-primary" />
                    <div>
                      <p className="font-medium">{t("setup.passkeyPrompt")}</p>
                      <p className="text-sm text-muted-foreground">{t("setup.passkeyPromptBody")}</p>
                    </div>
                  </div>
                </div>
              )}

              {authPhase === "done" && accountUser && (
                <div className="rounded-md border border-green-200 bg-green-50 p-5 dark:border-green-900 dark:bg-green-950">
                  <div className="flex items-center gap-3">
                    <BadgeCheck className="h-5 w-5 text-green-600" />
                    <div>
                      <p className="font-medium text-green-800 dark:text-green-300">
                        {t("setup.accountCreated")}
                      </p>
                      <p className="text-sm text-green-700 dark:text-green-400">{accountUser.email}</p>
                    </div>
                  </div>
                </div>
              )}

              <div className="flex justify-between">
                <Button variant="ghost" size="lg" onClick={goBack} disabled={authBusy}>
                  <ChevronLeft className="h-4 w-4" />
                  {t("setup.back")}
                </Button>
                <Button
                  variant="ink"
                  size="lg"
                  disabled={authPhase !== "done"}
                  onClick={goNext}
                >
                  {t("setup.next")}
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </section>
          )}

          {/* Step 5: Complete */}
          {step === "complete" && (
            <section className="space-y-6">
              <div>
                <h1 className="font-display text-3xl font-medium">{t("setup.completeTitle")}</h1>
                <p className="mt-3 leading-7 text-muted-foreground">{t("setup.completeBody")}</p>
              </div>

              <div className="rounded-md border border-border bg-card p-5 shadow-line">
                <ul className="space-y-3 text-sm">
                  <li className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-green-600" />
                    {t("setup.summarySecrets")}
                  </li>
                  <li className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-green-600" />
                    {t("setup.summaryChains", { count: enabledChainCount })}
                  </li>
                  <li className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-green-600" />
                    {t("setup.summaryLlm", { provider: llmProvider })}
                  </li>
                  <li className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-green-600" />
                    {t("setup.summaryAccount", { email: accountUser?.email ?? email })}
                  </li>
                </ul>
              </div>

              <div className="flex justify-between">
                <Button variant="ghost" size="lg" onClick={goBack}>
                  <ChevronLeft className="h-4 w-4" />
                  {t("setup.back")}
                </Button>
                <Button variant="ink" size="lg" disabled={completeBusy} onClick={handleComplete}>
                  {completeBusy ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Rocket className="h-4 w-4" />
                  )}
                  {t("setup.launchButton")}
                </Button>
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  );
}

function SecretField({ label, value }: { label: string; value: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // fallback for non-HTTPS
    }
  };

  return (
    <div className="space-y-1">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <div className="flex gap-2">
        <Input readOnly value={value} className="font-mono text-xs" />
        <Button variant="outline" size="icon" onClick={handleCopy}>
          {copied ? <Check className="h-4 w-4 text-green-600" /> : <Copy className="h-4 w-4" />}
        </Button>
      </div>
    </div>
  );
}

function downloadSecrets(secrets: SetupSecrets, t: (key: string) => string) {
  const content = [
    `# Web3 Companion - Security Keys`,
    `# ${t("setup.secretsWarning")}`,
    `# Generated: ${new Date().toISOString()}`,
    ``,
    `JWT_SECRET=${secrets.jwtSecret}`,
    `SSM_KEY=${secrets.ssmKey}`,
    ``,
  ].join("\n");

  const blob = new Blob([content], { type: "text/plain" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "web3-companion-secrets.txt";
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function messageFrom(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}
