// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { ChatMessage, ClientWsEvent, ServerWsEvent, SessionSummary } from "@/types/chat";
import i18n from "@/i18n";
export { createSessionId } from "@/lib/client-utils";
import { websocketURL } from "@/lib/api-base";
import {
  fetchWithAuth,
  refreshAccessToken,
  shouldRefreshBeforeUse,
  shouldRefreshMissingAccess,
} from "@/lib/auth-session";
import type { User } from "@/types/chat";

export async function fetchSessions(user: User): Promise<SessionSummary[]> {
  const response = await fetchWithAuth(user, "/api/companion/sessions");
  if (!response.ok) {
    throw new Error(i18n.t("errors.loadSessions"));
  }
  return response.json() as Promise<SessionSummary[]>;
}

export async function fetchSessionHistory(user: User, sessionId: string): Promise<ChatMessage[]> {
  const response = await fetchWithAuth(user, `/api/companion/sessions/${encodeURIComponent(sessionId)}/messages`);
  if (!response.ok) {
    throw new Error(i18n.t("errors.loadHistory"));
  }
  return response.json() as Promise<ChatMessage[]>;
}

export async function deleteSession(user: User, sessionId: string) {
  const response = await fetchWithAuth(user, `/api/companion/sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(i18n.t("errors.deleteSession"));
  }
}

export async function renameSession(user: User, sessionId: string, name: string) {
  const response = await fetchWithAuth(user, `/api/companion/sessions/${encodeURIComponent(sessionId)}/name`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    throw new Error(i18n.t("errors.requestFailed"));
  }
}

export async function fetchSessionNames(user: User): Promise<Record<string, string>> {
  const response = await fetchWithAuth(user, "/api/companion/session-names");
  if (!response.ok) return {};
  const data = (await response.json()) as { names?: Record<string, string> };
  return data.names ?? {};
}

export class CompanionSocket {
  private socket: WebSocket | null = null;
  private pendingEvents: ClientWsEvent[] = [];
  private readonly sessionId: string;
  private readonly user: User;
  private readonly handleEvent: (event: ServerWsEvent) => void;
  private manuallyClosed = false;
  private refreshingBeforeConnect = false;
  private openingTicket = false;

  constructor(user: User, sessionId: string, handleEvent: (event: ServerWsEvent) => void) {
    this.user = user;
    this.sessionId = sessionId;
    this.handleEvent = handleEvent;
  }

  connect() {
    if (this.manuallyClosed) return;
    if (shouldRefreshBeforeUse(this.user) || shouldRefreshMissingAccess(this.user)) {
      chatSocketLog("info", "refresh_before_connect", {
        sessionId: this.sessionId,
        reason: shouldRefreshMissingAccess(this.user) ? "missing_access" : "expiring_or_expired",
      });
      this.refreshingBeforeConnect = true;
      void refreshAccessToken(this.user)
        .then(() => {
          this.refreshingBeforeConnect = false;
          chatSocketLog("info", "refresh_before_connect_success", {
            sessionId: this.sessionId,
          });
          this.openSocket();
        })
        .catch((error: unknown) => {
          this.refreshingBeforeConnect = false;
          if (this.manuallyClosed) return;
          this.pendingEvents = [];
          chatSocketLog("error", "refresh_before_connect_failed", {
            sessionId: this.sessionId,
            error: errorMessage(error),
          });
          this.handleEvent({
            type: "error",
            sessionId: this.sessionId,
            message: error instanceof Error ? error.message : i18n.t("errors.socketDisconnected"),
          });
        });
      return;
    }
    this.openSocket();
  }

  private openSocket() {
    if (this.manuallyClosed) return;
    this.openingTicket = true;
    void createWebSocketTicket(this.user, this.sessionId)
      .then((ticket) => {
        this.openingTicket = false;
        this.openSocketWithTicket(ticket);
      })
      .catch((error: unknown) => {
        this.openingTicket = false;
        if (this.manuallyClosed) return;
        this.pendingEvents = [];
        chatSocketLog("error", "ticket_unavailable", {
          sessionId: this.sessionId,
          error: errorMessage(error),
        });
        this.handleEvent({
          type: "error",
          sessionId: this.sessionId,
          message: error instanceof Error ? error.message : i18n.t("errors.socketDisconnected"),
        });
      });
  }

  private openSocketWithTicket(ticket: string) {
    if (this.manuallyClosed) return;
    const authProtocols = webSocketAuthProtocols(ticket);
    const url = websocketURL(`/ws/chat?session=${encodeURIComponent(this.sessionId)}`);
    const startedAt = performance.now();
    chatSocketLog("info", "open_requested", {
      sessionId: this.sessionId,
      url,
      authKind: "ticket",
      pendingEvents: this.pendingEvents.length,
      readyState: this.socket?.readyState,
    });
    this.socket = new WebSocket(url, authProtocols);

    this.socket.addEventListener("open", () => {
      chatSocketLog("info", "open", {
        sessionId: this.sessionId,
        durationMs: elapsedMs(startedAt),
        protocol: this.socket?.protocol || undefined,
        pendingEvents: this.pendingEvents.length,
      });
      this.flushPendingEvents();
    });

    this.socket.addEventListener("message", (event) => {
      if (this.manuallyClosed) return;
      try {
        const serverEvent = JSON.parse(event.data) as ServerWsEvent;
        chatSocketLog("debug", "message", {
          sessionId: this.sessionId,
          serverEventType: serverEvent.type,
          payloadBytes: payloadBytes(event.data),
        });
        this.handleEvent(serverEvent);
      } catch {
        chatSocketLog("warn", "message_unreadable", {
          sessionId: this.sessionId,
          payloadBytes: payloadBytes(event.data),
        });
        this.handleEvent({
          type: "error",
          sessionId: this.sessionId,
          message: i18n.t("errors.unreadableMessage"),
        });
      }
    });

    this.socket.addEventListener("close", (event) => {
      if (this.manuallyClosed) return;
      this.pendingEvents = [];
      chatSocketLog("warn", "close", {
        sessionId: this.sessionId,
        durationMs: elapsedMs(startedAt),
        code: event.code,
        reason: event.reason || undefined,
        wasClean: event.wasClean,
      });
      this.handleEvent({
        type: "error",
        sessionId: this.sessionId,
        message: i18n.t("errors.socketClosed"),
      });
      this.handleEvent({
        type: "status",
        sessionId: this.sessionId,
        status: "idle",
      });
    });

    this.socket.addEventListener("error", (event) => {
      if (this.manuallyClosed) return;
      this.pendingEvents = [];
      chatSocketLog("error", "error", {
        sessionId: this.sessionId,
        durationMs: elapsedMs(startedAt),
        eventType: event.type,
        readyState: this.socket?.readyState,
      });
      this.handleEvent({
        type: "error",
        sessionId: this.sessionId,
        message: i18n.t("errors.socketError"),
      });
    });
  }

  send(event: ClientWsEvent) {
    if (this.socket?.readyState === WebSocket.OPEN) {
      chatSocketLog("debug", "send", {
        sessionId: this.sessionId,
        clientEventType: event.type,
      });
      this.socket.send(JSON.stringify(event));
      return;
    }
    if (this.socket?.readyState === WebSocket.CONNECTING) {
      this.pendingEvents.push(event);
      chatSocketLog("info", "send_queued_connecting", {
        sessionId: this.sessionId,
        clientEventType: event.type,
        pendingEvents: this.pendingEvents.length,
      });
      return;
    }
    if (this.refreshingBeforeConnect) {
      this.pendingEvents.push(event);
      chatSocketLog("info", "send_queued_refreshing", {
        sessionId: this.sessionId,
        clientEventType: event.type,
        pendingEvents: this.pendingEvents.length,
      });
      return;
    }
    if (this.openingTicket) {
      this.pendingEvents.push(event);
      chatSocketLog("info", "send_queued_ticket", {
        sessionId: this.sessionId,
        clientEventType: event.type,
        pendingEvents: this.pendingEvents.length,
      });
      return;
    }
    chatSocketLog("warn", "send_disconnected", {
      sessionId: this.sessionId,
      clientEventType: event.type,
      readyState: this.socket?.readyState,
    });
    this.handleEvent({
      type: "error",
      sessionId: this.sessionId,
      message: i18n.t("errors.socketDisconnected"),
    });
  }

  close() {
    this.manuallyClosed = true;
    this.refreshingBeforeConnect = false;
    this.openingTicket = false;
    this.pendingEvents = [];
    chatSocketLog("info", "manual_close", {
      sessionId: this.sessionId,
      readyState: this.socket?.readyState,
    });
    if (this.socket?.readyState === WebSocket.CONNECTING) {
      const socket = this.socket;
      socket.addEventListener("open", () => socket.close(), { once: true });
    } else {
      this.socket?.close();
    }
    this.socket = null;
  }

  private flushPendingEvents() {
    if (this.manuallyClosed || this.socket?.readyState !== WebSocket.OPEN) return;
    const events = this.pendingEvents;
    this.pendingEvents = [];
    chatSocketLog("debug", "flush_pending", {
      sessionId: this.sessionId,
      count: events.length,
    });
    for (const event of events) {
      this.socket.send(JSON.stringify(event));
    }
  }
}

async function createWebSocketTicket(user: User, sessionId: string) {
  const response = await fetchWithAuth(user, "/api/companion/ws-ticket", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId }),
  });
  if (!response.ok) {
    throw new Error(i18n.t("errors.socketDisconnected"));
  }
  const body = (await response.json()) as { ticket?: string };
  const ticket = body.ticket?.trim();
  if (!ticket) {
    throw new Error(i18n.t("errors.socketDisconnected"));
  }
  return ticket;
}

function webSocketAuthProtocols(ticket: string) {
  return ["web3-ws-ticket", ticket];
}

function chatSocketLog(level: "debug" | "info" | "warn" | "error", event: string, data: Record<string, unknown>) {
  const payload = {
    source: "frontend.chat.websocket",
    event: `frontend.ws.${event}`,
    ...data,
  };
  const logger = console[level] ?? console.log;
  logger.call(console, payload);
}

function payloadBytes(data: unknown) {
  if (typeof data === "string") {
    return new TextEncoder().encode(data).byteLength;
  }
  if (data instanceof Blob) return data.size;
  if (data instanceof ArrayBuffer) return data.byteLength;
  return undefined;
}

function elapsedMs(startedAt: number) {
  return Math.round((performance.now() - startedAt) * 100) / 100;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
