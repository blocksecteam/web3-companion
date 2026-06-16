// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type { User } from "@/types/chat";
import i18n from "@/i18n";
import { apiURL } from "@/lib/api-base";
import type {
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";

export type RegisterResponse = {
  status: "pending_passkey";
  user_id: string;
  temp_token: string;
};

export type PasskeySetupResponse = {
  status: "ready";
  challenge_id: string;
  user_id: string;
  public_key: PublicKeyCredentialCreationOptionsJSON;
};

export type PasskeyVerifyResponse = {
  status: "registered";
  user_id: string;
  credential_id: string;
  result_status: "claimable";
};

export type PasskeyResultResponse = {
  status: "saved";
  user_id: string;
  credential_id: string;
  access_token?: string;
  refresh_token?: string;
  token_type?: string;
  expires_at?: number;
  session_id?: string;
};

export type LoginOptionsResponse = {
  status: "ready";
  challenge_id: string;
  user_id: string;
  public_key: PublicKeyCredentialRequestOptionsJSON;
};

export type LoginVerifyResponse = {
  status: "authenticated";
  user_id: string;
  email: string;
  credential_id: string;
  access_token?: string;
  refresh_token?: string;
  token_type?: string;
  expires_at?: number;
  session_id?: string;
};

export type TokenPairResponse = {
  access_token: string;
  refresh_token?: string;
  token_type?: string;
  expires_at?: number;
  session_id?: string;
};

export type RegistrationCredentialPayload = {
  challenge_id: string;
  credential_id: string;
  client_data_json: string;
  attestation_object: string;
  device_name?: string;
};

export type AuthenticationCredentialPayload = {
  challenge_id: string;
  credential_id: string;
  client_data_json: string;
  authenticator_data: string;
  signature: string;
};

export async function registerEmail(email: string): Promise<RegisterResponse> {
  return authFetch("/auth/register", { email });
}

export async function setupPasskey(tempToken: string): Promise<PasskeySetupResponse> {
  return authFetch("/auth/passkey-setup", {
    temp_token: tempToken,
    result_mode: "claim",
  });
}

export async function verifyPasskey(payload: RegistrationCredentialPayload): Promise<PasskeyVerifyResponse> {
  return authFetch("/auth/passkey-verify", payload);
}

export async function claimPasskeyResult(input: {
  temp_token: string;
  challenge_id: string;
}): Promise<PasskeyResultResponse> {
  return authFetch("/auth/passkey-result", input);
}

export async function loginOptions(email: string): Promise<LoginOptionsResponse> {
  return authFetch("/auth/login-options", { email });
}

export async function loginVerify(payload: AuthenticationCredentialPayload): Promise<LoginVerifyResponse> {
  return authFetch("/auth/login-verify", payload);
}

export async function logoutSession(): Promise<{ status: "logged_out" }> {
  const response = await fetch(apiURL("/auth/logout"), {
    method: "POST",
    credentials: "include",
    keepalive: true,
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  if (!response.ok) {
    throw await apiError(response);
  }
  return response.json() as Promise<{ status: "logged_out" }>;
}

export function userFromRegistration(email: string, response: PasskeyResultResponse): User {
  return {
    email,
    userId: response.user_id,
    ...tokensFrom(response),
    credentialId: response.credential_id,
  };
}

export function userFromLogin(response: LoginVerifyResponse): User {
  return {
    email: response.email,
    userId: response.user_id,
    ...tokensFrom(response),
    credentialId: response.credential_id,
  };
}

function tokensFrom(response: {
  access_token?: string;
  refresh_token?: string;
  token_type?: string;
  expires_at?: number;
  session_id?: string;
}) {
  return {
    accessToken: response.access_token,
    tokenType: response.token_type || (response.access_token ? "Bearer" : undefined),
    expiresAt: response.expires_at,
    authSessionId: response.session_id,
  };
}

async function authFetch<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(apiURL(path), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw await apiError(response);
  }
  return response.json() as Promise<T>;
}

async function apiError(response: Response) {
  let message = i18n.t("errors.requestFailed");
  let code = "";
  let retryAfter = 0;
  try {
    const data = (await response.json()) as {
      error?: { code?: string; message?: string; retry_after?: number };
    };
    code = data.error?.code ?? "";
    message = data.error?.message ?? message;
    retryAfter = Number(data.error?.retry_after ?? response.headers.get("Retry-After") ?? 0);
  } catch {
    message = response.statusText || message;
  }
  const error = new Error(message) as Error & { code?: string; retryAfter?: number; status?: number };
  error.code = code;
  error.retryAfter = Number.isFinite(retryAfter) ? retryAfter : 0;
  error.status = response.status;
  return error;
}
