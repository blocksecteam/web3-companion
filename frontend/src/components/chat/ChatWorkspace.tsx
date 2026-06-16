// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronDown,
  CircleDollarSign,
  Copy,
  ExternalLink,
  Pencil,
  CircleStop,
  Clock3,
  Loader2,
  LogOut,
  MessageSquarePlus,
  PanelLeftClose,
  PanelLeftOpen,
  ReceiptText,
  RefreshCw,
  ScrollText,
  Send,
  Settings,
  ShieldAlert,
  SlidersHorizontal,
  Sparkles,
  Trash2,
  WalletCards,
} from "lucide-react";
import { nanoid } from "nanoid";
import { useTranslation } from "react-i18next";
import { LanguageToggle } from "@/components/LanguageToggle";
import { GeneratedAvatar } from "@/components/ui/avatar";
import { MessageBubble } from "@/components/chat/MessageBubble";
import { applyBlockUpdate, applyPartialMessage, applyToolUpdate, upsertChatMessage } from "@/components/chat/message-state";
import { PasskeyActionModal } from "@/components/PasskeyActionModal";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Textarea } from "@/components/ui/textarea";
import {
  CompanionSocket,
  createSessionId,
  deleteSession,
  fetchSessionHistory,
  fetchSessionNames,
  fetchSessions,
  renameSession,
} from "@/lib/chat-client";
import { fetchLLMProvider, fetchSettingsWallets } from "@/lib/user-api";
import { fetchChains, fetchPortfolioSnapshot } from "@/lib/web3-api";
import { isViewportNearBottom, scrollToViewportBottom } from "@/lib/client-utils";
import { cn, shortAddress, shortId } from "@/lib/utils";
import type {
  ChatMessage,
  Page,
  PasskeyApprovalRequest,
  ServerWsEvent,
  SessionSummary,
  User,
} from "@/types/chat";

