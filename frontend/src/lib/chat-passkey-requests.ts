// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import {
  confirmLinksFromContent,
  removeConfirmLinksFromContent,
} from "@/lib/passkey-confirm-links";
import type { ChatMessage, PasskeyRequestBlock } from "@/types/chat";

export type ChatPasskeyRequest = {
  challengeId: string;
  signUrl: string;
  source: "block" | "text";
  operation?: string;
  transactionId?: string;
  summary?: Record<string, unknown>;
};

export function passkeyRequestsForMessage(message: Pick<ChatMessage, "content" | "partial" | "blocks">) {
  const structured = (message.blocks ?? [])
    .filter(isPasskeyRequestBlock)
    .map((block): ChatPasskeyRequest => ({
      challengeId: block.challengeId,
      signUrl: block.signUrl,
      source: "block",
      operation: block.operation,
      transactionId: block.transactionId,
      summary: block.summary,
    }));
  const textLinks = !message.partial ? confirmLinksFromContent(message.content) : [];
  const textRequests = textLinks.map((link): ChatPasskeyRequest => ({
    challengeId: link.challengeId,
    signUrl: link.url,
    source: "text",
  }));

  return {
    displayContent: textLinks.length > 0 ? removeConfirmLinksFromContent(message.content) : message.content,
    requests: structured.length > 0 ? dedupeRequests(structured) : dedupeRequests(textRequests),
  };
}

function isPasskeyRequestBlock(block: NonNullable<ChatMessage["blocks"]>[number]): block is PasskeyRequestBlock {
  return block.type === "passkey_request";
}

function dedupeRequests(requests: ChatPasskeyRequest[]) {
  const seen = new Set<string>();
  return requests.filter((request) => {
    if (seen.has(request.challengeId)) return false;
    seen.add(request.challengeId);
    return true;
  });
}
