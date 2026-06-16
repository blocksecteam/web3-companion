// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

export const MAX_NAME_LENGTH = 50;
export const MAX_API_KEY_LENGTH = 200;
export const MAX_USD_VALUE = 1_000_000_000;

export function createSessionId() {
  return crypto.randomUUID();
}

export type ScrollViewport = {
  scrollHeight: number;
  scrollTo: (options: ScrollToOptions) => void;
};

export function scrollToViewportBottom(viewport: ScrollViewport | null, behavior: ScrollBehavior = "smooth") {
  viewport?.scrollTo({ top: viewport.scrollHeight, behavior });
}

export type ScrollPosition = {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
};

// isViewportNearBottom reports whether the viewport is scrolled to within
// `threshold` pixels of the bottom. It drives "stick to bottom" auto-scroll:
// when the user has scrolled up to read history, the chat must NOT yank them
// back down on every streamed token. A null viewport (not mounted yet) is
// treated as near-bottom so the first paint still pins to the latest message.
export function isViewportNearBottom(viewport: ScrollPosition | null, threshold = 80): boolean {
  if (!viewport) return true;
  const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
  return distanceFromBottom <= threshold;
}

export function messageFromApiError(error: unknown, fallback: string, messagesByCode: Record<string, string>) {
  const code = error && typeof error === "object" && "code" in error ? String((error as { code?: unknown }).code ?? "") : "";
  if (code && messagesByCode[code]) return messagesByCode[code];
  return error instanceof Error && error.message ? error.message : fallback;
}

export type WalletActionSafetyState =
  | { type: "delete" }
  | { type: "bind_policy" }
  | { type: "export"; privateKey?: string; saved?: boolean }
  | null;

export function shouldWarnBeforeLeavingWalletAction(action: WalletActionSafetyState) {
  return Boolean(action?.type === "export" && action.privateKey && !action.saved);
}