export function ChatWorkspace({
  user,
  activeSessionId,
  onSessionChange,
  onNavigate,
  onLogout,
}: {
  user: User;
  activeSessionId: string;
  onSessionChange: (sessionId: string) => void;
  onNavigate: (page: Page, sessionId?: string) => void;
  onLogout: () => void;
}) {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [localErrors, setLocalErrors] = useState<Array<{ id: string; message: string }>>([]);
  const [draft, setDraft] = useState("");
  const [runningSessionId, setRunningSessionId] = useState<string | null>(null);
  const [chatPasskeyRequest, setChatPasskeyRequest] = useState<PasskeyApprovalRequest | null>(null);
  const [modelKeyConfigured, setModelKeyConfigured] = useState<boolean | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(() =>
    typeof window === "undefined" ? true : window.matchMedia("(min-width: 1024px)").matches,
  );
  const socketRef = useRef<CompanionSocket | null>(null);
  const scrollViewportRef = useRef<HTMLDivElement | null>(null);
  const [pendingSynthetic, setPendingSynthetic] = useState<{ msg: string; session: string } | null>(null);

  const running = runningSessionId !== null;
  const activeRunning = runningSessionId === activeSessionId;
  const modelKeyMissing = modelKeyConfigured === false;

  const handleServerEvent = useCallback((event: ServerWsEvent) => {
    // Status events are global — they determine whether ANY session is
    // running, which controls input disabling across all sessions. Process
    // them before the per-session filter so a finish event for session A
    // received on session B's socket still clears runningSessionId.
    if (event.type === "status") {
      setRunningSessionId(event.status === "running" ? event.sessionId : null);
      setSessions((current) =>
        current.map((session) =>
          session.id === event.sessionId ? { ...session, status: event.status } : session,
        ),
      );
      return;
    }

    const eventSessionId =
      event.type === "session"
        ? event.session.id
        : "sessionId" in event
          ? event.sessionId
          : activeSessionId;
    if (eventSessionId && eventSessionId !== activeSessionId) return;

    if (event.type === "session") {
      if (event.session.messageCount > 0) {
        setSessions((current) => upsertSession(current, event.session));
      }
    }

    if (event.type === "message") {
      setMessages((current) => upsertChatMessage(current, event.message));
    }

    if (event.type === "partial") {
      setMessages((current) => applyPartialMessage(current, event));
    }

    if (event.type === "tool") {
      setMessages((current) => applyToolUpdate(current, event.messageId, event.tool));
    }

    if (event.type === "block_update") {
      setMessages((current) => applyBlockUpdate(current, event));
    }

    if (event.type === "error") {
      setLocalErrors((current) => [...current, { id: nanoid(), message: event.message }]);
    }
  }, [activeSessionId]);

  useEffect(() => {
    Promise.all([fetchSessions(user), fetchSessionNames(user).catch(() => ({} as Record<string, string>))])
      .then(([loaded, names]: [SessionSummary[], Record<string, string>]) => {
        const nonEmpty = loaded.filter((session) => session.messageCount > 0);
        const merged = nonEmpty.map((s) => names[s.id] ? { ...s, title: names[s.id] } : s);
        setSessions(merged);
      })
      .catch(() => {
        setSessions([]);
      });
  }, [user]);

  useEffect(() => {
    let stale = false;
    fetchLLMProvider(user)
      .then((provider) => {
        if (!stale) setModelKeyConfigured(provider.apiKeyConfigured);
      })
      .catch(() => {
        if (!stale) setModelKeyConfigured(null);
      });
    return () => {
      stale = true;
    };
  }, [user]);

  useEffect(() => {
    // Clear immediately so the old session's messages never flash in the
    // new session's view while fetchSessionHistory is in-flight.
    setMessages([]);
    setLocalErrors([]);

    socketRef.current?.close();
    const socket = new CompanionSocket(user, activeSessionId, handleServerEvent);
    socketRef.current = socket;
    socket.connect();

    let stale = false;
    fetchSessionHistory(user, activeSessionId)
      .then((loadedMessages) => {
        if (!stale) {
          setMessages(loadedMessages);
        }
      })
      .catch(() => {
        if (!stale) {
          setMessages([]);
        }
      });

    return () => {
      stale = true;
      socket.close();
    };
  }, [activeSessionId, handleServerEvent, user]);

  const stickToBottomRef = useRef(true);

  // Track whether the user is pinned to the bottom. Once they scroll up to
  // read earlier messages, suppress auto-scroll so streamed tokens don't yank
  // the viewport back down — that snap-back is what blocked scrolling up.
  useEffect(() => {
    const viewport = scrollViewportRef.current;
    if (!viewport) return;
    const onScroll = () => {
      stickToBottomRef.current = isViewportNearBottom(viewport);
    };
    viewport.addEventListener("scroll", onScroll, { passive: true });
    return () => viewport.removeEventListener("scroll", onScroll);
  }, []);

  // Switching sessions always jumps to the latest message and re-pins.
  useEffect(() => {
    stickToBottomRef.current = true;
    scrollToViewportBottom(scrollViewportRef.current);
  }, [activeSessionId]);

  // New content follows the stream only while the user is already at the
  // bottom; if they've scrolled up, leave their position alone.
  useEffect(() => {
    if (stickToBottomRef.current) {
      scrollToViewportBottom(scrollViewportRef.current);
    }
  }, [messages, localErrors]);

  const passkeyChallenge = useMemo(
    () => chatPasskeyRequest
      ? { status: "pending_user_auth" as const, challengeId: chatPasskeyRequest.challengeId, signUrl: chatPasskeyRequest.signUrl }
      : undefined,
    [chatPasskeyRequest],
  );

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId),
    [activeSessionId, sessions],
  );
  const createSession = () => {
    if (running) return;
    const id = createSessionId();
    setMessages([]);
    setLocalErrors([]);
    onSessionChange(id);
    if (window.innerWidth < 1024) setSidebarOpen(false);
  };

  const removeSession = async (sessionId: string) => {
    if (runningSessionId === sessionId) return;
    await deleteSession(user, sessionId).catch(() => undefined);
    setSessions((current) => current.filter((session) => session.id !== sessionId));
    if (sessionId === activeSessionId) {
      const next = sessions.find((session) => session.id !== sessionId)?.id ?? createSessionId();
      onSessionChange(next);
    }
  };

  const sendMessage = (content: string) => {
    const trimmed = content.trim();
    if (!trimmed || running || modelKeyMissing) return;
    const id = nanoid();
    setDraft("");
    socketRef.current?.send({
      type: "user_message",
      id,
      sessionId: activeSessionId,
      content: trimmed,
    });
  };

  const sendSyntheticUserMessage = (content: string) => {
    setPendingSynthetic({ msg: content, session: activeSessionId });
  };

  useEffect(() => {
    if (running || !pendingSynthetic) return;
    if (pendingSynthetic.session !== activeSessionId) {
      setPendingSynthetic(null);
      return;
    }
    const id = nanoid();
    socketRef.current?.send({
      type: "user_message",
      id,
      sessionId: pendingSynthetic.session,
      content: pendingSynthetic.msg,
    });
    setPendingSynthetic(null);
  }, [running, activeSessionId, pendingSynthetic]);

  const stopRun = () => {
    socketRef.current?.send({ type: "stop", sessionId: activeSessionId });
  };

  return (
    <main className="relative flex h-[100dvh] min-h-[640px] overflow-hidden">
      {sidebarOpen && (
        <button
          aria-label={t("chat.closeSidebar")}
          className="fixed inset-0 z-20 bg-foreground/15 backdrop-blur-[1px] lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-30 w-[min(20rem,calc(100vw-1.5rem))] border-r border-border bg-background/90 backdrop-blur-xl transition-transform duration-300 ease-out-quart lg:static lg:z-auto lg:block lg:w-80 lg:transition-[width]",
          sidebarOpen ? "translate-x-0" : "-translate-x-full lg:w-0 lg:translate-x-0 lg:border-r-0",
        )}
      >
        <div className={cn("flex h-full flex-col overflow-hidden", !sidebarOpen && "lg:invisible")}>
          <div className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-semibold">{t("chat.sessions")}</p>
              </div>
              <Button size="icon" variant="ghost" onClick={createSession} disabled={running}>
                <MessageSquarePlus className="h-4 w-4" />
              </Button>
            </div>
          </div>
          <div className="flex-1 overflow-y-auto px-3">
            <div className="space-y-2 pb-4">
              {sessions.filter((session) => session.messageCount > 0).map((session) => (
                <SessionItem
                  key={session.id}
                  session={session}
                  active={session.id === activeSessionId}
                  user={user}
                  onSelect={() => { onSessionChange(session.id); if (window.innerWidth < 1024) setSidebarOpen(false); }}
                  onDelete={() => void removeSession(session.id)}
                  onRename={(name) => setSessions((current) => current.map((s) => s.id === session.id ? { ...s, title: name } : s))}
                />
              ))}
            </div>
          </div>
          <div className="border-t border-border px-3 py-2">
            <SidebarWalletCard user={user} onNavigate={onNavigate} />
          </div>
          <div className="border-t border-border px-3 py-2">
            <SidebarNav onNavigate={(page) => { onNavigate(page); if (window.innerWidth < 1024) setSidebarOpen(false); }} />
          </div>
          <div className="border-t border-border p-3">
            <SidebarUser user={user} onLogout={onLogout} />
          </div>
        </div>
      </aside>

      <section className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-16 items-center justify-between border-b border-border bg-background/82 px-4 backdrop-blur-xl sm:px-6">
          <div className="flex items-center gap-3">
            <Button
              size="icon"
              variant="ghost"
              onClick={() => setSidebarOpen((value) => !value)}
            >
              {sidebarOpen ? <PanelLeftClose className="h-4 w-4" /> : <PanelLeftOpen className="h-4 w-4" />}
            </Button>
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold">
                {activeSession?.title ?? t("chat.newConversation")}
              </p>
              <p className="text-xs text-muted-foreground">
                {activeRunning ? t("chat.generating") : t("chat.ready")} · {shortId(activeSessionId)}
              </p>
            </div>
          </div>
          <LanguageToggle compact />
        </header>

        <ScrollArea className="flex-1" viewportRef={scrollViewportRef}>
          <div className="mx-auto flex w-full max-w-4xl flex-col px-4 py-8 pb-8 sm:px-6">
            {modelKeyMissing && (
              <ModelKeyWarning onConfigure={() => onNavigate("settings")} />
            )}
            {messages.length === 0 && localErrors.length === 0 ? (
              <EmptyChat onPrompt={sendMessage} disabled={running || modelKeyMissing} />
            ) : (
              <>
                {messages.map((message, index) => (
                  <div
                    key={message.id}
                    className={messageSpacing(message, messages[index - 1], index)}
                  >
                    <MessageBubble message={message} onPasskeyRequest={setChatPasskeyRequest} />
                  </div>
                ))}
                {localErrors.map((error) => (
                  <div
                    key={error.id}
                    className="mx-auto mt-5 max-w-xl rounded-full border border-destructive/15 bg-destructive/10 px-4 py-2 text-center text-sm text-destructive"
                  >
                    {error.message}
                  </div>
                ))}
              </>
            )}
          </div>
        </ScrollArea>

        <div
          className={cn(
            "safe-bottom border-t border-border bg-background/90 px-4 py-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] backdrop-blur-xl sm:px-6",
            activeRunning && "pt-0",
          )}
        >
          {activeRunning && <StreamingStatus label={t("chat.generating")} />}
          <form
            className="mx-auto flex max-w-4xl items-center gap-3"
            onSubmit={(event) => {
              event.preventDefault();
              sendMessage(draft);
            }}
          >
            <Textarea
              value={draft}
              disabled={running && !activeRunning}
              placeholder={
                modelKeyMissing
                  ? t("chat.modelKeyMissingBody")
                  : running && !activeRunning
                  ? t("chat.otherRunning")
                  : t("chat.placeholder")
              }
              className="min-h-[54px] flex-1 self-center"
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  sendMessage(draft);
                }
              }}
            />
            {activeRunning ? (
              <Button type="button" size="icon" variant="outline" className="self-center" onClick={stopRun}>
                <CircleStop className="h-5 w-5" />
              </Button>
            ) : (
              <Button type="submit" size="icon" variant="ink" className="self-center" disabled={!draft.trim() || running || modelKeyMissing}>
                <Send className="h-5 w-5" />
              </Button>
            )}
          </form>
        </div>
      </section>
      {chatPasskeyRequest && (
        <PasskeyActionModal
          user={user}
          challenge={passkeyChallenge}
          title={t("chat.passkeyConfirmTitle")}
          body={t("chat.passkeyConfirmBody")}
          confirmAction={async () => undefined}
          onSuccess={() => {
            setChatPasskeyRequest(null);
            sendSyntheticUserMessage(t("chat.passkeyAuthenticatedMessageWithChallenge", {
              challengeId: chatPasskeyRequest.challengeId,
            }));
          }}
          onCancel={() => setChatPasskeyRequest(null)}
        />
      )}
    </main>
  );
}

