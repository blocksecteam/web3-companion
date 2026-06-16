// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { Hono } from "hono";
import type { ServerWebSocket } from "bun";
import type { ClientWsEvent, ServerWsEvent } from "../types/chat";
import { runCompanion as defaultRunCompanion } from "./claude-runner";
import { logger } from "./logger";
import { hasModelAuth } from "./model-auth";
import { browserChatMessage } from "./message-projector";
import type { SessionStore } from "./session-store";

type WsData = {
  sessionId: string;
};

export type CompanionRuntime = {
  app: Hono;
  websocket: {
    open(ws: ServerWebSocket<WsData>): void;
    message(ws: ServerWebSocket<WsData>, raw: string | Buffer): void;
    close(ws: ServerWebSocket<WsData>, code: number, reason: string): void;
  };
};

type RunCompanion = typeof defaultRunCompanion;

type RuntimeOptions = {
  runCompanion?: RunCompanion;
  maxMessageBytes?: number;
  maxPromptChars?: number;
};

export function createCompanionRuntime(store: SessionStore, options: RuntimeOptions = {}): CompanionRuntime {
  const app = new Hono();
  const sockets = new Set<ServerWebSocket<WsData>>();
  const activeRuns = new Map<string, AbortController>();
  const socketOpenedAt = new WeakMap<ServerWebSocket<WsData>, number>();
  let runningSessionId: string | null = null;
  const runCompanion = options.runCompanion ?? defaultRunCompanion;
  const maxMessageBytes = options.maxMessageBytes ?? numberEnv("COMPANION_MAX_MESSAGE_BYTES", 64 * 1024);
  const maxPromptChars = options.maxPromptChars ?? numberEnv("COMPANION_MAX_PROMPT_CHARS", 32 * 1024);

  app.get("/api/health", (context) => {
    const modelAuthConfigured = hasModelAuth();
    return context.json({
      ok: true,
      sdk: "claude-agent-sdk",
      model_auth_configured: modelAuthConfigured,
      ...(!modelAuthConfigured ? { code: "model_auth_missing" } : {}),
      ...(process.env.COMPANION_STARTUP_NONCE ? { nonce: process.env.COMPANION_STARTUP_NONCE } : {}),
    });
  });

  app.get("/api/sessions", (context) => context.json(store.listSessions()));

  app.get("/api/sessions/:id/messages", (context) => {
    const sessionId = context.req.param("id");
    return context.json(store.listMessages(sessionId));
  });

  app.delete("/api/sessions/:id", (context) => {
    const sessionId = context.req.param("id");
    if (runningSessionId === sessionId) {
      return context.json({ error: "Cannot delete a running session." }, 409);
    }
    store.deleteSession(sessionId);
    return context.json({ ok: true });
  });

  async function handleWsEvent(ws: ServerWebSocket<WsData>, raw: string | Buffer) {
    const parsed = parseWsEvent(raw, { maxMessageBytes, maxPromptChars });
    if (!parsed.ok) {
      logger.warn(
        {
          event: "companion.ws.message.unreadable",
          sessionId: ws.data.sessionId,
          payloadBytes: payloadBytes(raw),
          reason: parsed.message,
        },
        "companion websocket message unreadable",
      );
      send(ws, {
        type: "error",
        sessionId: ws.data.sessionId,
        message: parsed.message,
      });
      return;
    }
    const event = parsed.event;

    if (event.sessionId !== ws.data.sessionId) {
      logger.warn(
        {
          event: "companion.ws.message.session_mismatch",
          socketSessionId: ws.data.sessionId,
          eventSessionId: event.sessionId,
          clientEventType: event.type,
        },
        "companion websocket message session mismatch",
      );
      send(ws, {
        type: "error",
        sessionId: ws.data.sessionId,
        message: "Session route changed. Reopen this conversation before sending again.",
      });
      return;
    }

    logger.info(
      {
        event: "companion.ws.message.received",
        sessionId: event.sessionId,
        clientEventType: event.type,
        payloadBytes: payloadBytes(raw),
        openSockets: sockets.size,
        runningSessionId,
      },
      "companion websocket message received",
    );

    if (event.type === "stop") {
      logger.info(
        {
          event: "companion.run.stop_requested",
          sessionId: event.sessionId,
          hadActiveRun: activeRuns.has(event.sessionId),
        },
        "companion run stop requested",
      );
      activeRuns.get(event.sessionId)?.abort();
      activeRuns.delete(event.sessionId);
      if (runningSessionId === event.sessionId) {
        runningSessionId = null;
        broadcast(event.sessionId, { type: "status", sessionId: event.sessionId, status: "idle" });
        const session = store.updateSessionStatus(event.sessionId, "idle");
        if (session) broadcast(event.sessionId, { type: "session", session });
      }
      return;
    }

    if (event.type !== "user_message") return;

    const sessionId = event.sessionId;
    store.ensureSession(sessionId);

    if (runningSessionId && runningSessionId !== sessionId) {
      logger.warn(
        {
          event: "companion.run.rejected_other_session_running",
          sessionId,
          runningSessionId,
        },
        "companion rejected message while another session is running",
      );
      send(ws, {
        type: "error",
        sessionId,
        message: "Another session is already running. Finish or stop it before starting a new one.",
      });
      return;
    }

    if (runningSessionId === sessionId) {
      logger.warn(
        {
          event: "companion.run.rejected_same_session_running",
          sessionId,
        },
        "companion rejected message because session is already running",
      );
      send(ws, {
        type: "error",
        sessionId,
        message: "This session is already running.",
      });
      return;
    }

    const userMessage = browserChatMessage(event.id, event.content);
    const savedUserMessage = store.addMessage(sessionId, userMessage);
    store.renameFromFirstMessage(sessionId, event.content);
    const runningSession = store.updateSessionStatus(sessionId, "running");

    if (runningSession) broadcast(sessionId, { type: "session", session: runningSession });
    broadcast(sessionId, { type: "message", sessionId, message: savedUserMessage });
    broadcast(sessionId, { type: "status", sessionId, status: "running" });

    const controller = new AbortController();
    runningSessionId = sessionId;
    activeRuns.set(sessionId, controller);
    const runStartedAt = performance.now();
    logger.info(
      {
        event: "companion.run.started",
        sessionId,
        promptChars: event.content.length,
      },
      "companion run started",
    );

    try {
      await runCompanion({
        sessionId,
        prompt: event.content,
        store,
        signal: controller.signal,
        emit: (serverEvent) => broadcast(sessionId, serverEvent as ServerWsEvent),
      });
    } catch (error) {
      if (!controller.signal.aborted) {
        logger.error(
          {
            event: "companion.run.failed",
            sessionId,
            durationMs: elapsedMs(runStartedAt),
            err: serializeError(error),
          },
          "companion run failed",
        );
        broadcast(sessionId, {
          type: "error",
          sessionId,
          message: error instanceof Error ? error.message : "Agent run failed.",
        });
      }
    } finally {
      activeRuns.delete(sessionId);
      if (runningSessionId === sessionId) {
        runningSessionId = null;
      }
      const idleSession = store.updateSessionStatus(sessionId, "idle");
      broadcast(sessionId, { type: "status", sessionId, status: "idle" });
      if (idleSession) broadcast(sessionId, { type: "session", session: idleSession });
      logger.info(
        {
          event: "companion.run.finished",
          sessionId,
          durationMs: elapsedMs(runStartedAt),
          aborted: controller.signal.aborted,
        },
        "companion run finished",
      );
    }
  }

  function broadcast(sessionId: string, event: ServerWsEvent) {
    let delivered = 0;
    for (const socket of sockets) {
      if (socket.data.sessionId === sessionId) {
        delivered++;
        send(socket, event);
      }
    }
    logger.debug(
      {
        event: "companion.ws.broadcast",
        sessionId,
        serverEventType: event.type,
        delivered,
        openSockets: sockets.size,
      },
      "companion websocket broadcast",
    );
  }

  function abortRunIfSessionDisconnected(sessionId: string) {
    logger.info(
      {
        event: "companion.run.disconnect_kept_alive",
        sessionId,
      },
      "companion run kept alive after websocket disconnect",
    );
  }

  function send(ws: ServerWebSocket<WsData>, event: ServerWsEvent) {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(event));
      logger.debug(
        {
          event: "companion.ws.send",
          sessionId: ws.data.sessionId,
          serverEventType: event.type,
        },
        "companion websocket event sent",
      );
      return;
    }
    logger.warn(
      {
        event: "companion.ws.send.skipped",
        sessionId: ws.data.sessionId,
        serverEventType: event.type,
        readyState: ws.readyState,
      },
      "companion websocket send skipped",
    );
  }

  return {
    app,
    websocket: {
      open(ws) {
        sockets.add(ws);
        socketOpenedAt.set(ws, performance.now());
        logger.info(
          {
            event: "companion.ws.open",
            sessionId: ws.data.sessionId,
            openSockets: sockets.size,
          },
          "companion websocket opened",
        );
        const session = store.getSession(ws.data.sessionId);
        if (session) {
          send(ws, { type: "session", session });
        }
        // Deliver the global running state so the frontend can sync after a
        // session switch. Without this, if session A was running and its idle
        // event was lost (socket closed during switch), the frontend's
        // runningSessionId stays stuck on A forever.
        send(ws, {
          type: "status",
          sessionId: runningSessionId ?? ws.data.sessionId,
          status: runningSessionId ? "running" : "idle",
        });
      },
      message(ws, raw) {
        void handleWsEvent(ws, raw).catch((error) => {
          logger.error(
            {
              event: "companion.ws.message.failed",
              sessionId: ws.data.sessionId,
              err: serializeError(error),
            },
            "companion websocket message failed",
          );
          send(ws, {
            type: "error",
            sessionId: ws.data.sessionId,
            message: error instanceof Error ? error.message : "Realtime event failed.",
          });
        });
      },
      close(ws, code, reason) {
        sockets.delete(ws);
        const openedAt = socketOpenedAt.get(ws);
        socketOpenedAt.delete(ws);
        logger.info(
          {
            event: "companion.ws.close",
            sessionId: ws.data.sessionId,
            openSockets: sockets.size,
            durationMs: openedAt === undefined ? undefined : elapsedMs(openedAt),
            code,
            reason: reason || undefined,
          },
          "companion websocket closed",
        );
        abortRunIfSessionDisconnected(ws.data.sessionId);
      },
    },
  };
}

