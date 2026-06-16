// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { Suspense, lazy } from "react";
import {
  ArrowUpRight,
  Braces,
  ChevronsUpDown,
  FileText,
  Fingerprint,
  KeyRound,
  Layers3,
  Network,
  Search,
  ShieldCheck,
  ShieldAlert,
  SquareActivity,
  SquareCode,
  TerminalSquare,
  WandSparkles,
  WalletCards,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  passkeyRequestsForMessage,
  type ChatPasskeyRequest,
} from "@/lib/chat-passkey-requests";
import { commandSubject, web3CommandFromTool, type Web3ToolView } from "@/lib/web3-command";
import { chainDisplayName } from "@/lib/chain-display";
import { skillCardFromTool, skillTranslationKey, toolCardKind, type SkillCardView } from "@/lib/tool-card";
import { cn } from "@/lib/utils";
import type { ChatMessage, PasskeyApprovalRequest, SecurityReviewBlock, ToolCallKind, ToolCallView } from "@/types/chat";

const StreamdownRenderer = lazy(() =>
  import("@/components/chat/StreamdownRenderer").then((module) => ({ default: module.StreamdownRenderer })),
);

const toolIconByKind: Record<ToolCallKind, typeof WalletCards> = {
  portfolio: WalletCards,
  swap: ArrowUpRight,
  bridge: Layers3,
  security: ShieldCheck,
  wallet: KeyRound,
  chain: SquareActivity,
  token: Search,
  web3: TerminalSquare,
  skill: WandSparkles,
  command: TerminalSquare,
  file: FileText,
  search: Search,
  tool: Braces,
};

const toolLabelKeyByKind: Record<ToolCallKind, string> = {
  portfolio: "web3.domains.portfolio",
  swap: "web3.domains.swap",
  bridge: "web3.domains.bridge",
  security: "web3.domains.security",
  wallet: "web3.domains.wallet",
  chain: "web3.domains.chain",
  token: "web3.domains.token",
  web3: "tool.web3",
  skill: "tool.skill",
  command: "tool.command",
  file: "tool.file",
  search: "tool.search",
  tool: "tool.generic",
};

export function MessageBubble({
  message,
  onPasskeyRequest,
}: {
  message: ChatMessage;
  onPasskeyRequest?: (request: PasskeyApprovalRequest) => void;
}) {
  const isUser = message.role === "user";
  const isSystem = message.role === "system";
  const { displayContent, requests: passkeyRequests } = passkeyRequestsForMessage(message);
  const hasContent = displayContent.trim().length > 0;
  const hasThinking = Boolean(message.thinking?.trim());
  const tools = message.tools ?? [];
  const blocks = message.blocks ?? [];
  const hasTools = tools.length > 0;

  if (isSystem) {
    return (
      <div className="mx-auto max-w-xl rounded-full border border-destructive/15 bg-destructive/10 px-4 py-2 text-center text-sm text-destructive">
        {message.content}
      </div>
    );
  }

  if (isUser) {
    return (
      <article className="flex justify-end">
        <div className="max-w-[min(760px,calc(100vw-2rem))] rounded-lg bg-foreground px-4 py-3 text-left leading-7 text-background shadow-line">
          <p className="whitespace-pre-wrap">{message.content}</p>
        </div>
      </article>
    );
  }

  return (
    <article className="flex justify-start">
      <div className="w-full max-w-[min(860px,calc(100vw-2rem))] text-left">
        {hasThinking && <ThinkingBlock text={message.thinking ?? ""} partial={message.partial} />}
        {hasContent && (
          <div className="assistant-markdown max-w-none text-[15px] leading-7 text-foreground sm:text-base">
            <Suspense fallback={<p className="whitespace-pre-wrap">{displayContent}</p>}>
              <StreamdownRenderer content={displayContent} partial={message.partial} />
            </Suspense>
          </div>
        )}
        {blocks.length > 0 && (
          <div className={cn("grid gap-2", hasContent ? "mt-3" : "mt-0")}>
            {blocks.map((block) => (
              block.type === "security_review" ? <SecurityReviewCard key={block.id} block={block} /> : null
            ))}
          </div>
        )}
        {passkeyRequests.length > 0 && (
          <div className={cn("grid gap-2 sm:max-w-md", hasContent ? "mt-3" : "mt-0")}>
            {passkeyRequests.map((request) => (
              <PasskeyConfirmButton
                key={request.challengeId}
                request={request}
                onPasskeyRequest={onPasskeyRequest}
              />
            ))}
          </div>
        )}
        {hasTools && (
          <div className={cn("grid gap-2", hasContent || passkeyRequests.length > 0 ? "mt-3" : hasThinking ? "mt-1" : "mt-0")}>
            {tools.map((tool) => (
              <ToolCallCard key={tool.id} tool={tool} />
            ))}
          </div>
        )}
      </div>
    </article>
  );
}

