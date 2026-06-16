// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useEffect, useState } from "react";
import { BadgeCheck, Fingerprint, Loader2, ShieldCheck, XCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { cancelPasskeyCeremony, getPasskeyAssertion } from "@/lib/passkeys";
import { fetchPublicPasskeyChallenge, submitPasskeyConfirmation, type PasskeyChallengeSummary } from "@/lib/user-api";
import { messageFromApiError } from "@/lib/client-utils";
import { localizePasskeySummary } from "@/lib/passkey-summary";

export function ConfirmPage({ challengeId, onDone }: { challengeId: string; onDone: () => void }) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<"idle" | "running" | "approved" | "error">("idle");
  const [message, setMessage] = useState("");
  const [summary, setSummary] = useState<PasskeyChallengeSummary | undefined>();
  const localizedSummary = localizePasskeySummary(summary, t);
  const hasSummaryDetails = Boolean(localizedSummary?.details?.length);
  const detailRows = hasSummaryDetails ? localizedSummary?.details ?? [] : [{ label: t("confirm.challenge"), value: challengeId }];

  useEffect(() => {
    let active = true;
    fetchPublicPasskeyChallenge(challengeId)
      .then((challenge) => {
        if (active) setSummary(challenge.summary);
      })
      .catch(() => undefined);
    return () => {
      active = false;
      void cancelPasskeyCeremony();
    };
  }, [challengeId]);

  const confirm = async () => {
    setStatus("running");
    setMessage("");
    try {
      const challenge = await fetchPublicPasskeyChallenge(challengeId);
      setSummary(challenge.summary);
      if (!challenge.publicKey) throw new Error(t("confirm.unavailable"));
      const assertion = await getPasskeyAssertion(challenge.challengeId, challenge.publicKey);
      await submitPasskeyConfirmation(assertion);
      setStatus("approved");
      setMessage(t("confirm.approvedMessage"));
    } catch (error) {
      setStatus("error");
      setMessage(passkeyErrorMessage(error, t));
    }
  };

  return (
    <main className="grid min-h-screen place-items-center px-5 py-8">
      <section className="w-full max-w-lg rounded-lg border border-border bg-card p-6 shadow-soft">
        <div className="flex items-start gap-4">
          <div className="grid h-12 w-12 place-items-center rounded-md bg-foreground text-background">
            <ShieldCheck className="h-5 w-5" />
          </div>
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              {t("confirm.eyebrow")}
            </p>
            <h1 className="mt-2 font-display text-3xl font-medium">{t("confirm.title")}</h1>
            <p className="mt-3 leading-7 text-muted-foreground">
              {t("confirm.body")}
            </p>
          </div>
        </div>

        <div className="mt-6 rounded-md border border-border bg-background/70 px-4 py-3">
          <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">{localizedSummary?.title || t("confirm.challenge")}</p>
          {!hasSummaryDetails && (
            <p className="mt-2 text-sm leading-6 text-muted-foreground">{t("confirm.detailFallback")}</p>
          )}
          <div className="mt-3 grid gap-2">
            {detailRows.map((detail) => (
              <div key={`${detail.label}:${detail.value}`} className="grid gap-1 sm:grid-cols-[8rem_1fr]">
                <span className="text-xs text-muted-foreground">{detail.label}</span>
                <span className="break-all font-mono text-xs">{detail.value}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="mt-4 rounded-md border border-[hsl(154_20%_35%/0.22)] bg-[hsl(88_30%_96%)] px-4 py-3">
          <p className="text-sm font-semibold text-[hsl(154_20%_25%)]">{t("confirm.securityNoteTitle")}</p>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">{t("confirm.securityNoteBody")}</p>
        </div>

        {message && (
          <p
            className={
              status === "error"
                ? "mt-5 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
                : "mt-5 rounded-md bg-accent px-3 py-2 text-sm text-accent-foreground"
            }
          >
            {message}
          </p>
        )}

        <div className="mt-6 flex flex-col gap-3 sm:flex-row">
          <Button className="flex-1" variant="ink" size="lg" onClick={confirm} disabled={status === "running" || status === "approved"}>
            {status === "running" ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : status === "approved" ? (
              <BadgeCheck className="h-4 w-4" />
            ) : (
              <Fingerprint className="h-4 w-4" />
            )}
            {status === "approved" ? t("confirm.approved") : t("confirm.usePasskey")}
          </Button>
          <Button variant="ghost" size="lg" onClick={onDone} disabled={status === "running"}>
            <XCircle className="h-4 w-4" />
            {t("common.close")}
          </Button>
        </div>
      </section>
    </main>
  );
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
