// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { CheckCircle2, ChevronDown, ChevronRight, Gauge, KeyRound, Loader2, RotateCcw, ShieldAlert, ShieldCheck, Star, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PasskeyActionModal } from "@/components/PasskeyActionModal";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { confirmPolicyDefinitionCreate, confirmPolicyDefinitionDelete, confirmPolicyDefinitionUpdate, requestPolicyDefinitionCreate, requestPolicyDefinitionDelete, requestPolicyDefinitionUpdate, type PolicyDefinition, type PolicyInput, type PolicyMode } from "@/lib/user-api";
import { recipientWhitelistStatus, type RecipientWhitelistStatus } from "@/lib/policy-whitelist";
import { policyDefinitionRowModel } from "@/lib/policy-view";
import { cn, formatTime } from "@/lib/utils";
import type { User } from "@/types/chat";
import { hasFieldErrors, limitLabel, messageFrom, validatePolicyDraftFields, type PolicyAction, type PolicyDraft } from "./policy-model";

export function PolicyDefinitionRow({
  definition,
  locale,
  active,
  dirty,
  onOpen,
}: {
  definition: PolicyDefinition;
  locale: string;
  active: boolean;
  dirty: boolean;
  onOpen: () => void;
}) {
  const { i18n, t } = useTranslation();
  const timeLocale = i18n.language.startsWith("zh") ? "zh-CN" : "en-US";
  const row = policyDefinitionRowModel(definition, locale, t("common.unlimited"));
  const Chevron = active ? ChevronDown : ChevronRight;
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-expanded={active}
      className={cn(
        "grid w-full gap-4 px-5 py-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring lg:grid-cols-[minmax(16rem,1.15fr)_minmax(50rem,2.75fr)_auto] lg:items-center",
        active ? "bg-secondary/55" : "hover:bg-secondary/45",
      )}
    >
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Chevron className="h-4 w-4 shrink-0 text-muted-foreground" />
          <span className="truncate text-sm font-semibold">{definition.name}</span>
          <span className={cn("rounded-full px-2 py-0.5 text-[11px] font-medium", row.mode === "manual" ? "bg-secondary text-secondary-foreground" : "bg-accent text-accent-foreground")}>
            {t(row.modeLabelKey)}
          </span>
          {row.defaultBadge && <span className="rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground">{t("policies.defaultBadge")}</span>}
          {dirty && <span className="rounded-full border border-[hsl(24_42%_73%)] bg-[hsl(39_44%_97%)] px-2 py-0.5 text-[11px] font-medium text-foreground">{t("policies.unsavedChanges")}</span>}
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("policies.version", { version: definition.version })}
          {definition.updatedAt ? ` · ${t("policies.updatedAt", { time: formatTime(definition.updatedAt, timeLocale) })}` : ""}
        </p>
      </div>

      <div className="grid gap-x-4 gap-y-1 text-xs text-muted-foreground sm:grid-cols-7">
        {[
          t("policies.maxPerTransaction"),
          t("policies.dailyTransferLimit"),
          t("policies.dailyTradeLimit"),
          t("policies.autoSignMode"),
          t("policies.autoSignMaxPerTx"),
          t("policies.autoSignDailyLimit"),
          t("policies.autoSignMaxCount"),
        ].map((label) => (
          <p key={label} className="flex items-end justify-center text-center text-[11px] uppercase tracking-[0.12em]">{label}</p>
        ))}
        {[
          row.limitLabels.maxPerTransaction,
          row.limitLabels.dailyTransfer,
          row.limitLabels.dailyTrade,
          t(row.modeRowKey),
          row.limitLabels.autoSignMaxPerTx,
          row.limitLabels.autoSignDailyLimit,
          row.limitLabels.autoSignMaxCountDay,
        ].map((value, i) => (
          <p key={i} className="text-center font-mono text-sm font-semibold text-foreground">{value}</p>
        ))}
      </div>

      <span className="justify-self-start rounded-md border border-border bg-background px-3 py-1 text-xs font-semibold text-muted-foreground lg:justify-self-end">
        {active ? t("policies.closePolicy") : t("policies.editPolicy")}
      </span>
    </button>
  );
}


