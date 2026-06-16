// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { TokenPairResponse } from "@/lib/auth-client";
import { apiURL } from "@/lib/api-base";
import { setCurrentUser } from "@/lib/current-user";
import type { User } from "@/types/chat";

const REFRESH_SKEW_SECONDS = 60;

const refreshPromises = new Map<string, Promise<string>>();

export type AuthSessionOptions = {
  onUserUpdated?: (user: User) => void;
};

export class AuthExpiredError extends Error {
  constructor(message = "This session has expired.") {
    super(message);
    this.name = "AuthExpiredError";
  }
}

export async function fetchWithAuth(user: User, input: RequestInfo | URL, init: RequestInit = {}, options: AuthSessionOptions = {}) {
  const firstToken = shouldRefreshBeforeUse(user) || shouldRefreshMissingAccess(user)
    ? await refreshAccessToken(user, options)
    : bearerTokenFromUser(user);
  const firstResponse = await fetch(apiURL(String(input)), withAuthorization(init, firstToken));

  if (firstResponse.status !== 401 || !canRefreshSession(user)) {
    return firstResponse;
  }

  const refreshedToken = await refreshAccessToken(user, options);
  return fetch(apiURL(String(input)), withAuthorization(init, refreshedToken));
}

export function needsSessionHydration(user: User | null) {
  return Boolean(user?.authSessionId?.trim() && !user.accessToken?.trim());
}

export async function hydrateSession(user: User | null, options: AuthSessionOptions = {}) {
  if (!user) return null;
  if (!needsSessionHydration(user)) return user;
  try {
    await refreshAccessToken(user, options);
    return user;
  } catch (error) {
    if (error instanceof AuthExpiredError) return null;
    throw error;
  }
}

export function bearerTokenFromUser(user: User) {
  const accessToken = user.accessToken?.trim();
  if (accessToken) return accessToken;
  throw new Error("No access token is available for this session.");
}

export function shouldRefreshBeforeUse(user: User) {
  if (!user.accessToken || !canRefreshSession(user)) return false;
  if (!user.expiresAt) return true;
  return user.expiresAt <= Math.floor(Date.now() / 1000) + REFRESH_SKEW_SECONDS;
}

export function shouldRefreshMissingAccess(user: User) {
  return !user.accessToken?.trim() && canRefreshSession(user);
}

export async function refreshAccessToken(user: User, options: AuthSessionOptions = {}) {
  if (!canRefreshSession(user)) {
    throw new Error("Refresh token is unavailable for this session.");
  }
  const key = refreshKey(user);
  let promise = refreshPromises.get(key);
  if (!promise) {
    promise = rotateRefreshToken(user, options).finally(() => {
      refreshPromises.delete(key);
    });
    refreshPromises.set(key, promise);
  }
  return promise;
}

function withAuthorization(init: RequestInit, token: string): RequestInit {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${token}`);
  return {
    ...init,
    headers,
  };
}

async function rotateRefreshToken(user: User, options: AuthSessionOptions) {
  const response = await fetch(apiURL("/auth/refresh"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify(refreshRequestBody(user)),
  });
  if (!response.ok) {
    clearExpiredSession(user);
    throw new AuthExpiredError("Could not refresh this session.");
  }

  const tokenPair = (await response.json()) as TokenPairResponse;
  if (!tokenPair.access_token) {
    throw new Error("Refresh response did not include an access token.");
  }

  const nextUser: User = {
    ...user,
    accessToken: tokenPair.access_token,
    tokenType: tokenPair.token_type || "Bearer",
    expiresAt: tokenPair.expires_at,
    authSessionId: tokenPair.session_id || user.authSessionId,
  };
  if (tokenPair.refresh_token) {
    nextUser.refreshToken = tokenPair.refresh_token;
  } else {
    delete nextUser.refreshToken;
  }
  Object.assign(user, nextUser);
  if (!nextUser.refreshToken) {
    delete user.refreshToken;
  }
  setCurrentUser(nextUser);
  options.onUserUpdated?.(nextUser);
  return tokenPair.access_token;
}

function clearExpiredSession(user: User) {
  delete user.accessToken;
  delete user.refreshToken;
  delete user.tokenType;
  delete user.expiresAt;
  delete user.authSessionId;
  setCurrentUser(null);
}

function canRefreshSession(user: User) {
  return Boolean(user.authSessionId?.trim() || user.refreshToken?.trim());
}

function refreshKey(user: User) {
  return user.authSessionId?.trim() || user.userId?.trim() || user.email.trim();
}

function refreshRequestBody(user: User) {
  const refreshToken = user.refreshToken?.trim();
  if (refreshToken) return { refresh_token: refreshToken };
  return {};
}