function StreamingStatus({ label }: { label: string }) {
  return (
    <div className="mx-auto my-2 flex max-w-4xl justify-center" role="status" aria-live="polite">
      <div className="streaming-status-pill inline-flex items-center gap-1.5 rounded-full border border-[hsl(154_12%_45%/0.18)] bg-background/55 px-2.5 py-1 text-xs font-medium text-muted-foreground backdrop-blur-sm">
        <span className="streaming-status-mark" aria-hidden="true" />
        <span>{label}</span>
        <span className="flex items-center gap-1" aria-hidden="true">
          <span className="streaming-dot" />
          <span className="streaming-dot [animation-delay:160ms]" />
          <span className="streaming-dot [animation-delay:320ms]" />
        </span>
      </div>
    </div>
  );
}

const chainExplorers: Record<string, string> = {
  ethereum: "https://etherscan.io/address/",
  base: "https://basescan.org/address/",
  bsc: "https://bscscan.com/address/",
  polygon: "https://polygonscan.com/address/",
};

function chainExplorerAddress(chain: string, address: string): string | null {
  const base = chainExplorers[chain.toLowerCase()];
  return base ? `${base}${address}` : null;
}

function SessionItem({ session, active, user, onSelect, onDelete, onRename }: { session: SessionSummary; active: boolean; user: User; onSelect: () => void; onDelete: () => void; onRename: (name: string) => void }) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing && inputRef.current) inputRef.current.focus();
  }, [editing]);

  const saveRename = () => {
    const name = draft.trim();
    setEditing(false);
    if (name && name !== session.title) {
      void renameSession(user, session.id, name);
      onRename(name);
    }
  };

  return (
    <div className={cn("group w-full overflow-hidden rounded-md border border-transparent px-3 py-3 text-left transition-colors hover:bg-secondary/70", active && "border-border bg-card shadow-line")}>
      <div className="flex min-w-0 items-center justify-between gap-2">
        {editing ? (
          <input
            ref={inputRef}
            className="min-w-0 flex-1 truncate rounded border border-border bg-background px-1 py-0.5 text-sm font-medium leading-5"
            value={draft}
            maxLength={200}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={saveRename}
            onKeyDown={(e) => { if (e.key === "Enter") saveRename(); if (e.key === "Escape") setEditing(false); }}
          />
        ) : (
          <button className="min-w-0 flex-1 truncate text-left text-sm font-medium leading-5" onClick={onSelect} onDoubleClick={() => { setDraft(session.title); setEditing(true); }}>
            {session.title}
          </button>
        )}
        <div className="flex shrink-0 items-center gap-0.5 opacity-0 group-hover:opacity-100">
          <button className="rounded-sm p-1 hover:bg-secondary" onClick={() => { setDraft(session.title); setEditing(true); }} aria-label={t("chat.renameSession")}>
            <Pencil className="h-3 w-3" />
          </button>
          <button className="rounded-sm p-1 hover:bg-destructive/10 hover:text-destructive" onClick={onDelete} aria-label={t("chat.deleteSession")}>
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
      <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
        <span>{shortId(session.id)}</span>
        <span className="inline-flex items-center gap-1">
          {session.status === "running" ? <Loader2 className="h-3 w-3 animate-spin" /> : <Clock3 className="h-3 w-3" />}
          {t("chat.messages", { count: session.messageCount })}
        </span>
      </div>
    </div>
  );
}

