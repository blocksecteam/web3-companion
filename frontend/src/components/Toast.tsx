// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useEffect, useRef } from "react";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";

export function Toast({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  const { t } = useTranslation();
  // Latest-ref pattern: callers commonly pass inline arrow onDismiss
  // (e.g. App.tsx: `onDismiss={() => setToast("")}`), which is a new
  // function reference every parent re-render. Including it in the
  // effect's deps would cancel and restart the 5.2s auto-dismiss timer
  // on every unrelated parent state change, so the toast would never
  // self-dismiss while other parts of the app are active. Read the
  // latest onDismiss through a ref instead and keep deps to [message].
  const onDismissRef = useRef(onDismiss);
  onDismissRef.current = onDismiss;

  useEffect(() => {
    if (!message) return;
    const timeout = window.setTimeout(() => onDismissRef.current(), 5200);
    return () => window.clearTimeout(timeout);
  }, [message]);

  if (!message) return null;

  return (
    <div className="fixed right-4 top-4 z-50 flex max-w-sm items-start gap-3 rounded-md border border-destructive/20 bg-[hsl(39_44%_97%)] px-4 py-3 text-sm text-destructive shadow-soft">
      <p className="leading-6">{message}</p>
      <button
        className="rounded-sm p-1 text-destructive/70 transition-colors hover:bg-destructive/10 hover:text-destructive"
        onClick={onDismiss}
        aria-label={t("common.dismissNotification")}
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}
