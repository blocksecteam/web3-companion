// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { createSessionId } from "@/lib/client-utils";
import type { Page } from "@/types/chat";

export type AppRoute = {
  page: Page;
  sessionId?: string;
};

export function readRoute(): AppRoute {
  const [, firstSegment, secondSegment] = window.location.pathname.split("/");
  if (firstSegment === "chat") {
    return { page: "chat", sessionId: secondSegment };
  }
  if (firstSegment === "auth") return { page: "auth" };
  if (firstSegment === "confirm") return { page: "confirm", sessionId: secondSegment };
  if (firstSegment === "portfolio") return { page: "portfolio" };
  if (firstSegment === "transactions") return { page: "transactions" };
  if (firstSegment === "policies") return { page: "policies" };
  if (firstSegment === "audit") return { page: "audit" };
  if (firstSegment === "wallets") return { page: "wallets" };
  if (firstSegment === "settings") return { page: "settings" };
  return { page: "landing" };
}

export function normalizeRoute(route: AppRoute): AppRoute {
  if (route.page === "chat") {
    return { page: "chat", sessionId: route.sessionId || createSessionId() };
  }
  return { page: route.page };
}

export function pathForRoute(route: AppRoute) {
  if (route.page === "chat") return `/chat/${encodeURIComponent(route.sessionId ?? createSessionId())}`;
  if (route.page === "confirm") return `/confirm/${encodeURIComponent(route.sessionId ?? "")}`;
  if (route.page === "landing") return "/";
  return `/${route.page}`;
}

export function shouldWriteHistory(nextPath: string, location: Pick<Location, "pathname" | "search" | "hash">) {
  return `${location.pathname}${location.search}${location.hash}` !== nextPath;
}

export function hasResetAuthRequest(search: string) {
  return new URLSearchParams(search).get("resetAuth") === "1";
}
