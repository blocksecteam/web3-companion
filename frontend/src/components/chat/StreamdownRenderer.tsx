// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { Streamdown } from "streamdown";
import type { StreamdownTranslations } from "streamdown";
import { useTranslation } from "react-i18next";
import "streamdown/styles.css";

export function StreamdownRenderer({
  content,
  partial,
}: {
  content: string;
  partial?: boolean;
}) {
  const { t } = useTranslation();
  const translations = {
    openExternalLink: t("streamdown.openExternalLink"),
    externalLinkWarning: t("streamdown.externalLinkWarning"),
    close: t("streamdown.close"),
    copyLink: t("streamdown.copyLink"),
    copied: t("streamdown.copied"),
    openLink: t("streamdown.openLink"),
  } satisfies Partial<StreamdownTranslations>;

  return (
    <Streamdown
      mode={partial ? "streaming" : "static"}
      parseIncompleteMarkdown
      controls={{ code: { copy: true }, table: { copy: true } }}
      isAnimating={Boolean(partial)}
      translations={translations}
    >
      {content}
    </Streamdown>
  );
}
