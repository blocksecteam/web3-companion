// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { Languages } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { languageStorageKey, type AppLanguage } from "@/i18n";

export function LanguageToggle({ compact = false }: { compact?: boolean }) {
  const { i18n, t } = useTranslation();
  const current = i18n.language.startsWith("zh") ? "zh" : "en";

  const setLanguage = (language: AppLanguage) => {
    localStorage.setItem(languageStorageKey, language);
    void i18n.changeLanguage(language);
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size={compact ? "icon" : "default"} aria-label={t("common.language")}>
          <Languages className="h-4 w-4" />
          {!compact && <span>{current === "zh" ? t("common.chinese") : t("common.english")}</span>}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-36">
        <DropdownMenuItem onClick={() => setLanguage("zh")}>
          <span className="w-4 text-xs">{current === "zh" ? "✓" : ""}</span>
          {t("common.chinese")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => setLanguage("en")}>
          <span className="w-4 text-xs">{current === "en" ? "✓" : ""}</span>
          {t("common.english")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
