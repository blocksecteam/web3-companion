// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useEffect, useMemo, useRef, useState } from "react";
import { BadgeCheck, Fingerprint, Loader2, ShieldCheck, XCircle } from "lucide-react";
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

type ModalState = "loading" | "pending" | "running" | "approved" | "terminal" | "error";

export function PasskeyApprovalModal({
  user,
  challenge,
  title,
  body,
  restoredApproved = false,
  onApproved,
  onCancel,
}: {
  user: User;
  challenge: WalletChallenge;
  title: string;
  body: string;
  restoredApproved?: boolean;
  onApproved: () => void;
  onCancel: (reason: "cancelled" | "expired" | "used" | "error") => void;
}) {
  const { t } = useTranslation();
  const [state, setState] = useState<ModalState>(restoredApproved ? "approved" : "loading");
  const [message, setMessage] = useState("");
  const [summary, setSummary] = useState<PasskeyChallengeSummary | undefined>(challenge.summary);
  const onCancelRef = useRef(onCancel);

  useEffect(() => {
    onCancelRef.current = onCancel;
  }, [onCancel]);

  useEffect(() => {
    let active = true;
    if (restoredApproved) {
      setState("approved");
      setMessage(t("passkeyModal.approvedBody"));
      return () => {
        active = false;
        void cancelPasskeyCeremony();
      };
    }
    setState("loading");
    fetchPasskeyChallenge(user, challenge.challengeId)
      .then((fresh) => {
        if (!active) return;
        setSummary(fresh.summary ?? challenge.summary);
        if (fresh.status === "pending") {
          setState("pending");
          setMessage("");
        } else if (fresh.status === "approved") {
          setState("approved");
          setMessage(t("passkeyModal.approvedBody"));
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
  }, [challenge.challengeId, challenge.summary, restoredApproved, t, user]);

  const localizedSummary = useMemo(() => localizePasskeySummary(summary, t), [summary, t]);
  const detailRows = useMemo(() => {
    const rows = localizedSummary?.details?.filter((detail) => detail.label && detail.value) ?? [];
    if (rows.length > 0) return rows;
    return [{ label: t("confirm.challenge"), value: challenge.challengeId }];
  }, [challenge.challengeId, localizedSummary?.details, t]);

  const usePasskey = async () => {
    setState("running");
    setMessage("");
    try {
      const fresh = await fetchPublicPasskeyChallenge(challenge.challengeId);
      setSummary(fresh.summary ?? summary);
      if (!fresh.publicKey) throw new Error(t("passkeyModal.loadError"));
      const assertion = await getPasskeyAssertion(fresh.challengeId, fresh.publicKey);
      await submitPasskeyConfirmation(assertion);
      setState("approved");
      setMessage(t("passkeyModal.approvedBody"));
    } catch (error) {
      setState("error");
      setMessage(passkeyErrorMessage(error, t));
    }
  };

  const cancel = async () => {
    try {
      await cancelPasskeyCeremony();
      if (state === "pending" || state === "running" || state === "error" || state === "loading") {
        await cancelPasskeyChallenge(user, challenge.challengeId).catch(() => undefined);
      }
    } finally {
      onCancelRef.current(state === "terminal" ? "expired" : "cancelled");
    }
  };

  const closeTerminal = () => {
    onCancelRef.current("expired");
  };

  const terminal = state === "terminal";
  const canUsePasskey = state === "pending" || state === "error";
  const canContinue = state === "approved";
  const busy = state === "running" || state === "loading";

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-background/85 px-4 py-6 backdrop-blur-sm" role="presentation">
      <section
        aria-modal="true"
        role="dialog"
        aria-labelledby="passkey-approval-title"
        className="w-full max-w-lg rounded-md border border-border bg-card p-5 shadow-soft"
      >
        <div className="flex items-start gap-3">
          <div className="grid h-11 w-11 shrink-0 place-items-center rounded-md bg-foreground text-background">
            <ShieldCheck className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{t("passkeyModal.eyebrow")}</p>
            <h2 id="passkey-approval-title" className="mt-1 text-xl font-semibold">{title}</h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">{body}</p>
          </div>
        </div>

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

        {message && (
          <p className={state === "error" || terminal ? "mt-4 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive" : "mt-4 rounded-md bg-accent px-3 py-2 text-sm text-accent-foreground"}>
            {message}
          </p>
        )}

        <div className="mt-5 flex flex-col gap-2 sm:flex-row">
          {canContinue ? (
            <Button className="flex-1" variant="ink" onClick={onApproved}>
              <BadgeCheck className="h-4 w-4" />
              {t("passkeyModal.continue")}
            </Button>
          ) : (
            <Button className="flex-1" variant="ink" onClick={() => void usePasskey()} disabled={!canUsePasskey || busy || terminal}>
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Fingerprint className="h-4 w-4" />}
              {state === "loading" ? t("common.loading") : t("passkeyModal.usePasskey")}
            </Button>
          )}
          <Button variant="outline" onClick={terminal ? closeTerminal : () => void cancel()} disabled={state === "running"}>
            <XCircle className="h-4 w-4" />
            {terminal ? t("common.close") : t("passkeyModal.cancel")}
          </Button>
        </div>
      </section>
    </div>
  );
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
