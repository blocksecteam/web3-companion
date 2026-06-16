// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

export type PasskeyConfirmLink = {
  challengeId: string;
  url: string;
};

const markdownLinkPattern = /\[([^\]]*)\]\(([^)]+)\)/g;
const rawLinkPattern = /[*`]*(https?:\/\/[^\s<>()*`]+|\/confirm\/[^\s<>()*`]+)[*`]*/g;

export function confirmLinksFromContent(content: string): PasskeyConfirmLink[] {
  const links: PasskeyConfirmLink[] = [];
  for (const match of content.matchAll(markdownLinkPattern)) {
    const url = cleanUrl(match[2] ?? "");
    const challengeId = challengeIdFromUrl(url);
    if (challengeId) links.push({ challengeId, url });
  }
  for (const match of content.matchAll(rawLinkPattern)) {
    const url = cleanUrl(match[1] ?? "");
    const challengeId = challengeIdFromUrl(url);
    if (challengeId) links.push({ challengeId, url });
  }
  return dedupeConfirmLinks(links);
}

export function removeConfirmLinksFromContent(content: string) {
  return content
    .replace(markdownLinkPattern, (full, _label: string, href: string) =>
      challengeIdFromUrl(cleanUrl(href)) ? "" : full,
    )
    .replace(rawLinkPattern, (full) => (challengeIdFromUrl(cleanUrl(full)) ? "" : full))
    .replace(/[ \t]+([.,;:!?])/g, "$1")
    .replace(/[ \t]+\n/g, "\n")
    .trim();
}

function challengeIdFromUrl(value: string) {
  try {
    const url = new URL(value, "https://web3-companion.local");
    const pathParts = url.pathname.split("/").filter(Boolean);
    if (pathParts[0] === "confirm" && pathParts[1]) return decodeURIComponent(pathParts[1]);
  } catch {
    return "";
  }
  return "";
}

function cleanUrl(value: string) {
  return value
    .trim()
    .replace(/^[*_`]+/g, "")
    .replace(/[.,;:!?*_`]+$/g, "");
}

function dedupeConfirmLinks(links: PasskeyConfirmLink[]) {
  const seen = new Set<string>();
  return links.filter((link) => {
    if (seen.has(link.challengeId)) return false;
    seen.add(link.challengeId);
    return true;
  });
}
