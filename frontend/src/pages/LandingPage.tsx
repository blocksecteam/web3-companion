// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { ArrowUpRight, Bot, Fingerprint, ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { LanguageToggle } from "@/components/LanguageToggle";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import logoUrl from "@/assets/logo.png";

export function LandingPage({ onStart }: { onStart: () => void }) {
  const { t } = useTranslation();
  const steps = [
    ["portfolio", t("landing.previewSteps.portfolio"), t("landing.previewSteps.portfolioBody"), "complete"],
    ["route", t("landing.previewSteps.route"), t("landing.previewSteps.routeBody"), "running"],
    ["security", t("landing.previewSteps.security"), t("landing.previewSteps.securityBody"), "queued"],
  ];

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col px-5 py-5 sm:px-8 lg:px-10">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <img src={logoUrl} alt="Web3 Companion" className="h-10" />
        </div>
        <div className="flex items-center gap-2">
          <LanguageToggle compact />
          <Button variant="ghost" onClick={onStart}>
            {t("landing.login")}
            <ArrowUpRight className="h-4 w-4" />
          </Button>
        </div>
      </header>

      <section className="grid flex-1 items-center gap-10 py-12 lg:grid-cols-[1.04fr_0.96fr] lg:py-8">
        <div className="max-w-3xl animate-gentle-rise">
          <div className="mb-7 inline-flex items-center gap-2 rounded-full border border-foreground/10 bg-background/70 px-3 py-1 text-sm text-muted-foreground">
            <ShieldCheck className="h-4 w-4 text-[hsl(150_23%_34%)]" />
            {t("landing.badge")}
          </div>
          <h1 className="font-display text-[clamp(3rem,8vw,7.8rem)] font-medium leading-[0.9] tracking-normal text-balance">
            {t("landing.title")}
          </h1>
          <p className="mt-7 max-w-2xl text-lg leading-8 text-muted-foreground sm:text-xl">
            {t("landing.body")}
          </p>
          <div className="mt-9 flex flex-col gap-3 sm:flex-row">
            <Button size="lg" variant="ink" onClick={onStart}>
              <Fingerprint className="h-5 w-5" />
              {t("landing.loginPasskey")}
            </Button>
            <Button size="lg" variant="outline" onClick={onStart}>
              {t("landing.create")}
            </Button>
          </div>
        </div>

        <div className="relative min-h-[520px] animate-gentle-rise [animation-delay:120ms]">
          <div className="absolute left-[7%] top-[7%] h-28 w-28 rounded-full border border-foreground/10 bg-[hsl(151_18%_76%)]/50" />
          <div className="absolute right-[3%] top-[3%] h-40 w-40 rounded-full border border-foreground/10 bg-[hsl(24_42%_73%)]/40" />
          <div className="absolute inset-x-0 bottom-0 mx-auto max-w-xl rounded-lg border border-foreground/10 bg-[hsl(39_44%_97%)]/92 p-4 shadow-soft backdrop-blur">
            <div className="flex items-start gap-3 border-b border-border pb-4">
              <div className="grid h-9 w-9 place-items-center rounded-md bg-foreground text-background">
                <Bot className="h-4 w-4" />
              </div>
              <div>
                <p className="font-medium">{t("landing.previewTitle")}</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {t("landing.previewBody")}
                </p>
              </div>
            </div>

            <div className="grid gap-3 py-4">
              {steps.map(([key, title, body, state]) => (
                <div
                  key={key}
                  className="flex items-center justify-between rounded-md border border-border bg-background/70 px-4 py-3"
                >
                  <div>
                    <p className="text-sm font-medium">{title}</p>
                    <p className="text-xs text-muted-foreground">{body}</p>
                  </div>
                  <StatusPill state={state} />
                </div>
              ))}
            </div>

            <div className="rounded-md bg-foreground p-4 text-background">
              <p className="text-sm text-background/70">{t("landing.draftLabel")}</p>
              <p className="mt-2 text-base leading-7">
                {t("landing.draft")}
              </p>
            </div>
          </div>
        </div>
      </section>
    </main>
  );
}

function StatusPill({ state }: { state: string }) {
  const { t } = useTranslation();
  const tone =
    state === "complete"
      ? "bg-[hsl(151_18%_76%)] text-[hsl(164_22%_19%)]"
      : state === "running"
        ? "bg-[hsl(24_42%_73%)] text-[hsl(24_35%_20%)]"
        : "bg-secondary text-secondary-foreground";
  return <span className={cn("rounded-full px-2 py-1 text-xs font-medium", tone)}>{t(`landing.states.${state}`)}</span>;
}
