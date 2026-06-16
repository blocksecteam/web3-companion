// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { prepareClaudeHome } from "./claude-runner";
import { createCompanionRuntime } from "./app";
import { logger } from "./logger";
import { createSessionStoreFromConfig } from "./session-store";

type WsData = {
  sessionId: string;
};

await prepareClaudeHome();

const store = createSessionStoreFromConfig();
const runtime = createCompanionRuntime(store);
const port = Number(process.env.COMPANION_PORT ?? 8787);
const hostname = process.env.COMPANION_HOST ?? "127.0.0.1";

const server = Bun.serve<WsData>({
  port,
  hostname,
  async fetch(request, server) {
    const startedAt = performance.now();
    const url = new URL(request.url);
    if (url.pathname === "/ws/chat") {
      const sessionId = url.searchParams.get("session") || crypto.randomUUID();
      logger.info(
        {
          event: "companion.ws.upgrade.request",
          sessionId,
          method: request.method,
          path: url.pathname,
          hasUpgrade: request.headers.get("upgrade")?.toLowerCase() === "websocket",
          hasWebSocketKey: request.headers.has("sec-websocket-key"),
          protocolCount: protocolCount(request.headers.get("sec-websocket-protocol")),
        },
        "companion websocket upgrade requested",
      );
      if (server.upgrade(request, { data: { sessionId } })) {
        logger.info(
          {
            event: "companion.ws.upgrade.accepted",
            sessionId,
            durationMs: elapsedMs(startedAt),
          },
          "companion websocket upgrade accepted",
        );
        return undefined;
      }
      logger.warn(
        {
          event: "companion.ws.upgrade.rejected",
          sessionId,
          durationMs: elapsedMs(startedAt),
        },
        "companion websocket upgrade rejected",
      );
      return new Response("WebSocket upgrade failed", { status: 400 });
    }

    const response = await runtime.app.fetch(request);
    if (url.pathname.startsWith("/api/")) {
      logger.info(
        {
          event: "companion.http.response",
          method: request.method,
          path: url.pathname,
          status: response.status,
          durationMs: elapsedMs(startedAt),
        },
        "companion http response",
      );
    }
    return response;
  },
  websocket: runtime.websocket,
});

await writeReadyFile({
  host: hostname,
  port: server.port ?? port,
  pid: process.pid,
  nonce: process.env.COMPANION_STARTUP_NONCE ?? "",
  sdk: "claude-agent-sdk",
});

logger.info(
  {
    event: "companion.server.started",
    hostname,
    port: server.port,
  },
  "Web3 Companion server listening",
);

async function writeReadyFile(payload: { host: string; port: number; pid: number; nonce: string; sdk: string }) {
  const path = process.env.COMPANION_READY_FILE;
  if (!path) return;
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(payload)}\n`, { mode: 0o600 });
}

function elapsedMs(startedAt: number) {
  return Math.round((performance.now() - startedAt) * 100) / 100;
}

function protocolCount(header: string | null) {
  if (!header) return 0;
  return header.split(",").filter((part) => part.trim() !== "").length;
}
