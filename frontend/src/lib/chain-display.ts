// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

type Translate = (key: string, options?: { defaultValue?: string }) => string;

export type ChainLabelSource = {
  name: string;
  nativeSymbol?: string;
};

export function chainDisplayName(chainName: string, t: Translate) {
  const normalized = chainName.trim().toLowerCase();
  if (!normalized) return "";
  return t(`chains.${normalized}`, { defaultValue: chainName });
}

export function chainOptionLabel(chain: ChainLabelSource, t: Translate) {
  const displayName = chainDisplayName(chain.name, t);
  return chain.nativeSymbol ? `${displayName} (${chain.nativeSymbol})` : displayName;
}
