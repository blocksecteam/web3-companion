// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { User } from "@/types/chat";

const USER_KEY = "web3-companion:user";

const listeners = new Set<(user: User | null) => void>();

export function getCurrentUser(): User | null {
  try {
    return persistedUser(JSON.parse(localStorage.getItem(USER_KEY) ?? "null") as User | null);
  } catch {
    return null;
  }
}

export function setCurrentUser(user: User | null) {
  if (!user) {
    localStorage.removeItem(USER_KEY);
    notifyCurrentUser(null);
    return;
  }
  const persisted = persistedUser(user);
  localStorage.setItem(USER_KEY, JSON.stringify(persisted));
  notifyCurrentUser(user);
}

export function logout() {
  setCurrentUser(null);
}

export function resetLocalAuthState() {
  setCurrentUser(null);
}

export function onCurrentUserChanged(listener: (user: User | null) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function notifyCurrentUser(user: User | null) {
  for (const listener of listeners) {
    listener(user);
  }
}

function persistedUser(user: User | null): User | null {
  if (!user) return null;
  return {
    userId: user.userId,
    email: user.email,
    authSessionId: user.authSessionId,
    credentialId: user.credentialId,
  };
}
