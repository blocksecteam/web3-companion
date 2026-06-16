// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { Copy, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PasskeyActionModal } from "@/components/PasskeyActionModal";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { confirmWalletDelete, confirmWalletExport, confirmWalletPolicyBinding, requestWalletDelete, requestWalletExport, type PolicyDefinition, type SettingsWallet } from "@/lib/user-api";
import { shortAddress } from "@/lib/utils";
import type { User } from "@/types/chat";
import { messageFrom, policyName, walletDeleteErrorMessage, type WalletAction } from "./wallet-model";

export function InlineError({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
      <p>{message}</p>
    </div>
  );
}
export function WalletPasskeyActionModal({
  user,
  action,
  selectedWallet,
  definitions,
  onActionChange,
  onCopySecret,
  onCancel,
  onPasskeyCancel,
  onChallenge,
  onCompleted,
}: {
  user: User;
  action: WalletAction;
  selectedWallet?: SettingsWallet;
  definitions: PolicyDefinition[];
  onActionChange: (action: WalletAction) => void;
  onCopySecret: (value: string) => Promise<void>;
  onCancel: () => void;
  onPasskeyCancel: () => void;
  onChallenge: (action: WalletAction) => void;
  onCompleted: (action: WalletAction, result: unknown) => void;
}) {
  const { t } = useTranslation();
  const title = action.type === "delete"
    ? t("settings.deleteConfirmTitle")
    : action.type === "export"
      ? t("settings.exportConfirmTitle")
      : t("settings.policyBindingConfirmTitle");
  const body = action.type === "delete"
    ? t("settings.deleteConfirmBody", { wallet: action.walletId })
    : action.type === "export"
      ? t("settings.exportConfirmBody", { wallet: action.walletId })
      : t("settings.policyBindingConfirmBody", {
          wallet: action.walletId,
          policy: policyName(definitions, action.policyDefinitionId),
        });
  const details = [
    { label: t("common.wallet"), value: action.walletId },
    ...(selectedWallet ? [{ label: t("settings.walletAddress"), value: shortAddress(selectedWallet.address) }] : []),
    ...(action.type === "bind_policy" ? [{ label: t("settings.policyDefinition"), value: policyName(definitions, action.policyDefinitionId) }] : []),
  ];

  return (
    <PasskeyActionModal
      user={user}
      title={title}
      body={body}
      challenge={action.challenge}
      danger={action.type !== "bind_policy"}
      restoredApproved={action.restoredApproved}
      keepOpenOnSuccess={action.type === "export"}
      canRequest={action.type !== "delete" || action.confirmation === action.walletId}
      requestLabel={t("settings.requestPasskey")}
      details={details}
      childrenBeforePasskey={
        <>
          {action.type === "delete" && (
            <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
              {t("settings.typeWalletId")}
              <Input
                value={action.confirmation}
                onChange={(event) => onActionChange({ ...action, confirmation: event.target.value })}
                className="mt-2"
                placeholder={action.walletId}
              />
            </label>
          )}
          {action.error && <div className="mt-4"><InlineError message={action.error} /></div>}
        </>
      }
      childrenAfterSuccess={() => (
        action.type === "export" && action.privateKey ? (
          <div className="mt-5 rounded-md border border-border bg-background/80 p-3">
            <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{t("settings.privateKey")}</p>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate font-mono text-xs">{action.privateKey}</code>
              <Button variant="outline" size="icon" onClick={() => void onCopySecret(action.privateKey ?? "")}>
                <Copy className="h-4 w-4" />
              </Button>
            </div>
            <label className="mt-3 flex items-start gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={Boolean(action.saved)}
                onChange={(event) => onActionChange({ ...action, saved: event.target.checked })}
                className="mt-1 h-4 w-4 rounded border-input accent-primary"
              />
              <span>{t("settings.privateKeySavedConfirm")}</span>
            </label>
            {action.error && <div className="mt-3"><InlineError message={action.error} /></div>}
          </div>
        ) : null
      )}
      requestChallenge={action.challenge ? undefined : async () => {
        // Defensive guard: bind_policy challenges are prepared upstream
        // (`startBindPolicy` in WalletsPage sets `action.challenge` before
        // mounting this modal), so this `requestChallenge` fallback path
        // should never run for that action type. If it does, the upstream
        // wiring lost the challenge — fail fast rather than silently issuing
        // a delete/export challenge here. The throw is caught by
        // PasskeyActionModal and surfaced via `errorMessage` below.
        if (action.type === "bind_policy") throw new Error(t("settings.policyBindingError"));
        const challenge = action.type === "delete"
          ? await requestWalletDelete(user, action.walletId, action.confirmation)
          : await requestWalletExport(user, action.walletId);
        const next = { ...action, challenge, error: "", restoredApproved: false } as WalletAction;
        onActionChange(next);
        onChallenge(next);
        return challenge;
      }}
      confirmAction={(challengeId) => {
        if (action.type === "delete") return confirmWalletDelete(user, action.walletId, action.confirmation, challengeId);
        if (action.type === "export") return confirmWalletExport(user, action.walletId, challengeId);
        return confirmWalletPolicyBinding(user, action.walletId, action.policyDefinitionId, challengeId);
      }}
      errorMessage={(error) => {
        if (action.type === "delete") return walletDeleteErrorMessage(error, t);
        if (action.type === "export") return messageFrom(error, t("settings.walletExportError"));
        return messageFrom(error, t("settings.policyBindingError"));
      }}
      onSuccess={(result) => onCompleted(action, result)}
      onCancel={(reason) => {
        if (reason === "used" && action.type === "export" && action.privateKey && !action.saved) {
          onActionChange({ ...action, error: t("settings.privateKeySaveRequired") });
          return;
        }
        if (reason === "used") {
          onCancel();
          return;
        }
        onPasskeyCancel();
      }}
    />
  );
}