function payloadBytes(raw: string | Buffer) {
  return typeof raw === "string" ? new TextEncoder().encode(raw).byteLength : raw.byteLength;
}

function elapsedMs(startedAt: number) {
  return Math.round((performance.now() - startedAt) * 100) / 100;
}

function serializeError(error: unknown) {
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
      stack: error.stack,
    };
  }
  return { message: String(error) };
}

type ParseResult =
  | { ok: true; event: ClientWsEvent }
  | { ok: false; message: string };

function parseWsEvent(raw: string | Buffer, limits: { maxMessageBytes: number; maxPromptChars: number }): ParseResult {
  if (payloadBytes(raw) > limits.maxMessageBytes) {
    return { ok: false, message: "Realtime event is too large." };
  }
  let parsed: unknown;
  try {
    const text = typeof raw === "string" ? raw : raw.toString("utf8");
    parsed = JSON.parse(text) as unknown;
  } catch {
    return { ok: false, message: "The server could not understand that realtime event." };
  }
  if (!isRecord(parsed)) {
    return { ok: false, message: "Realtime event must be a JSON object." };
  }
  const type = parsed.type;
  const sessionId = parsed.sessionId;
  if (type !== "user_message" && type !== "stop") {
    return { ok: false, message: "Realtime event type is not supported." };
  }
  if (!isNonEmptyString(sessionId)) {
    return { ok: false, message: "Realtime event session is required." };
  }
  if (type === "stop") {
    return { ok: true, event: { type, sessionId } };
  }
  if (!isNonEmptyString(parsed.id)) {
    return { ok: false, message: "Realtime message id is required." };
  }
  if (!isNonEmptyString(parsed.content)) {
    return { ok: false, message: "Realtime message content is required." };
  }
  if (parsed.content.length > limits.maxPromptChars) {
    return { ok: false, message: "Realtime message content is too long." };
  }
  return { ok: true, event: { type, id: parsed.id, sessionId, content: parsed.content } };
}

function numberEnv(name: string, fallback: number) {
  const value = Number(process.env[name]);
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim() !== "";
}
