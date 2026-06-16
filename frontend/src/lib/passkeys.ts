// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import type {
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import type {
  AuthenticationCredentialPayload,
  RegistrationCredentialPayload,
} from "@/lib/auth-client";
import i18n from "@/i18n";

const MIN_PASSKEY_TIMEOUT_MS = 180000;
const DEFAULT_PASSKEY_TIMEOUT_MS = 300000;
const PASSKEY_WATCHDOG_GRACE_MS = 30000;
const MAX_PASSKEY_TIMEOUT_MS = 600000;
const MAX_BROWSER_TIMER_MS = 2147483647;
const PASSKEY_CANCELLED_ERROR_CODES = new Set(["ERROR_CEREMONY_ABORTED", "ERROR_PASSTHROUGH_SEE_CAUSE_PROPERTY"]);
const PASSKEY_PRIVACY_ERROR_FRAGMENT = "privacy-considerations-client";

export async function createPasskeyCredential(
  challengeId: string,
  optionsJSON: PublicKeyCredentialCreationOptionsJSON,
): Promise<RegistrationCredentialPayload> {
  const { startRegistration } = await loadWebAuthnBrowser();
  const timeoutMs = passkeyTimeoutMs(optionsJSON.timeout);
  try {
    await assertPasskeyReady();
    const credential = await withPasskeyTimeout(startRegistration({ optionsJSON }), timeoutMs);
    return {
      challenge_id: challengeId,
      credential_id: credential.id,
      client_data_json: credential.response.clientDataJSON,
      attestation_object: credential.response.attestationObject,
      device_name: navigator.platform || "Browser Passkey",
    };
  } catch (error) {
    throw passkeyError(error, i18n.t("errors.passkeyRegistration"));
  }
}

export async function getPasskeyAssertion(
  challengeId: string,
  optionsJSON: PublicKeyCredentialRequestOptionsJSON,
): Promise<AuthenticationCredentialPayload> {
  const { startAuthentication } = await loadWebAuthnBrowser();
  const normalizedOptions = normalizeRequestOptions(optionsJSON);
  const timeoutMs = passkeyTimeoutMs(normalizedOptions.timeout);
  try {
    await assertPasskeyReady();
    const credential = await withPasskeyTimeout(startAuthentication({ optionsJSON: normalizedOptions }), timeoutMs);
    return {
      challenge_id: challengeId,
      credential_id: credential.id,
      client_data_json: credential.response.clientDataJSON,
      authenticator_data: credential.response.authenticatorData,
      signature: credential.response.signature,
    };
  } catch (error) {
    throw passkeyError(error, i18n.t("errors.passkeyVerification"));
  }
}

function normalizeRequestOptions(
  options: PublicKeyCredentialRequestOptionsJSON,
): PublicKeyCredentialRequestOptionsJSON {
  const rawAllowCredentials = options.allowCredentials as unknown;
  if (!Array.isArray(rawAllowCredentials) || rawAllowCredentials.length === 0) {
    return options;
  }
  if (typeof rawAllowCredentials[0] !== "string") {
    return options;
  }
  return {
    ...options,
    allowCredentials: rawAllowCredentials.map((id) => ({
      id,
      type: "public-key" as const,
    })),
  };
}

async function withPasskeyTimeout<T>(promise: Promise<T>, ceremonyTimeoutMs: number) {
  const { WebAuthnAbortService } = await loadWebAuthnBrowser();
  const watchdogTimeoutMs = Math.min(ceremonyTimeoutMs + PASSKEY_WATCHDOG_GRACE_MS, MAX_BROWSER_TIMER_MS);
  let timeoutId: number | undefined;

  try {
    return await Promise.race([
      promise,
      new Promise<T>((_, reject) => {
        timeoutId = window.setTimeout(() => {
          WebAuthnAbortService.cancelCeremony();
          reject(new Error(i18n.t("errors.passkeyTimeout")));
        }, watchdogTimeoutMs);
      }),
    ]);
  } finally {
    if (timeoutId) window.clearTimeout(timeoutId);
  }
}

export function passkeyTimeoutMs(serverTimeout?: number) {
  const configured = Number(import.meta.env.VITE_PASSKEY_TIMEOUT_MS ?? serverTimeout ?? DEFAULT_PASSKEY_TIMEOUT_MS);
  if (!Number.isFinite(configured) || configured <= 0) return DEFAULT_PASSKEY_TIMEOUT_MS;
  return Math.min(Math.max(configured, MIN_PASSKEY_TIMEOUT_MS), MAX_PASSKEY_TIMEOUT_MS);
}

export async function cancelPasskeyCeremony() {
  const { WebAuthnAbortService } = await loadWebAuthnBrowser();
  WebAuthnAbortService.cancelCeremony();
}

async function assertPasskeyReady() {
  const { browserSupportsWebAuthn, platformAuthenticatorIsAvailable } = await loadWebAuthnBrowser();
  if (!window.isSecureContext) {
    throw new Error(i18n.t("errors.passkeySecureContext"));
  }
  if (!browserSupportsWebAuthn()) {
    throw new Error(i18n.t("errors.passkeyUnsupported"));
  }
  if (!(await platformAuthenticatorIsAvailable())) {
    throw new Error(i18n.t("errors.passkeyUnavailable"));
  }
}

function passkeyError(error: unknown, defaultMessage: string) {
  return new Error(passkeyErrorMessage(error, defaultMessage));
}

export function passkeyErrorMessage(error: unknown, defaultMessage: string) {
  if (!isRecord(error)) return defaultMessage;
  if (isPasskeyCancelError(error)) return i18n.t("errors.passkeyCancelled");
  const message = typeof error.message === "string" ? error.message : "";
  return message || defaultMessage;
}

function isPasskeyCancelError(error: Record<string, unknown>) {
  const name = typeof error.name === "string" ? error.name : "";
  const message = typeof error.message === "string" ? error.message : "";
  const code = typeof error.code === "string" ? error.code : "";
  const cause = isRecord(error.cause) ? error.cause : undefined;
  const causeName = typeof cause?.name === "string" ? cause.name : "";
  return (
    name === "AbortError" ||
    name === "NotAllowedError" ||
    causeName === "AbortError" ||
    causeName === "NotAllowedError" ||
    PASSKEY_CANCELLED_ERROR_CODES.has(code) ||
    message.includes(PASSKEY_PRIVACY_ERROR_FRAGMENT)
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function loadWebAuthnBrowser() {
  return import("@simplewebauthn/browser");
}
