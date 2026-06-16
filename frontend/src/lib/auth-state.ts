// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { User } from "@/types/chat";

export type AuthStatus = "guest" | "stale" | "ready";

export function authStatus(user: User | null): AuthStatus {
  if (!user) return "guest";
  return hasSessionCredential(user) ? "ready" : "stale";
}

export function hasSessionCredential(user: User | null) {
  return Boolean(user?.accessToken?.trim() || user?.authSessionId?.trim());
}
