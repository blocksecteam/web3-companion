// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

const configuredAPIBase = String(import.meta.env.VITE_WEB3_API_URL ?? "").trim().replace(/\/+$/, "");

export function apiURL(path: string) {
  if (!configuredAPIBase) return path;
  return `${configuredAPIBase}${path.startsWith("/") ? path : `/${path}`}`;
}

export function websocketURL(path: string) {
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;
  if (configuredAPIBase) {
    const base = new URL(configuredAPIBase);
    base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
    base.pathname = normalizedPath;
    base.search = "";
    base.hash = "";
    return base.toString();
  }
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  return `${protocol}://${window.location.host}${normalizedPath}`;
}
