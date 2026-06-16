// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  Gauge,
  KeyRound,
  Loader2,
  Plus,
  RefreshCw,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Star,
  Trash2,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppPageShell } from "@/components/AppPageShell";
import { PasskeyActionModal } from "@/components/PasskeyActionModal";
import { ProductSectionNav } from "@/components/ProductSectionNav";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  confirmPolicyDefinitionCreate,
  confirmPolicyDefinitionDelete,
  confirmPolicyDefinitionUpdate,
  fetchPasskeyChallenge,
  fetchPolicyDefinitions,
  requestPolicyDefinitionCreate,
  requestPolicyDefinitionDelete,
  requestPolicyDefinitionUpdate,
  setDefaultPolicyDefinition,
  type PolicyDefinition,
  type PolicyInput,
  type PolicyMode,
  type WalletChallenge,
} from "@/lib/user-api";
import {
  clearPasskeyAction,
  loadPasskeyAction,
  restoreDecisionForStatus,
  savePasskeyAction,
  type StoredPasskeyAction,
} from "@/lib/passkey-action-store";
import { MAX_DECIMAL_INPUT_VALUE, validateDecimalInput, validateIntegerInput, validateNameInput } from "@/lib/input-validation";
import { parseRecipientWhitelist, recipientWhitelistStatus, type RecipientWhitelistStatus } from "@/lib/policy-whitelist";
import { policyDefinitionRowModel } from "@/lib/policy-view";
import { cn, formatTime, formatUsd } from "@/lib/utils";
import type { Page, User } from "@/types/chat";

