export type ModelRouting = Record<string, number[]>;

export function isModelRouteSelected(routing: ModelRouting, modelId: string): boolean {
  return (routing[modelId]?.length ?? 0) > 0;
}

export function normalizeModelRouting(routing: ModelRouting): ModelRouting {
  const normalized: ModelRouting = {};
  for (const [modelId, accountIds] of Object.entries(routing)) {
    const uniqueAccountIds = Array.from(new Set(accountIds));
    if (uniqueAccountIds.length > 0) {
      normalized[modelId] = uniqueAccountIds;
    }
  }
  return normalized;
}

export function toggleModelRoute(
  routing: ModelRouting,
  modelId: string,
  checked: boolean,
  defaultAccountIds: number[],
): ModelRouting {
  const next = { ...routing };
  if (!checked) {
    delete next[modelId];
    return next;
  }

  if ((next[modelId]?.length ?? 0) === 0) {
    next[modelId] = Array.from(new Set(defaultAccountIds));
  }
  return next;
}
