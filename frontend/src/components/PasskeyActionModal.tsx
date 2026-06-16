// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { BadgeCheck, Fingerprint, Loader2, ShieldAlert, ShieldCheck, XCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { cancelPasskeyCeremony, getPasskeyAssertion } from "@/lib/passkeys";
import {
  cancelPasskeyChallenge,
  fetchPasskeyChallenge,
  fetchPublicPasskeyChallenge,
  submitPasskeyConfirmation,
  type PasskeyChallengeSummary,
  type WalletChallenge,
} from "@/lib/user-api";
import { messageFromApiError } from "@/lib/client-utils";
import { localizePasskeySummary } from "@/lib/passkey-summary";
import type { User } from "@/types/chat";

type ActionState =
  | "requesting"
  | "loading"
  | "pending"
  | "authenticating"
  | "confirming"
  | "success"
  | "terminal"
  | "error";

export function PasskeyActionModal<Result>({
  user,
  title,
  body,
  challenge,
  danger = false,
  restoredApproved = false,
  requestLabel,
  approveLabel,
  closeLabel,
  keepOpenOnSuccess = false,
  canRequest = true,
  details,
  childrenBeforePasskey,
  childrenAfterSuccess,
  requestChallenge,
  confirmAction,
  onChallenge,
  onSuccess,
  onCancel,
  errorMessage,
}: {
  user: User;
  title: string;
  body: string;
  challenge?: WalletChallenge;
  danger?: boolean;
  restoredApproved?: boolean;
  requestLabel?: string;
  approveLabel?: string;
  closeLabel?: string;
  keepOpenOnSuccess?: boolean;
  canRequest?: boolean;
  details?: Array<{ label: string; value: string }>;
  childrenBeforePasskey?: ReactNode;
  childrenAfterSuccess?: (result: Result) => ReactNode;
  requestChallenge?: () => Promise<WalletChallenge>;
  confirmAction: (challengeId: string) => Promise<Result>;
  onChallenge?: (challenge: WalletChallenge) => void;
  onSuccess: (result: Result) => void;
  onCancel: (reason: "cancelled" | "expired" | "used" | "error") => void;
  errorMessage?: (error: unknown, phase: "request" | "confirm") => string;
}) {
  const { t } = useTranslation();
  const [activeChallenge, setActiveChallenge] = useState<WalletChallenge | undefined>(challenge);
  const [summary, setSummary] = useState<PasskeyChallengeSummary | undefined>(challenge?.summary);
  const [state, setState] = useState<ActionState>(challenge ? (restoredApproved ? "confirming" : "loading") : "pending");
  const [message, setMessage] = useState("");
  const [result, setResult] = useState<Result | undefined>();
  const confirmingRef = useRef(false);
  const onCancelRef = useRef(onCancel);

  useEffect(() => {
    onCancelRef.current = onCancel;
  }, [onCancel]);

  useEffect(() => {
    setActiveChallenge(challenge);
    setSummary(challenge?.summary);
    setResult(undefined);
    setMessage("");
    confirmingRef.current = false;
    setState(challenge ? (restoredApproved ? "confirming" : "loading") : "pending");
  }, [challenge, restoredApproved]);

  const finishAction = async (challengeId: string) => {
    if (confirmingRef.current) return;
    confirmingRef.current = true;
    setState("confirming");
    setMessage(t("passkeyAction.confirming"));
    try {
      const actionResult = await confirmAction(challengeId);
      setResult(actionResult);
      setState("success");
      setMessage(t("passkeyAction.completed"));
      onSuccess(actionResult);
      if (!keepOpenOnSuccess) onCancelRef.current("used");
    } catch (error) {
      confirmingRef.current = false;
      setState("error");
      setMessage(errorMessage?.(error, "confirm") ?? messageFrom(error, t("passkeyAction.confirmError")));
    }
  };

  useEffect(() => {
    let active = true;
    if (!activeChallenge) return () => {
      active = false;
    };
    if (restoredApproved) {
      void finishAction(activeChallenge.challengeId);
      return () => {
        active = false;
        void cancelPasskeyCeremony();
      };
    }
    setState("loading");
    fetchPasskeyChallenge(user, activeChallenge.challengeId)
      .then((fresh) => {
        if (!active) return;
        setSummary(fresh.summary ?? activeChallenge.summary);
        if (fresh.status === "pending") {
          setState("pending");
          setMessage("");
        } else if (fresh.status === "approved") {
          void finishAction(fresh.challengeId);
        } else {
          setState("terminal");
          setMessage(t(`passkeyModal.status.${statusKey(fresh.status)}`));
        }
      })
      .catch((error) => {
        if (!active) return;
        setState("error");
        setMessage(messageFrom(error, t("passkeyModal.loadError")));
      });
    return () => {
      active = false;
      void cancelPasskeyCeremony();
    };
    // finishAction intentionally closes over the latest action props.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeChallenge?.challengeId, restoredApproved, t, user]);

  const localizedSummary = useMemo(() => localizePasskeySummary(summary, t), [summary, t]);
  const detailRows = useMemo(() => {
    const rows = details?.filter((detail) => detail.label && detail.value) ?? [];
    if (rows.length > 0) return rows;
    const summaryRows = localizedSummary?.details?.filter((detail) => detail.label && detail.value) ?? [];
    if (summaryRows.length > 0) return summaryRows;
    if (activeChallenge) return [{ label: t("confirm.challenge"), value: activeChallenge.challengeId }];
    return [];
  }, [activeChallenge, details, localizedSummary?.details, t]);

  const autoRequestedRef = useRef(false);
  useEffect(() => {
    // Auto-fire the challenge request once `canRequest` is true so the
    // modal renders directly in the "use Passkey" state. The ref
    // single-flights against React 18 StrictMode's double-mount and
    // against further edits to the caller's gate (e.g. re-typing the
    // wallet id for delete) — both would otherwise re-issue challenges.
    if (activeChallenge || !requestChallenge) return;
    if (!canRequest) return;
    if (autoRequestedRef.current) return;
    autoRequestedRef.current = true;
    void request();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeChallenge, requestChallenge, canRequest]);

  const request = async () => {
    if (!requestChallenge) return;
    setState("requesting");
    setMessage("");
    try {
      const nextChallenge = await requestChallenge();
      setActiveChallenge(nextChallenge);
      setSummary(nextChallenge.summary);
      onChallenge?.(nextChallenge);
      setState("pending");
    } catch (error) {
      setState("error");
      setMessage(errorMessage?.(error, "request") ?? messageFrom(error, t("passkeyAction.requestError")));
    }
  };

  const approve = async () => {
    if (!activeChallenge) return;
    setState("authenticating");
    setMessage("");
    try {
      const fresh = await fetchPublicPasskeyChallenge(activeChallenge.challengeId);
      setSummary(fresh.summary ?? summary);
      if (!fresh.publicKey) throw new Error(t("passkeyModal.loadError"));
      const assertion = await getPasskeyAssertion(fresh.challengeId, fresh.publicKey);
      await submitPasskeyConfirmation(assertion);
      await finishAction(fresh.challengeId);
    } catch (error) {
      confirmingRef.current = false;
      setState("error");
      setMessage(passkeyErrorMessage(error, t));
    }
  };

  const cancel = async () => {
    try {
      await cancelPasskeyCeremony();
      if (activeChallenge && canCancelChallenge(state)) {
        await cancelPasskeyChallenge(user, activeChallenge.challengeId).catch(() => undefined);
      }
    } finally {
      onCancelRef.current(state === "terminal" ? "expired" : "cancelled");
    }
  };

  const busy = state === "requesting" || state === "loading" || state === "authenticating" || state === "confirming";
  const terminal = state === "terminal";
  const canApprove = Boolean(activeChallenge) && (state === "pending" || state === "error");
  const Icon = danger ? ShieldAlert : ShieldCheck;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-background/85 px-4 py-6 backdrop-blur-sm" role="presentation">
      <section
        aria-modal="true"
        role="dialog"
        aria-labelledby="passkey-action-title"
        className="w-full max-w-xl rounded-md border border-border bg-card p-5 shadow-soft"
      >
        <div className="flex items-start gap-3">
          <div className={danger ? "grid h-11 w-11 shrink-0 place-items-center rounded-md bg-destructive text-destructive-foreground" : "grid h-11 w-11 shrink-0 place-items-center rounded-md bg-foreground text-background"}>
            <Icon className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{t("passkeyModal.eyebrow")}</p>
            <h2 id="passkey-action-title" className="mt-1 text-xl font-semibold">{title}</h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">{body}</p>
          </div>
        </div>

        {detailRows.length > 0 && (
          <div className="mt-5 rounded-md border border-border bg-background/70 p-4">
            <p className="text-sm font-semibold">{localizedSummary?.title || t("passkeyModal.operation")}</p>
            <div className="mt-3 grid gap-2">
              {detailRows.map((detail) => (
                <div key={`${detail.label}:${detail.value}`} className="grid gap-1 sm:grid-cols-[8rem_1fr]">
                  <span className="text-xs text-muted-foreground">{detail.label}</span>
                  <span className="break-all font-mono text-xs">{detail.value}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {childrenBeforePasskey && state !== "success" && (
          <div className="mt-5">{childrenBeforePasskey}</div>
        )}

        {state === "success" && result !== undefined && childrenAfterSuccess?.(result)}

        {message && (
          <p className={state === "error" || terminal ? "mt-4 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive" : "mt-4 rounded-md bg-accent px-3 py-2 text-sm text-accent-foreground"}>
            {message}
          </p>
        )}

        <div className="mt-5 flex flex-col gap-2 sm:flex-row">
          {!activeChallenge && requestChallenge && (
            <Button className="flex-1" variant={danger ? "destructive" : "ink"} onClick={() => void request()} disabled={!canRequest || busy}>
              {state === "requesting" ? <Loader2 className="h-4 w-4 animate-spin" /> : <Fingerprint className="h-4 w-4" />}
              {requestLabel ?? t("passkeyAction.request")}
            </Button>
          )}
          {activeChallenge && state !== "success" && (
            <Button className="flex-1" variant={danger ? "destructive" : "ink"} onClick={() => void approve()} disabled={!canApprove || busy || terminal}>
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Fingerprint className="h-4 w-4" />}
              {state === "loading" ? t("common.loading") : state === "confirming" ? t("passkeyAction.confirming") : approveLabel ?? t("passkeyModal.usePasskey")}
            </Button>
          )}
          {state === "success" && (
            <Button className="flex-1" variant="ink" onClick={() => onCancelRef.current("used")} disabled={busy}>
              <BadgeCheck className="h-4 w-4" />
              {closeLabel ?? t("common.close")}
            </Button>
          )}
          {state !== "success" && (
            <Button variant="outline" onClick={() => void cancel()} disabled={state === "authenticating" || state === "confirming" || state === "requesting"}>
              <XCircle className="h-4 w-4" />
              {terminal ? t("common.close") : t("passkeyModal.cancel")}
            </Button>
          )}
        </div>
      </section>
    </div>
  );
}

function canCancelChallenge(state: ActionState) {
  return state === "pending" || state === "authenticating" || state === "error" || state === "loading";
}

function statusKey(status: string) {
  if (status === "timeout" || status === "cancelled" || status === "used" || status === "completed") return status;
  return "unavailable";
}

function messageFrom(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

function passkeyErrorMessage(error: unknown, t: (key: string) => string) {
  return messageFromApiError(error, t("confirm.fallbackError"), {
    challenge_expired: t("confirm.expired"),
    challenge_cancelled: t("confirm.cancelled"),
    challenge_used: t("confirm.alreadyUsed"),
    challenge_unavailable: t("confirm.unavailable"),
    challenge_not_found: t("confirm.unavailable"),
    challenge_already_used: t("confirm.alreadyUsed"),
    credential_mismatch: t("confirm.credentialMismatch"),
  });
}
