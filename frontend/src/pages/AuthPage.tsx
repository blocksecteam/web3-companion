// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useState } from "react";
import type React from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowUpRight,
  BadgeCheck,
  ChevronLeft,
  Fingerprint,
  KeyRound,
  Loader2,
  MailCheck,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  claimPasskeyResult,
  loginOptions,
  loginVerify,
  registerEmail,
  setupPasskey,
  userFromLogin,
  userFromRegistration,
  verifyPasskey,
} from "@/lib/auth-client";
import { createPasskeyCredential, getPasskeyAssertion } from "@/lib/passkeys";
import type { AuthPhase, User } from "@/types/chat";

export function AuthPage({
  onBack,
  onAuthenticated,
  onToast,
}: {
  onBack: () => void;
  onAuthenticated: (user: User) => void;
  onToast: (message: string) => void;
}) {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [phase, setPhase] = useState<AuthPhase>("email");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [userId, setUserId] = useState("");
  const [tempToken, setTempToken] = useState("");
  const [pendingUser, setPendingUser] = useState<User | null>(null);

  const normalizedEmail = email.trim().toLowerCase();

  const submitEmail = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!assertEmail()) return;
    setBusy(true);
    setError("");
    try {
      const options = await loginOptions(normalizedEmail);
      setUserId(options.user_id);
      setPhase("login-passkey");
    } catch (loginError) {
      const errorCode = apiCode(loginError);
      if (errorCode && errorCode !== "login_unavailable") {
        setError(messageFrom(loginError, t("auth.loginStartFallback")));
        return;
      }
      try {
        const response = await registerEmail(normalizedEmail);
        setUserId(response.user_id);
        setTempToken(response.temp_token);
        setPhase("register-passkey");
        onToast(t("auth.registerStarted"));
      } catch (registerError) {
        if (apiCode(registerError) === "already_registered") {
          setError(t("auth.registeredLoginUnavailable"));
          return;
        }
        setError(messageFrom(registerError, t("auth.registerFallback")));
      }
    } finally {
      setBusy(false);
    }
  };

  const runRegister = async () => {
    setBusy(true);
    setError("");
    try {
      const setup = await setupPasskey(tempToken);
      const credential = await createPasskeyCredential(setup.challenge_id, setup.public_key);
      await verifyPasskey(credential);
      const claimed = await claimPasskeyResult({
        temp_token: tempToken,
        challenge_id: setup.challenge_id,
      });
      const nextUser = userFromRegistration(normalizedEmail, claimed);
      setPendingUser(nextUser);
      setPhase("confirm-passkey");
    } catch (authError) {
      const message = messageFrom(authError, t("auth.passkeyRegisterFallback"));
      setError(message);
      onToast(t("auth.restartRegistrationToast", { message }));
      setPendingUser(null);
      setPhase("email");
    } finally {
      setBusy(false);
    }
  };

  const runLogin = async () => {
    setBusy(true);
    setError("");
    try {
      const options = await loginOptions(normalizedEmail);
      const assertion = await getPasskeyAssertion(options.challenge_id, options.public_key);
      const response = await loginVerify(assertion);
      onAuthenticated(userFromLogin(response));
    } catch (authError) {
      const message = messageFrom(authError, t("auth.passkeyLoginFallback"));
      setError(message);
      onToast(t("auth.restartLoginToast", { message }));
      setPhase("email");
    } finally {
      setBusy(false);
    }
  };

  const runConfirm = async () => {
    setBusy(true);
    setError("");
    try {
      if (!pendingUser) {
        throw new Error(t("auth.passkeyConfirmFallback"));
      }
      const options = await loginOptions(pendingUser.email);
      const assertion = await getPasskeyAssertion(options.challenge_id, options.public_key);
      const response = await loginVerify(assertion);
      onAuthenticated(userFromLogin(response));
    } catch (authError) {
      const message = messageFrom(authError, t("auth.passkeyConfirmFallback"));
      setError(message);
      onToast(t("auth.restartRegistrationToast", { message }));
      setPendingUser(null);
      setPhase("email");
    } finally {
      setBusy(false);
    }
  };

  const assertEmail = () => {
    setError("");
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)) {
      setError(t("auth.validEmailRequired"));
      return false;
    }
    return true;
  };

  const resetEmail = () => {
    setPhase("email");
    setUserId("");
    setTempToken("");
    setPendingUser(null);
    setError("");
  };

  return (
    <main className="grid min-h-screen px-5 py-6 md:grid-cols-[0.9fr_1.1fr] md:px-8">
      <aside className="hidden flex-col justify-between rounded-lg border border-foreground/10 bg-foreground p-8 text-background shadow-soft md:flex">
        <Button
          variant="ghost"
          className="w-fit text-background hover:bg-background/10 hover:text-background"
          onClick={onBack}
        >
          <ChevronLeft className="h-4 w-4" />
          {t("auth.back")}
        </Button>
        <div>
          <p className="mb-5 inline-flex items-center gap-2 rounded-full bg-background/10 px-3 py-1 text-sm text-background/80">
            <Fingerprint className="h-4 w-4" />
            {t("auth.passkeyFirst")}
          </p>
          <h2 className="max-w-lg font-display text-5xl font-medium leading-tight">
            {t("auth.heroTitle")}
          </h2>
          <p className="mt-6 max-w-md leading-7 text-background/70">
            {t("auth.heroBody")}
          </p>
        </div>
        <div className="grid grid-cols-4 gap-3 text-xs text-background/70">
          <span>{t("auth.steps.email")}</span>
          <span>{t("auth.steps.register")}</span>
          <span>{t("auth.steps.passkey")}</span>
          <span>{t("auth.steps.confirm")}</span>
        </div>
      </aside>

      <section className="mx-auto flex w-full max-w-lg flex-col justify-center py-12 md:pl-10">
        <Button variant="ghost" className="mb-8 w-fit md:hidden" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          {t("auth.back")}
        </Button>

        <div className="mb-8">
          <p className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
            {t("auth.eyebrow")}
          </p>
          <h1 className="mt-3 font-display text-4xl font-medium">{t("auth.title")}</h1>
        </div>

        {phase === "email" && (
          <form className="rounded-lg border border-border bg-card p-5 shadow-line" onSubmit={submitEmail}>
            <div className="flex items-start gap-4">
              <div className="grid h-11 w-11 shrink-0 place-items-center rounded-md bg-accent text-accent-foreground">
                <MailCheck className="h-5 w-5" />
              </div>
              <div>
                <h2 className="text-xl font-semibold">{t("auth.emailLabel")}</h2>
                <p className="mt-2 leading-7 text-muted-foreground">{t("auth.intro")}</p>
              </div>
            </div>

            <div className="mt-5 space-y-2">
              <Label htmlFor="email">{t("auth.emailLabel")}</Label>
              <div className="flex flex-col gap-3 sm:flex-row">
                <Input
                  id="email"
                  autoFocus
                  type="text"
                  inputMode="email"
                  value={email}
                  placeholder={t("auth.emailPlaceholder")}
                  disabled={busy}
                  onChange={(event) => {
                    setEmail(event.target.value);
                    setUserId("");
                    setError("");
                  }}
                />
              </div>
            </div>

            {error && <p className="mt-5 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</p>}
            <div className="mt-6 flex flex-col gap-3 sm:flex-row">
              <Button className="flex-1" variant="ink" size="lg" type="submit" disabled={busy}>
                {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowUpRight className="h-4 w-4" />}
                {t("auth.continue")}
              </Button>
            </div>
          </form>
        )}

        {phase === "login-passkey" && (
          <PasskeyStep
            title={t("auth.passkeyLoginTitle")}
            body={t("auth.passkeyLoginBody", { email: normalizedEmail })}
            busy={busy}
            error={error}
            actionLabel={t("auth.passkeyLoginAction")}
            icon={Fingerprint}
            onAction={runLogin}
            onChangeEmail={resetEmail}
          />
        )}

        {phase === "register-passkey" && (
          <PasskeyStep
            title={t("auth.registerPasskeyTitle")}
            body={t("auth.registerPasskeyBody", { email: normalizedEmail })}
            busy={busy}
            error={error}
            actionLabel={t("auth.registerPasskeyAction")}
            icon={KeyRound}
            onAction={runRegister}
            onChangeEmail={resetEmail}
          />
        )}

        {phase === "confirm-passkey" && (
          <PasskeyStep
            title={t("auth.confirmPasskeyTitle")}
            body={t("auth.confirmPasskeyBody")}
            busy={busy}
            error={error}
            actionLabel={t("auth.confirmPasskeyAction")}
            icon={BadgeCheck}
            onAction={runConfirm}
            onChangeEmail={resetEmail}
          />
        )}
      </section>
    </main>
  );
}