export function PolicyDefinitionEditor({
  definition,
  draft,
  locale,
  dirty,
  busy,
  isDefault,
  version,
  onModeChange,
  onDraftChange,
  onSave,
  onReset,
  onDelete,
  onSetDefault,
  busyDefault,
}: {
  definition?: PolicyDefinition;
  draft: PolicyDraft;
  locale: string;
  dirty: boolean;
  busy: boolean;
  isDefault: boolean;
  version: number;
  onModeChange: (mode: PolicyMode) => void;
  onDraftChange: (patch: Partial<PolicyDraft>) => void;
  onSave: () => void;
  onReset: () => void;
  onDelete?: () => void;
  onSetDefault?: () => void;
  busyDefault?: boolean;
}) {
  const { i18n, t } = useTranslation();
  const timeLocale = i18n.language.startsWith("zh") ? "zh-CN" : "en-US";
  const manual = draft.mode === "manual";
  const whitelistStatus = draft.recipientWhitelistEnabled ? recipientWhitelistStatus(draft.recipientWhitelistAddrs) : undefined;
  const fieldErrors = validatePolicyDraftFields(draft, t);
  return (
    <div className="grid gap-5 px-5 py-5 xl:grid-cols-[minmax(0,1fr)_19rem]">
      <div className="space-y-5">
        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_14rem] md:items-start">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <p className="font-semibold">{definition?.name ?? t("policies.newPolicy")}</p>
              <span className={cn("rounded-full px-2 py-0.5 text-[11px] font-medium", manual ? "bg-secondary text-secondary-foreground" : "bg-accent text-accent-foreground")}>
                {manual ? t("policies.modeManual") : t("policies.modeAuto")}
              </span>
              {isDefault && <span className="rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground">{t("policies.defaultBadge")}</span>}
            </div>
            <p className="mt-1 text-xs text-muted-foreground">{version > 0 ? t("policies.version", { version }) : t("policies.unsavedVersion")}</p>
            {definition?.updatedAt && <p className="mt-1 text-xs text-muted-foreground">{t("policies.updatedAt", { time: formatTime(definition.updatedAt, timeLocale) })}</p>}
          </div>
          <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
            {t("policies.policyName")}
            <ValidatedInput
              value={draft.name}
              onChange={(value) => onDraftChange({ name: value })}
              error={fieldErrors.name}
            />
          </label>
        </div>

        <div className="grid gap-3 md:grid-cols-2">
          <ModeOption selected={manual} icon={KeyRound} title={t("policies.manualTitle")} body={t("policies.manualBody")} onClick={() => onModeChange("manual")} />
          <ModeOption selected={!manual} icon={Gauge} title={t("policies.autoTitle")} body={t("policies.autoBody")} onClick={() => onModeChange("auto")} />
        </div>

        <div className="grid gap-4 md:grid-cols-3">
          <MoneyInput label={t("policies.maxPerTransaction")} value={draft.maxPerTransactionUsd} error={fieldErrors.maxPerTransactionUsd} onChange={(value) => onDraftChange({ maxPerTransactionUsd: value })} />
          <MoneyInput label={t("policies.dailyTransferLimit")} value={draft.dailyTransferLimitUsd} error={fieldErrors.dailyTransferLimitUsd} onChange={(value) => onDraftChange({ dailyTransferLimitUsd: value })} />
          <MoneyInput label={t("policies.dailyTradeLimit")} value={draft.dailyTradeLimitUsd} error={fieldErrors.dailyTradeLimitUsd} onChange={(value) => onDraftChange({ dailyTradeLimitUsd: value })} />
        </div>

        {!manual && (
          <div className="rounded-md border border-border bg-background/55 p-4">
            <div className="flex items-center gap-2">
              <Gauge className="h-4 w-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold">{t("policies.autoSignSettings")}</h3>
            </div>
            <div className="mt-4 grid gap-4 md:grid-cols-3">
              <MoneyInput label={t("policies.autoSignMaxPerTx")} value={draft.autoSignMaxPerTxUsd} error={fieldErrors.autoSignMaxPerTxUsd} onChange={(value) => onDraftChange({ autoSignMaxPerTxUsd: value })} />
              <MoneyInput label={t("policies.autoSignDailyLimit")} value={draft.autoSignDailyLimitUsd} error={fieldErrors.autoSignDailyLimitUsd} onChange={(value) => onDraftChange({ autoSignDailyLimitUsd: value })} />
              <IntegerInput label={t("policies.autoSignMaxCount")} value={draft.autoSignMaxCountDay} error={fieldErrors.autoSignMaxCountDay} onChange={(value) => onDraftChange({ autoSignMaxCountDay: value })} />
            </div>
          </div>
        )}

        <div className="rounded-md border border-border bg-background/55 p-4">
          <label className="flex items-start gap-3">
            <input
              type="checkbox"
              checked={draft.recipientWhitelistEnabled}
              onChange={(event) => onDraftChange({ recipientWhitelistEnabled: event.target.checked })}
              className="mt-1 h-4 w-4 rounded border-input accent-primary"
            />
            <span>
              <span className="block text-sm font-semibold">{t("policies.recipientWhitelist")}</span>
              <span className="mt-1 block text-sm leading-6 text-muted-foreground">{t("policies.recipientWhitelistBody")}</span>
            </span>
          </label>
          {draft.recipientWhitelistEnabled && (
            <>
              <Textarea
                value={draft.recipientWhitelistAddrs}
                onChange={(event) => onDraftChange({ recipientWhitelistAddrs: event.target.value })}
                aria-invalid={whitelistStatus ? !whitelistStatus.valid : undefined}
                aria-describedby="recipient-whitelist-status"
                className={cn("mt-3 font-mono", whitelistStatus && !whitelistStatus.valid ? "border-destructive focus-visible:ring-destructive" : "")}
                placeholder="0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
              />
              {whitelistStatus && <RecipientWhitelistStatusView status={whitelistStatus} />}
            </>
          )}
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            onClick={onSave}
            disabled={
              busy
              || !dirty
              || hasFieldErrors(fieldErrors)
              || whitelistStatus?.valid === false
              || (draft.recipientWhitelistEnabled && (whitelistStatus?.validCount ?? 0) === 0)
            }
          >
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
            {definition ? t("policies.savePolicy") : t("policies.createPolicy")}
          </Button>
          <Button variant="outline" onClick={onReset} disabled={busy || !dirty}>
            <RotateCcw className="h-4 w-4" />
            {t("policies.resetPolicy")}
          </Button>
          {definition && !isDefault && onDelete && (
            <Button variant="destructive" onClick={onDelete} disabled={busy}>
              <Trash2 className="h-4 w-4" />
              {t("policies.deletePolicy")}
            </Button>
          )}
          {definition && onSetDefault && (
            <Button variant="outline" onClick={onSetDefault} disabled={isDefault || busy || busyDefault}>
              {busyDefault ? <Loader2 className="h-4 w-4 animate-spin" /> : <Star className="h-4 w-4" />}
              {t(isDefault ? "policies.alreadyDefault" : "policies.setDefault")}
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-3">
        <HighRiskWarning />
      </div>
    </div>
  );
}

function RecipientWhitelistStatusView({ status }: { status: RecipientWhitelistStatus }) {
  const { t } = useTranslation();
  if (status.valid) {
    return (
      <div id="recipient-whitelist-status" className="mt-2 flex items-center gap-2 text-sm text-[hsl(143_36%_31%)]">
        <CheckCircle2 className="h-4 w-4 shrink-0" />
        <span>{t("policies.recipientWhitelistValid", { count: status.validCount })}</span>
      </div>
    );
  }

  return (
    <div id="recipient-whitelist-status" className="mt-2 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <div className="flex items-start gap-2">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
        <div className="min-w-0">
          <p className="font-medium">{t("policies.recipientWhitelistInvalidTitle")}</p>
          <ul className="mt-1 space-y-1">
            {status.invalidLines.map((issue) => (
              <li key={`${issue.line}-${issue.value}`} className="break-all">
                {t(issue.reason === "multiple_addresses" ? "policies.recipientWhitelistMultipleAddresses" : "policies.recipientWhitelistInvalidLine", {
                  line: issue.line,
                  value: issue.value,
                })}
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

function HighRiskWarning() {
  const { t } = useTranslation();
  return (
    <div className="rounded-md border border-destructive/20 bg-destructive/10 p-4">
      <div className="flex items-start gap-2">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
        <p className="text-sm leading-6 text-destructive">{t("policies.definitionUpdateWarning")}</p>
      </div>
    </div>
  );
}

function ModeOption({ selected, icon: Icon, title, body, onClick }: { selected: boolean; icon: typeof KeyRound; title: string; body: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex min-h-28 items-start gap-3 rounded-md border p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        selected ? "border-primary bg-primary text-primary-foreground" : "border-border bg-background/60 text-foreground hover:bg-secondary/60",
      )}
    >
      <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", selected ? "text-primary-foreground" : "text-muted-foreground")} />
      <span>
        <span className="block text-sm font-semibold">{title}</span>
        <span className={cn("mt-2 block text-sm leading-6", selected ? "text-primary-foreground/80" : "text-muted-foreground")}>{body}</span>
      </span>
    </button>
  );
}

function ValidatedInput({
  value,
  onChange,
  error,
  inputMode,
}: {
  value: string;
  onChange: (value: string) => void;
  error?: string;
  inputMode?: "decimal" | "numeric";
}) {
  return (
    <>
      <Input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        inputMode={inputMode}
        aria-invalid={Boolean(error)}
        className={cn("mt-2", error ? "border-destructive focus-visible:ring-destructive" : "")}
      />
      {error && <p className="mt-1.5 text-xs normal-case tracking-normal text-destructive">{error}</p>}
    </>
  );
}

function MoneyInput({ label, value, error, onChange }: { label: string; value: string; error?: string; onChange: (value: string) => void }) {
  return (
    <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
      {label}
      <ValidatedInput value={value} onChange={onChange} inputMode="decimal" error={error} />
    </label>
  );
}

function IntegerInput({ label, value, error, onChange }: { label: string; value: string; error?: string; onChange: (value: string) => void }) {
  return (
    <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
      {label}
      <ValidatedInput value={value} onChange={onChange} inputMode="numeric" error={error} />
    </label>
  );
}

function PolicySummary({ input, locale }: { input: PolicyInput; locale: string }) {
  const { t } = useTranslation();
  return (
    <div className="mt-4 grid gap-2 rounded-md border border-border bg-background/70 p-4 text-sm sm:grid-cols-2">
      <Info label={t("policies.policyName")} value={input.name} />
      <Info label={t("policies.policyMode")} value={input.mode === "auto" ? t("policies.modeAuto") : t("policies.modeManual")} />
      <Info label={t("policies.maxPerTransaction")} value={limitLabel(input.maxPerTransactionUsd, locale, t("common.unlimited"))} />
      <Info label={t("policies.autoSignMaxPerTx")} value={input.mode === "auto" ? limitLabel(input.autoSignMaxPerTxUsd, locale, t("common.unlimited")) : t("policies.manualOnly")} />
    </div>
  );
}

export function PolicyMetric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md border border-border bg-card px-4 py-3 shadow-line">
      <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</p>
      <p className="mt-2 font-mono text-2xl font-semibold">{value}</p>
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[11px] text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-sm font-semibold">{value}</p>
    </div>
  );
}

export function InlineError({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
      <p>{message}</p>
    </div>
  );
}

export function PolicyPasskeyActionModal({
  user,
  action,
  locale,
  onActionChange,
  onChallenge,
  onCancel,
  onCompleted,
}: {
  user: User;
  action: PolicyAction;
  locale: string;
  onActionChange: (action: PolicyAction) => void;
  onChallenge: (action: PolicyAction) => void;
  onCancel: () => void;
  onCompleted: (action: PolicyAction, result: unknown) => void;
}) {
  const { t } = useTranslation();
  const title = t("policies.confirmTitle");
  const body = action.kind === "delete" ? t("policies.deleteConfirmBody", { name: action.name }) : t("policies.confirmBody");

  return (
    <PasskeyActionModal
      user={user}
      title={title}
      body={body}
      challenge={action.challenge}
      danger={action.kind === "delete"}
      restoredApproved={action.restoredApproved}
      details={policyActionDetails(action, t)}
      requestLabel={t("policies.requestPasskey")}
      childrenBeforePasskey={
        <>
          {action.kind !== "delete" && <HighRiskWarning />}
          {action.kind !== "delete" && <PolicySummary input={action.input} locale={locale} />}
          {action.error && <div className="mt-4"><InlineError message={action.error} /></div>}
        </>
      }
      requestChallenge={action.challenge ? undefined : async () => {
        const challenge = action.kind === "create"
          ? await requestPolicyDefinitionCreate(user, action.input)
          : action.kind === "update"
            ? await requestPolicyDefinitionUpdate(user, action.definitionId, action.input)
            : await requestPolicyDefinitionDelete(user, action.definitionId);
        const next = { ...action, challenge, restoredApproved: false, error: "" } as PolicyAction;
        onActionChange(next);
        onChallenge(next);
        return challenge;
      }}
      confirmAction={(challengeId) => {
        if (action.kind === "create") return confirmPolicyDefinitionCreate(user, action.input, challengeId);
        if (action.kind === "update") return confirmPolicyDefinitionUpdate(user, action.definitionId, action.input, challengeId);
        return confirmPolicyDefinitionDelete(user, action.definitionId, challengeId);
      }}
      errorMessage={(error) => messageFrom(error, action.kind === "delete" ? t("policies.deleteError") : t("policies.saveError"))}
      onSuccess={(result) => onCompleted(action, result)}
      onCancel={(reason) => {
        if (reason === "used") {
          onCancel();
          return;
        }
        onCancel();
      }}
    />
  );
}

function policyActionDetails(action: PolicyAction, t: (key: string, options?: Record<string, string>) => string) {
  if (action.kind === "delete") {
    return [{ label: t("policies.policyName"), value: action.name }];
  }
  return [
    { label: t("policies.policyName"), value: action.input.name },
    { label: t("policies.policyMode"), value: action.input.mode === "auto" ? t("policies.modeAuto") : t("policies.modeManual") },
  ];
}