function SidebarWalletCard({ user, onNavigate }: { user: User; onNavigate: (page: Page) => void }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const [wallet, setWallet] = useState<{ address: string; walletId: string } | null>(null);
  const [balances, setBalances] = useState<Array<{ chain: string; symbol: string; balance: string; usd: string }>>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [balanceFailed, setBalanceFailed] = useState(false);

  const loadBalances = useCallback(async (walletAddress: string) => {
    const chains = await fetchChains();
    const results: typeof balances = [];
    for (const chain of chains) {
      try {
        const snapshot = await fetchPortfolioSnapshot(user, chain.name, walletAddress);
        if (snapshot.balances.length > 0) {
          const native = snapshot.balances.find((b) => b.symbol === chain.nativeSymbol);
          if (native) {
            results.push({ chain: chain.name, symbol: native.symbol, balance: native.balance.toFixed(6), usd: native.balanceUsd.toFixed(2) });
          }
        } else {
          results.push({ chain: chain.name, symbol: chain.nativeSymbol, balance: "0", usd: "0.00" });
        }
      } catch {
        results.push({ chain: chain.name, symbol: chain.nativeSymbol, balance: "?", usd: "?" });
      }
    }
    return results;
  }, [user]);

  useEffect(() => {
    let stale = false;
    (async () => {
      try {
        const wallets = await fetchSettingsWallets(user);
        const defaultWallet = wallets.items.find((w) => w.isDefault) ?? wallets.items[0];
        if (stale || !defaultWallet) { setLoading(false); return; }
        setWallet({ address: defaultWallet.address, walletId: defaultWallet.walletId });
        const results = await loadBalances(defaultWallet.address);
        if (!stale) {
          setBalances(results);
          setBalanceFailed(results.length > 0 && results.every((b) => b.balance === "?"));
        }
      } catch {
        if (!stale) setBalanceFailed(true);
      } finally {
        if (!stale) setLoading(false);
      }
    })();
    return () => { stale = true; };
  }, [user, loadBalances]);

  const handleRefresh = async () => {
    if (!wallet || refreshing) return;
    setRefreshing(true);
    try {
      const results = await loadBalances(wallet.address);
      setBalances(results);
      setBalanceFailed(results.length > 0 && results.every((b) => b.balance === "?"));
    } catch {
      setBalanceFailed(true);
    } finally {
      setRefreshing(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center gap-2 px-2.5 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        {t("common.loading")}
      </div>
    );
  }

  if (!wallet) return null;

  const totalUsd = balances.reduce((sum, b) => sum + (parseFloat(b.usd) || 0), 0);

  return (
    <div className="rounded-md border border-border bg-card/50 px-2.5 py-2">
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left"
        onClick={() => setExpanded((v) => !v)}
      >
        <WalletCards className="h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1">
            <span className="truncate font-mono text-xs">{shortAddress(wallet.address)}</span>
            <button
              type="button"
              className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
              onClick={(e) => { e.stopPropagation(); void navigator.clipboard.writeText(wallet.address); }}
              aria-label={t("common.copy")}
            >
              <Copy className="h-3 w-3" />
            </button>
            <button
              type="button"
              className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
              onClick={(e) => { e.stopPropagation(); void handleRefresh(); }}
              aria-label={t("common.refresh")}
            >
              <RefreshCw className={cn("h-3 w-3", refreshing && "animate-spin")} />
            </button>
          </div>
          <p className="text-[10px] text-muted-foreground">
            {balanceFailed ? (
              <button
                type="button"
                className="underline decoration-dotted hover:text-foreground"
                onClick={(e) => { e.stopPropagation(); onNavigate("settings"); }}
              >
                {t("chat.walletBalanceUnavailable")}
              </button>
            ) : `$${totalUsd.toFixed(2)} · ${balances.length} ${t("common.chains")}`}
          </p>
        </div>
        <ChevronDown className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform", expanded && "rotate-180")} />
      </button>
      {expanded && (
        <div className="mt-2 space-y-1 border-t border-border pt-2">
          {balances.map((b) => {
            const explorerUrl = chainExplorerAddress(b.chain, wallet.address);
            return (
              <div key={b.chain} className="flex items-center justify-between text-[11px]">
                <span className="flex items-center gap-1 capitalize text-muted-foreground">
                  {b.chain}
                  {explorerUrl && (
                    <a href={explorerUrl} target="_blank" rel="noreferrer" className="hover:text-foreground">
                      <ExternalLink className="h-2.5 w-2.5" />
                    </a>
                  )}
                </span>
                <span className="font-mono">{b.balance} {b.symbol}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function SidebarNav({ onNavigate }: { onNavigate: (page: Page) => void }) {
  const { t } = useTranslation();
  const items: { page: Page; label: string; icon: typeof CircleDollarSign }[] = [
    { page: "portfolio", label: t("nav.portfolio"), icon: CircleDollarSign },
    { page: "transactions", label: t("nav.transactions"), icon: ReceiptText },
    { page: "wallets", label: t("nav.wallets"), icon: WalletCards },
  ];
  const mgmt: { page: Page; label: string; icon: typeof Settings }[] = [
    { page: "settings", label: t("nav.settings"), icon: Settings },
    { page: "policies", label: t("nav.policies"), icon: SlidersHorizontal },
    { page: "audit", label: t("nav.audit"), icon: ScrollText },
  ];
  return (
    <nav className="space-y-1">
      {items.map((item) => {
        const Icon = item.icon;
        return (
          <button key={item.page} type="button" className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-secondary/70 hover:text-foreground" onClick={() => onNavigate(item.page)}>
            <Icon className="h-4 w-4" />
            {item.label}
          </button>
        );
      })}
      <div className="my-1 border-t border-border" />
      {mgmt.map((item) => {
        const Icon = item.icon;
        return (
          <button key={item.page} type="button" className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-secondary/70 hover:text-foreground" onClick={() => onNavigate(item.page)}>
            <Icon className="h-4 w-4" />
            {item.label}
          </button>
        );
      })}
    </nav>
  );
}

function SidebarUser({ user, onLogout }: { user: User; onLogout: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-3">
      <GeneratedAvatar email={user.email} className="h-8 w-8" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-xs font-medium">{user.email}</p>
      </div>
      <button type="button" className="rounded-md p-1.5 text-muted-foreground hover:bg-secondary/70 hover:text-foreground" onClick={onLogout} aria-label={t("nav.logout")}>
        <LogOut className="h-4 w-4" />
      </button>
    </div>
  );
}

function ModelKeyWarning({ onConfigure }: { onConfigure: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="mb-5 rounded-md border border-destructive/20 bg-destructive/10 p-4 text-destructive">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <div>
            <p className="text-sm font-semibold">{t("chat.modelKeyMissingTitle")}</p>
            <p className="mt-1 text-sm leading-6 text-destructive/85">{t("chat.modelKeyMissingBody")}</p>
          </div>
        </div>
        <Button type="button" variant="outline" size="sm" className="shrink-0 border-destructive/25 text-destructive hover:bg-destructive/10" onClick={onConfigure}>
          <Settings className="h-4 w-4" />
          {t("chat.configureModel")}
        </Button>
      </div>
    </div>
  );
}

function EmptyChat({ onPrompt, disabled }: { onPrompt: (prompt: string) => void; disabled: boolean }) {
  const { t } = useTranslation();
  const suggestedPrompts = t("chat.suggestedPrompts", { returnObjects: true }) as string[];

  return (
    <div className="py-10">
      <div className="max-w-2xl">
        <p className="inline-flex items-center gap-2 rounded-full border border-foreground/10 bg-card px-3 py-1 text-sm text-muted-foreground">
          <Sparkles className="h-4 w-4 text-[hsl(25_48%_43%)]" />
          {t("chat.emptyBadge")}
        </p>
        <h1 className="mt-6 font-display text-5xl font-medium leading-tight text-balance">
          {t("chat.emptyTitle")}
        </h1>
        <p className="mt-5 max-w-xl leading-7 text-muted-foreground">
          {t("chat.emptyBody")}
        </p>
      </div>
      <div className="mt-8 grid gap-3 md:grid-cols-2">
        {suggestedPrompts.map((prompt) => (
          <button
            key={prompt}
            disabled={disabled}
            className="rounded-lg border border-border bg-card p-4 text-left leading-6 shadow-line transition-colors hover:bg-secondary/80 disabled:opacity-50"
            onClick={() => onPrompt(prompt)}
          >
            {prompt}
          </button>
        ))}
      </div>
    </div>
  );
}

function upsertSession(sessions: SessionSummary[], next: SessionSummary) {
  const existing = sessions.find((session) => session.id === next.id);
  if (existing) {
    // Preserve the user's locally-known title: user-set names (from the Go
    // backend) are merged into sessions at mount; the companion store only
    // has auto-generated titles. Replacing wholesale would revert renames.
    const merged = { ...next, title: existing.title || next.title };
    return sessions.map((session) => (session.id === next.id ? merged : session))
      .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime());
  }
  return [next, ...sessions].sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime());
}

function messageSpacing(message: ChatMessage, previous: ChatMessage | undefined, index: number) {
  if (index === 0) return "";
  if (isThinkingOnly(message) || (previous && isThinkingOnly(previous))) return "mt-2";
  return "mt-5";
}

function isThinkingOnly(message: ChatMessage) {
  return (
    message.role === "assistant" &&
    !message.content.trim() &&
    Boolean(message.thinking?.trim()) &&
    (message.tools?.length ?? 0) === 0
  );
}