function SecurityReviewCard({ block }: { block: SecurityReviewBlock }) {
  const isReviewing = block.status === "reviewing";
  const isSafe = block.status === "safe";
  const Icon = isSafe ? ShieldCheck : ShieldAlert;
  const tone = isReviewing
    ? "border-[hsl(154_20%_35%/0.24)] bg-[hsl(88_30%_96%)]"
    : isSafe
      ? "border-[hsl(154_20%_35%/0.28)] bg-[hsl(88_30%_96%)]"
      : "border-destructive/20 bg-destructive/10";
  const iconTone = isReviewing
    ? "bg-[hsl(154_20%_25%)] text-background"
    : isSafe
      ? "bg-[hsl(154_20%_25%)] text-background"
      : "bg-destructive text-destructive-foreground";

  return (
    <section className={cn("rounded-md border p-3 text-left shadow-line", tone)}>
      <div className="flex items-start gap-3">
        <span className={cn("grid h-9 w-9 shrink-0 place-items-center rounded-md", iconTone)}>
          {isReviewing ? <span className="streaming-status-mark" aria-hidden="true" /> : <Icon className="h-4 w-4" />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm font-semibold text-foreground">Web3 Security Agent Review</p>
            <span className="rounded-full bg-background/80 px-2 py-0.5 text-[11px] font-semibold text-muted-foreground">
              {block.status}
            </span>
          </div>
          {block.transactionId && (
            <p className="mt-1 break-all font-mono text-xs text-muted-foreground">{block.transactionId}</p>
          )}
          {isReviewing ? (
            <div className="mt-3 flex items-center gap-2 text-sm text-muted-foreground" role="status" aria-live="polite">
              <span>{block.markdown}</span>
              <span className="flex items-center gap-1" aria-hidden="true">
                <span className="streaming-dot" />
                <span className="streaming-dot [animation-delay:160ms]" />
                <span className="streaming-dot [animation-delay:320ms]" />
              </span>
            </div>
          ) : (
            <div className="assistant-markdown mt-3 max-w-none text-sm leading-6 text-foreground">
              <Suspense fallback={<p className="whitespace-pre-wrap">{block.markdown}</p>}>
                <StreamdownRenderer content={block.markdown} />
              </Suspense>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

function PasskeyConfirmButton({
  request,
  onPasskeyRequest,
}: {
  request: ChatPasskeyRequest;
  onPasskeyRequest?: (request: PasskeyApprovalRequest) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="rounded-md border border-[hsl(154_20%_35%/0.28)] bg-[hsl(88_30%_96%)] p-3 text-left shadow-line">
      <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
        {t("chat.passkeyConfirmEyebrow")}
      </p>
      <p className="mt-1 text-sm leading-6 text-foreground">{t("chat.passkeyConfirmInlineBody")}</p>
      <Button
        className="mt-3"
        variant="ink"
        onClick={() =>
          onPasskeyRequest?.({
            challengeId: request.challengeId,
            signUrl: request.signUrl,
          })
        }
      >
        <Fingerprint className="h-4 w-4" />
        {t("chat.passkeyConfirmAction")}
      </Button>
    </div>
  );
}

function ThinkingBlock({ text, partial }: { text: string; partial?: boolean }) {
  const { t } = useTranslation();
  return (
    <details className="mb-1 text-left text-sm text-muted-foreground" open={false}>
      <summary className="inline-flex cursor-pointer select-none items-center gap-2 rounded-sm px-1 py-1 transition-colors hover:bg-muted/70">
        <ChevronsUpDown className="h-3.5 w-3.5" />
        <span className="font-medium">{t("chat.thinking")}{partial ? "..." : ""}</span>
      </summary>
      <div className="mt-2 max-h-48 overflow-y-auto border-l border-border pl-3 text-[13px] leading-6">
        <p className="whitespace-pre-wrap break-words">{text}</p>
      </div>
    </details>
  );
}

function ToolCallCard({ tool }: { tool: ToolCallView }) {
  const { t } = useTranslation();
  const cardKind = toolCardKind(tool);
  if (cardKind === "web3-cli") {
    const web3 = web3CommandFromTool(tool);
    if (web3) return <Web3ToolCard web3={web3} />;
  }
  if (cardKind === "web3-skill") {
    const skill = skillCardFromTool(tool);
    if (skill) return <Web3SkillCard tool={tool} skill={skill} />;
  }

  const effectiveKind = inferToolKind(tool);
  const Icon = toolIconByKind[effectiveKind] ?? Braces;
  const title = toolTitle(tool, t);
  const description = describeTool(tool, t);

  return (
    <details className="group rounded-md border border-border/80 bg-background/68 text-left shadow-line">
      <summary className="flex cursor-pointer list-none items-center gap-3 px-3 py-2.5">
        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-secondary text-secondary-foreground">
          <Icon className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold text-foreground">{title}</span>
          {description && (
            <span className="block truncate text-xs leading-5 text-muted-foreground">{description}</span>
          )}
        </span>
      </summary>
      {tool.input !== undefined && (
        <pre className="mx-3 mb-3 max-h-36 overflow-auto rounded-md bg-muted/60 px-3 py-2 font-mono text-[11px] leading-5 text-muted-foreground">
          {JSON.stringify(tool.input, null, 2)}
        </pre>
      )}
    </details>
  );
}

function Web3ToolCard({ web3 }: { web3: Web3ToolView }) {
  const { t } = useTranslation();
  const Icon = Network;
  const subject = web3Summary(web3, t);
  const safetyTone =
    web3.safety === "sensitive"
      ? "bg-destructive/10 text-destructive"
      : web3.safety === "prepare"
        ? "bg-[hsl(24_42%_73%)] text-[hsl(24_35%_20%)]"
        : "bg-accent text-accent-foreground";

  return (
    <details className="group rounded-md border border-border/80 bg-[hsl(39_44%_97%)] text-left shadow-line">
      <summary className="flex cursor-pointer list-none items-center gap-3 px-3 py-3">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[hsl(154_20%_25%)] text-background">
          <Icon className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-foreground">
              {t(`web3.domains.${web3.domain}`)} · {t(`web3.actions.${web3.action}`, { defaultValue: web3.action })}
            </span>
            <span className={cn("rounded-full px-2 py-0.5 text-[11px] font-semibold", safetyTone)}>
              {web3.safety === "read"
                ? t("tool.readOnly")
                : web3.safety === "prepare"
                  ? t("tool.prepare")
                  : t("tool.sensitive")}
            </span>
          </span>
          <span className="mt-1 block truncate text-xs leading-5 text-muted-foreground">
            {subject || t("tool.interactWithWeb3")}
          </span>
        </span>
      </summary>
      <div className="mx-3 mb-3 grid gap-2">
        <div className="rounded-md border border-border bg-background/70 px-3 py-2">
          <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{t("tool.command")}</p>
          <p className="mt-1 break-words font-mono text-xs leading-5">{web3.command}</p>
        </div>
        {commandSubject(web3) && (
          <div className="grid gap-1 rounded-md bg-secondary/65 px-3 py-2 text-xs sm:grid-cols-[7rem_1fr]">
            <span className="font-semibold text-muted-foreground">{t("tool.subject")}</span>
            <span className="break-all font-mono">{commandSubject(web3)}</span>
          </div>
        )}
      </div>
    </details>
  );
}

function Web3SkillCard({ tool, skill }: { tool: ToolCallView; skill: SkillCardView }) {
  const { t } = useTranslation();
  const translationKey = skillTranslationKey(skill);
  const title = translationKey ? t(`tool.skills.${translationKey}.title`) : skill.name;
  const description = translationKey
    ? t(`tool.skills.${translationKey}.description`)
    : skill.description || skillPathLabel(skill.path) || t("tool.web3SkillDescription");

  return (
    <details className="group rounded-md border border-[hsl(154_20%_35%/0.28)] bg-[hsl(88_30%_96%)] text-left shadow-line">
      <summary className="flex cursor-pointer list-none items-center gap-3 px-3 py-3">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[hsl(154_20%_25%)] text-background">
          <SquareCode className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-foreground">{title}</span>
            <span className="rounded-full bg-background/80 px-2 py-0.5 text-[11px] font-semibold text-[hsl(154_20%_25%)]">
              {t("tool.web3Skill")}
            </span>
          </span>
          <span className="mt-1 block truncate text-xs leading-5 text-muted-foreground">{description}</span>
        </span>
      </summary>
      <div className="mx-3 mb-3 grid gap-2">
        {skill.path && (
          <div className="rounded-md border border-[hsl(154_20%_35%/0.18)] bg-background/72 px-3 py-2">
            <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{t("tool.skillFile")}</p>
            <p className="mt-1 break-all font-mono text-xs leading-5">{skill.path}</p>
          </div>
        )}
        {tool.input !== undefined && (
          <pre className="max-h-36 overflow-auto rounded-md bg-background/72 px-3 py-2 font-mono text-[11px] leading-5 text-muted-foreground">
            {JSON.stringify(tool.input, null, 2)}
          </pre>
        )}
      </div>
    </details>
  );
}

function toolTitle(tool: ToolCallView, t: (key: string, options?: Record<string, unknown>) => string) {
  const known = prettifyToolName(tool.name);
  if (known && known.toLowerCase() !== "tool") return known;
  return t(toolLabelKeyByKind[tool.kind] ?? "tool.generic");
}

function describeTool(tool: ToolCallView, t: (key: string, options?: Record<string, unknown>) => string) {
  const input = asRecord(tool.input);
  const name = prettifyToolName(tool.name);
  const lowerName = tool.name.toLowerCase();
  const kind = inferToolKind(tool);

  if (kind === "skill") {
    const skill = textFrom(input.skill);
    const args = textFrom(input.args);
    if (skill && args) return `${skill} · ${args}`;
    return skill || args || t("tool.loadedCapability");
  }
  if (kind === "command") {
    return textFrom(input.description) || commandSummary(textFrom(input.command), t) || t("tool.ranCommand");
  }
  if (kind === "file") {
    return readPathSummary(input) || t("tool.readContext");
  }
  if (kind === "search") {
    return searchSummary(input, lowerName) || t("tool.searchedContext");
  }
  if (kind === "portfolio") return textFrom(input.address) || textFrom(input.wallet) || t("tool.checkedPortfolio");
  if (kind === "swap") return textFrom(input.token) || textFrom(input.route) || t("tool.preparedQuote");
  if (kind === "bridge") return textFrom(input.route) || t("tool.preparedBridge");
  if (kind === "security") return textFrom(input.description) || t("tool.safetyCheck");
  if (kind === "wallet") return textFrom(input.email) || textFrom(input.address) || t("tool.checkedWallet");
  if (kind === "chain") return textFrom(input.chain) || t("tool.checkedChain");
  if (kind === "token") return textFrom(input.symbol) || textFrom(input.token) || t("tool.checkedToken");
  return name ? t("tool.usedTool") : t("tool.generic");
}

function inferToolKind(tool: ToolCallView): ToolCallKind {
  const lower = tool.name.toLowerCase();
  if (lower.includes("skill")) return "skill";
  if (lower.includes("bash")) return "command";
  if (lower.includes("read") || lower.includes("write") || lower.includes("edit")) return "file";
  if (lower.includes("grep") || lower.includes("glob") || lower.includes("search") || lower.includes("ls")) return "search";
  return tool.kind;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function textFrom(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function readPathSummary(input: Record<string, unknown>) {
  const file = textFrom(input.file_path) || textFrom(input.path);
  if (!file) return "";
  const parts = file.split(/[\\/]/).filter(Boolean);
  return parts.slice(-3).join("/");
}

function searchSummary(input: Record<string, unknown>, lowerName: string) {
  const pattern = textFrom(input.pattern) || textFrom(input.query);
  const path = textFrom(input.path);
  if (pattern && path) return `${pattern} · ${basename(path)}`;
  if (pattern) return lowerName.includes("glob") ? `Pattern ${pattern}` : `Search ${pattern}`;
  return path ? basename(path) : "";
}

function commandSummary(command: string, t: (key: string, options?: Record<string, unknown>) => string) {
  if (!command) return "";
  const compact = command.replace(/\s+/g, " ").trim();
  if (compact.includes(" context status")) return t("tool.checkedWallet");
  if (compact.includes(" portfolio")) return t("tool.checkedPortfolio");
  if (compact.includes(" quote") || compact.includes(" swap")) return t("tool.preparedQuote");
  if (compact.includes(" bridge")) return t("tool.preparedBridge");
  return compact.length > 72 ? `${compact.slice(0, 72)}...` : compact;
}

function web3Summary(web3: Web3ToolView, t: (key: string, options?: Record<string, unknown>) => string) {
  const chain = web3.chain ? chainDisplayName(web3.chain, t) : "";
  if (web3.domain === "portfolio" && web3.chain && web3.address) {
    return t("web3.summaries.portfolio", { chain, address: web3.address });
  }
  if (web3.walletId) return t("web3.summaries.wallet", { wallet: web3.walletId });
  if (web3.hash) return t("web3.summaries.hash", { hash: web3.hash });
  if (web3.amount) return t("web3.summaries.amount", { amount: web3.amount });
  if (web3.protocol) return t("web3.summaries.protocol", { protocol: web3.protocol });
  if (chain) return t("web3.summaries.chain", { chain });
  return "";
}

function prettifyToolName(name: string) {
  const cleaned = name.replace(/[_-]+/g, " ").trim();
  return cleaned ? cleaned.replace(/\b\w/g, (char) => char.toUpperCase()) : "";
}

function basename(path: string) {
  return path.split(/[\\/]/).filter(Boolean).at(-1) ?? path;
}

function skillPathLabel(path: string) {
  if (!path) return "";
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.slice(-3).join("/");
}