function PasskeyStep({
  title,
  body,
  busy,
  error,
  actionLabel,
  icon: Icon,
  onAction,
  onChangeEmail,
}: {
  title: string;
  body: string;
  busy: boolean;
  error: string;
  actionLabel: string;
  icon: typeof Fingerprint;
  onAction: () => void;
  onChangeEmail: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-border bg-card p-5 shadow-line">
      <div className="flex items-start gap-4">
        <div className="grid h-11 w-11 place-items-center rounded-md bg-accent text-accent-foreground">
          <Icon className="h-5 w-5" />
        </div>
        <div>
          <h2 className="text-xl font-semibold">{title}</h2>
          <p className="mt-2 leading-7 text-muted-foreground">{body}</p>
        </div>
      </div>
      {error && <p className="mt-5 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</p>}
      <div className="mt-6 flex flex-col gap-3 sm:flex-row">
        <Button className="flex-1" variant="ink" size="lg" onClick={onAction} disabled={busy}>
          {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Fingerprint className="h-4 w-4" />}
          {actionLabel}
        </Button>
        <Button variant="ghost" size="lg" onClick={onChangeEmail} disabled={busy}>
          {t("auth.changeEmail")}
        </Button>
      </div>
      <p className="mt-4 text-xs leading-5 text-muted-foreground">
        {t("auth.passkeyHint")}
      </p>
    </div>
  );
}

function messageFrom(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

function apiCode(error: unknown) {
  return typeof error === "object" && error !== null && "code" in error
    ? String((error as { code?: string }).code ?? "")
    : "";
}
