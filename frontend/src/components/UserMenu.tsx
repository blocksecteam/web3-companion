// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { ChevronDown, LogOut, ReceiptText, ScrollText, Settings, SlidersHorizontal, WalletCards } from "lucide-react";
import { useTranslation } from "react-i18next";
import { GeneratedAvatar } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import type { Page, User } from "@/types/chat";

export function UserMenu({
  user,
  onNavigate,
  onLogout,
  className,
  side = "top",
  align = "start",
}: {
  user: User;
  onNavigate: (page: Page) => void;
  onLogout: () => void;
  className?: string;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
}) {
  const { t } = useTranslation();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className={cn(
            "flex w-full items-center gap-3 rounded-md border border-border bg-card px-3 py-2 text-left shadow-line transition-colors hover:bg-secondary/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            className,
          )}
        >
          <GeneratedAvatar email={user.email} className="h-9 w-9" />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-semibold">{user.email}</span>
            <span className="block text-xs text-muted-foreground">{t("common.passkeyVerified")}</span>
          </span>
          <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} side={side} className="w-60">
        <DropdownMenuLabel>
          <div className="flex items-center gap-3">
            <GeneratedAvatar email={user.email} />
            <div className="min-w-0">
              <p className="truncate text-sm">{user.email}</p>
              <p className="text-xs font-normal text-muted-foreground">{t("common.passkeyVerified")}</p>
            </div>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => onNavigate("portfolio")}>
          <WalletCards className="h-4 w-4" />
          {t("nav.portfolio")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onNavigate("transactions")}>
          <ReceiptText className="h-4 w-4" />
          {t("nav.transactions")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onNavigate("wallets")}>
          <WalletCards className="h-4 w-4" />
          {t("nav.wallets")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onNavigate("policies")}>
          <SlidersHorizontal className="h-4 w-4" />
          {t("nav.policies")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onNavigate("audit")}>
          <ScrollText className="h-4 w-4" />
          {t("nav.audit")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onNavigate("settings")}>
          <Settings className="h-4 w-4" />
          {t("nav.settings")}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onLogout}>
          <LogOut className="h-4 w-4" />
          {t("nav.logout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
