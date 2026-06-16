// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { CircleDollarSign, ScrollText, Settings, ShieldCheck, SlidersHorizontal, WalletCards } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import type { Page } from "@/types/chat";

const items: { page: Page; labelKey: string; icon: typeof CircleDollarSign }[] = [
  { page: "portfolio", labelKey: "nav.portfolio", icon: CircleDollarSign },
  { page: "transactions", labelKey: "nav.transactions", icon: ScrollText },
  { page: "wallets", labelKey: "nav.wallets", icon: WalletCards },
  { page: "policies", labelKey: "nav.policies", icon: SlidersHorizontal },
  { page: "audit", labelKey: "nav.audit", icon: ShieldCheck },
  { page: "settings", labelKey: "nav.settings", icon: Settings },
];

export function ProductSectionNav({
  active,
  onNavigate,
}: {
  active: "portfolio" | "transactions" | "wallets" | "policies" | "audit" | "settings";
  onNavigate: (page: Page) => void;
}) {
  const { t } = useTranslation();

  return (
    <nav className="flex flex-wrap gap-2" aria-label={t("nav.productSections")}>
      {items.map((item) => {
        const Icon = item.icon;
        const selected = item.page === active;
        return (
          <button
            key={item.page}
            type="button"
            className={cn(
              "inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              selected
                ? "border-primary bg-primary text-primary-foreground"
                : "border-border bg-card text-muted-foreground hover:bg-secondary/70 hover:text-foreground",
            )}
            aria-current={selected ? "page" : undefined}
            onClick={() => onNavigate(item.page)}
          >
            <Icon className="h-4 w-4" />
            {t(item.labelKey)}
          </button>
        );
      })}
    </nav>
  );
}