import {
  PolicyDefinitionEditor,
  PolicyDefinitionRow,
  PolicyMetric,
  InlineError,
  PolicyPasskeyActionModal,
} from "./policies/policy-ui";
import {
  draftFromDefinition,
  emptyDraft,
  firstFieldError,
  hasFieldErrors,
  messageFrom,
  policyInputFromDraft,
  policyIsDirty,
  preparePolicyInputFromDraft,
  savePolicyPasskeyAction,
  validatePolicyDraftFields,
  validatePolicyInput,
  type PolicyAction,
  type PolicyDraft,
} from "./policies/policy-model";
export function PolicyPage({
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
  const [definitions, setDefinitions] = useState<PolicyDefinition[]>([]);
  const [policyOffset, setPolicyOffset] = useState(0);
  const [policyHasMore, setPolicyHasMore] = useState(false);
  const [loadingMorePolicies, setLoadingMorePolicies] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, PolicyDraft>>({});
  const [newDraft, setNewDraft] = useState<PolicyDraft>(() => emptyDraft());
  const [busyDefaultId, setBusyDefaultId] = useState("");
  const [activeDefinitionId, setActiveDefinitionId] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [policyAction, setPolicyAction] = useState<PolicyAction | null>(null);
  const [summary, setSummary] = useState({ manual: 0, auto: 0, defaultName: "default" });

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await fetchPolicyDefinitions(user, 0);
      setDefinitions(result.items);
      setPolicyHasMore(result.hasMore);
      setPolicyOffset(result.items.length);
      if (result.summary) setSummary(result.summary);
      setDrafts((current) => {
        const next = { ...current };
        for (const definition of result.items) {
          if (!next[definition.policyDefinitionId]) next[definition.policyDefinitionId] = draftFromDefinition(definition);
        }
        return next;
      });
    } catch (loadError) {
      setError(messageFrom(loadError, t("policies.loadError")));
      setDefinitions([]);
    } finally {
      setLoading(false);
    }
  }, [t, user]);

  const loadMorePolicies = useCallback(async () => {
    setLoadingMorePolicies(true);
    try {
      const result = await fetchPolicyDefinitions(user, policyOffset);
      setDefinitions((current) => [...current, ...result.items]);
      setPolicyHasMore(result.hasMore);
      setPolicyOffset((prev) => prev + result.items.length);
      setDrafts((current) => {
        const next = { ...current };
        for (const definition of result.items) {
          if (!next[definition.policyDefinitionId]) next[definition.policyDefinitionId] = draftFromDefinition(definition);
        }
        return next;
      });
    } catch {
      onToast(t("common.loadMoreFailed"));
    } finally {
      setLoadingMorePolicies(false);
    }
  }, [user, policyOffset]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    let active = true;
    const stored = loadPasskeyAction(user.userId, "policies");
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
          clearPasskeyAction(user.userId, "policies");
          onToast(t("passkeyModal.restoredUnavailable"));
          return;
        }
        const challenge = {
          ...stored.challenge,
          summary: fresh.summary ?? stored.challenge.summary,
          timeoutAt: fresh.expiresAt ?? stored.challenge.timeoutAt,
        };
        const restoredApproved = decision === "auto_confirm";
        if (stored.kind === "policy_definition_create") {
          setPolicyAction({ kind: "create", input: stored.input, challenge, restoredApproved });
        } else if (stored.kind === "policy_definition_update") {
          setPolicyAction({ kind: "update", definitionId: stored.policyDefinitionId, input: stored.input, challenge, restoredApproved });
        } else if (stored.kind === "policy_definition_delete") {
          setPolicyAction({ kind: "delete", definitionId: stored.policyDefinitionId, name: stored.name, challenge, restoredApproved });
        }
      })
      .catch(() => {
        if (!active) return;
        clearPasskeyAction(user.userId, "policies");
        onToast(t("passkeyModal.restoredUnavailable"));
      });
    return () => {
      active = false;
    };
  }, [definitions, onToast, t, user]);

  const updateDraft = (definitionId: string, patch: Partial<PolicyDraft>) => {
    setDrafts((current) => ({ ...current, [definitionId]: { ...current[definitionId], ...patch } }));
  };

  const setDefinitionMode = (definition: PolicyDefinition, mode: PolicyMode) => {
    updateDraft(definition.policyDefinitionId, { mode });
  };

  const openCreate = () => {
    setCreateOpen(true);
    setActiveDefinitionId("");
  };

  const toggleDefinition = (definitionId: string) => {
    setCreateOpen(false);
    setActiveDefinitionId((current) => (current === definitionId ? "" : definitionId));
  };

  const startCreate = async () => {
    const fieldErrors = validatePolicyDraftFields(newDraft, t);
    if (hasFieldErrors(fieldErrors)) {
      setError(firstFieldError(fieldErrors) ?? t("policies.saveError"));
      return;
    }
    const prepared = preparePolicyInputFromDraft(newDraft);
    if (prepared.error) {
      setError(t(prepared.error.key, prepared.error.options));
      return;
    }
    const input = prepared.input;
    const validation = validatePolicyInput(input, t("policies.policyNameRequired"), t("policies.policyAutoLimitsRequired"));
    if (validation) {
      setError(validation);
      return;
    }
    setNewDraft((current) => ({ ...current, recipientWhitelistAddrs: prepared.whitelistText }));
    setError("");
    setPolicyAction(null);
    setPolicyAction({ kind: "create", input });
  };

  const startUpdate = async (definition: PolicyDefinition) => {
    const draft = drafts[definition.policyDefinitionId] ?? draftFromDefinition(definition);
    const fieldErrors = validatePolicyDraftFields(draft, t);
    if (hasFieldErrors(fieldErrors)) {
      setError(firstFieldError(fieldErrors) ?? t("policies.saveError"));
      return;
    }
    const prepared = preparePolicyInputFromDraft(draft);
    if (prepared.error) {
      setError(t(prepared.error.key, prepared.error.options));
      return;
    }
    const input = prepared.input;
    const validation = validatePolicyInput(input, t("policies.policyNameRequired"), t("policies.policyAutoLimitsRequired"));
    if (validation) {
      setError(validation);
      return;
    }
    updateDraft(definition.policyDefinitionId, { recipientWhitelistAddrs: prepared.whitelistText });
    setError("");
    setPolicyAction(null);
    setPolicyAction({ kind: "update", definitionId: definition.policyDefinitionId, input });
  };

  const startDelete = async (definition: PolicyDefinition) => {
    if (definition.isDefault) {
      setError(t("policies.defaultDeleteBlocked"));
      return;
    }
    setError("");
    setPolicyAction(null);
    setPolicyAction({ kind: "delete", definitionId: definition.policyDefinitionId, name: definition.name });
  };

  const startSetDefault = async (definition: PolicyDefinition) => {
    setBusyDefaultId(definition.policyDefinitionId);
    setError("");
    try {
      await setDefaultPolicyDefinition(user, definition.policyDefinitionId);
      setDefinitions([]);
      setPolicyOffset(0);
      setPolicyHasMore(false);
      void load();
      onToast(t("policies.setDefaultDone"));
    } catch (err) {
      setError(messageFrom(err, t("policies.saveError")));
    } finally {
      setBusyDefaultId("");
    }
  };

  const cancelPolicyPasskey = useCallback(() => {
    clearPasskeyAction(user.userId, "policies");
    setPolicyAction(null);
  }, [user.userId]);

  return (
    <AppPageShell user={user} onNavigate={onNavigate} onLogout={onLogout}>
      <section className="mx-auto grid w-full max-w-7xl gap-6 py-8">
        <div className="grid gap-6 lg:grid-cols-[1fr_auto]">
          <div>
            <ProductSectionNav active="policies" onNavigate={onNavigate} />
            <p className="mt-5 text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">{t("policies.eyebrow")}</p>
            <h1 className="mt-3 font-display text-5xl font-medium leading-tight">{t("policies.title")}</h1>
            <p className="mt-4 max-w-2xl leading-7 text-muted-foreground">{t("policies.intro")}</p>
          </div>
          <div className="flex flex-wrap items-start justify-start gap-2 lg:justify-end">
            <Button variant="outline" onClick={() => void load()} disabled={loading}>
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {t("common.refresh")}
            </Button>
          </div>
        </div>

        {error && <InlineError message={error} />}

        <div className="grid gap-3 md:grid-cols-3">
          <PolicyMetric label={t("policies.manualPolicies")} value={summary.manual} />
          <PolicyMetric label={t("policies.autoPolicies")} value={summary.auto} />
          <PolicyMetric label={t("policies.defaultPolicy")} value={summary.defaultName} />
        </div>

        <section className="overflow-hidden rounded-md border border-border bg-card shadow-line">
          <div className="grid gap-4 border-b border-border px-5 py-4 md:grid-cols-[1fr_auto] md:items-center">
            <div>
              <div className="flex items-center gap-2">
                <SlidersHorizontal className="h-4 w-4 text-muted-foreground" />
                <h2 className="text-lg font-semibold">{t("policies.currentPolicies")}</h2>
              </div>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t("policies.currentPoliciesBody")}</p>
            </div>
            <Button onClick={openCreate} className="shrink-0">
              <Plus className="h-4 w-4" />
              {t("policies.createPolicy")}
            </Button>
          </div>

          {createOpen && (
            <div className="border-b border-border bg-[hsl(39_44%_97%)]">
              <div className="flex items-start justify-between gap-4 px-5 py-4">
                <div>
                  <p className="text-sm font-semibold">{t("policies.createPolicy")}</p>
                  <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">{t("policies.createPolicyBody")}</p>
                </div>
                <Button variant="ghost" size="sm" onClick={() => setCreateOpen(false)}>{t("common.close")}</Button>
              </div>
              <PolicyDefinitionEditor
                draft={newDraft}
                locale={locale}
                dirty
                busy={Boolean(policyAction?.kind === "create")}
                isDefault={false}
                version={0}
                onModeChange={(mode) => setNewDraft((current) => ({ ...current, mode }))}
                onDraftChange={(patch) => setNewDraft((current) => ({ ...current, ...patch }))}
                onSave={() => void startCreate()}
                onReset={() => setNewDraft(emptyDraft())}
              />
            </div>
          )}

          <div className="divide-y divide-border">
            {loading ? (
              <div className="p-8 text-center">
                <div className="mx-auto grid h-12 w-12 place-items-center rounded-md bg-secondary text-secondary-foreground">
                  <Loader2 className="h-5 w-5 animate-spin" />
                </div>
                <h3 className="mt-4 text-xl font-semibold">{t("common.loading")}</h3>
              </div>
            ) : definitions.length === 0 ? (
              <div className="p-8 text-center">
                <div className="mx-auto grid h-12 w-12 place-items-center rounded-md bg-accent text-accent-foreground">
                  <ShieldCheck className="h-5 w-5" />
                </div>
                <h3 className="mt-4 text-xl font-semibold">{t("policies.noPoliciesTitle")}</h3>
                <p className="mx-auto mt-2 max-w-xl text-sm leading-6 text-muted-foreground">{t("policies.noPoliciesBody")}</p>
              </div>
            ) : (
              definitions.map((definition) => {
                const draft = drafts[definition.policyDefinitionId] ?? draftFromDefinition(definition);
                const input = policyInputFromDraft(draft);
                const dirty = policyIsDirty(definition, input);
                const active = activeDefinitionId === definition.policyDefinitionId;
                return (
                  <div key={definition.policyDefinitionId} className="bg-card">
                    <PolicyDefinitionRow
                      definition={definition}
                      locale={locale}
                      active={active}
                      dirty={dirty}
                      onOpen={() => toggleDefinition(definition.policyDefinitionId)}
                    />
                    {active && (
                      <div className="border-t border-border bg-background/45">
                        <PolicyDefinitionEditor
                          definition={definition}
                          draft={draft}
                          locale={locale}
                          dirty={dirty}
                          busy={Boolean(policyAction && policyAction.kind !== "create" && "definitionId" in policyAction && policyAction.definitionId === definition.policyDefinitionId)}
                          isDefault={definition.isDefault}
                          version={definition.version}
                          onModeChange={(mode) => setDefinitionMode(definition, mode)}
                          onDraftChange={(patch) => updateDraft(definition.policyDefinitionId, patch)}
                          onSave={() => void startUpdate(definition)}
                          onReset={() => updateDraft(definition.policyDefinitionId, draftFromDefinition(definition))}
                          onDelete={() => void startDelete(definition)}
                          onSetDefault={() => void startSetDefault(definition)}
                          busyDefault={busyDefaultId === definition.policyDefinitionId}
                        />
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>
          {policyHasMore && (
            <div className="flex justify-center py-4">
              <Button variant="outline" onClick={() => void loadMorePolicies()} disabled={loadingMorePolicies}>
                {loadingMorePolicies && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                {t("common.loadMore")}
              </Button>
            </div>
          )}
        </section>

        {policyAction && (
          <PolicyPasskeyActionModal
            user={user}
            action={policyAction}
            locale={locale}
            onActionChange={setPolicyAction}
            onChallenge={(action) => savePolicyPasskeyAction(user.userId, action)}
            onCancel={cancelPolicyPasskey}
            onCompleted={(action, result) => {
              clearPasskeyAction(user.userId, "policies");
              if (action.kind === "create") {
                const created = result as PolicyDefinition;
                setNewDraft(emptyDraft());
                setCreateOpen(false);
                setActiveDefinitionId(created.policyDefinitionId);
                onToast(t("policies.saved"));
              } else if (action.kind === "update") {
                const updated = result as PolicyDefinition;
                setActiveDefinitionId(updated.policyDefinitionId);
                onToast(t("policies.saved"));
              } else {
                setActiveDefinitionId((current) => (current === action.definitionId ? "" : current));
                onToast(t("policies.deleted"));
              }
              setDefinitions([]);
              setPolicyOffset(0);
              setPolicyHasMore(false);
              void load();
              setPolicyAction(null);
            }}
          />
        )}
      </section>
    </AppPageShell>
  );
}
