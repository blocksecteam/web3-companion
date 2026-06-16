// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

interface EtherscanPagerProps {
  page: number;
  totalPages: number;
  disabled?: boolean;
  onFirst: () => void;
  onPrev: () => void;
  onNext: () => void;
  onLast: () => void;
}

export function EtherscanPager({
  page,
  totalPages,
  disabled,
  onFirst,
  onPrev,
  onNext,
  onLast,
}: EtherscanPagerProps) {
  const { t } = useTranslation();
  const isFirst = page <= 1;
  const isLast = page >= totalPages;

  if (totalPages <= 1) return null;

  return (
    <div className="flex items-center justify-center gap-1 py-4">
      <Button
        variant="outline"
        size="sm"
        onClick={onFirst}
        disabled={disabled || isFirst}
        aria-label={t("common.firstPage")}
      >
        <ChevronsLeft className="h-4 w-4" />
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={onPrev}
        disabled={disabled || isFirst}
        aria-label={t("common.prevPage")}
      >
        <ChevronLeft className="h-4 w-4" />
      </Button>
      <span className="min-w-[8rem] px-3 text-center text-sm text-muted-foreground">
        {t("common.pageOf", { page, total: totalPages })}
      </span>
      <Button
        variant="outline"
        size="sm"
        onClick={onNext}
        disabled={disabled || isLast}
        aria-label={t("common.nextPage")}
      >
        <ChevronRight className="h-4 w-4" />
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={onLast}
        disabled={disabled || isLast}
        aria-label={t("common.lastPage")}
      >
        <ChevronsRight className="h-4 w-4" />
      </Button>
    </div>
  );
}
