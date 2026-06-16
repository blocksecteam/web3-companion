// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { ChevronLeft } from "lucide-react";
import type React from "react";
import { useTranslation } from "react-i18next";
import { LanguageToggle } from "@/components/LanguageToggle";
import { UserMenu } from "@/components/UserMenu";
import { Button } from "@/components/ui/button";
import type { Page, User } from "@/types/chat";

export function AppPageShell({
  user,
  onNavigate,
  onLogout,
  children,
}: {
  user: User;
  onNavigate: (page: Page, sessionId?: string) => void;
  onLogout: () => void;
  children: React.ReactNode;
}) {
  const { t } = useTranslation();

  return (
    <main className="min-h-screen px-4 py-4 sm:px-6 lg:px-8">
      <header className="mx-auto flex w-full max-w-7xl items-center justify-between gap-3">
        <Button variant="ghost" onClick={() => onNavigate("chat")} className="shrink-0">
          <ChevronLeft className="h-4 w-4" />
          <span className="hidden sm:inline">{t("common.backToChat")}</span>
        </Button>
        <div className="flex min-w-0 items-center gap-2">
          <LanguageToggle compact />
          <div className="w-[min(18rem,52vw)]">
            <UserMenu user={user} onNavigate={onNavigate} onLogout={onLogout} side="bottom" align="end" />
          </div>
        </div>
      </header>
      {children}
    </main>
  );
}
