// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import pino from "pino";

export const logger = pino({
  level: process.env.COMPANION_LOG_LEVEL ?? process.env.LOG_LEVEL ?? "info",
  base: {
    service: "web3-companion-server",
  },
  redact: {
    paths: [
      "req.headers.authorization",
      "req.headers.cookie",
      "req.headers['sec-websocket-protocol']",
      "headers.authorization",
      "headers.cookie",
      "headers['sec-websocket-protocol']",
      "*.accessToken",
      "*.refreshToken",
      "*.apiKey",
      "*.token",
    ],
    remove: true,
  },
});
