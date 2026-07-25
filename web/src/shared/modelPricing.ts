import type { MyModelPricing, MyPricingModel } from './api/models';
import type { GroupResp } from './types';

function globMatches(pattern: string, value: string): boolean {
  let source = '^';
  for (const char of pattern) {
    if (char === '*') source += '[^/]*';
    else if (char === '?') source += '[^/]';
    else source += char.replace(/[\\^$.*+?()[\]{}|]/g, '\\$&');
  }
  return new RegExp(`${source}$`).test(value);
}

export function groupServesModel(routing: GroupResp['model_routing'], modelID: string): boolean {
  const entries = Object.entries(routing ?? {});
  if (entries.length === 0) return true;
  const exact = routing?.[modelID];
  if (exact) return exact.length > 0;
  for (const [pattern, accountIDs] of entries) {
    if (globMatches(pattern, modelID)) return accountIDs.length > 0;
  }
  return false;
}

export function effectiveGroupRate(group: GroupResp, userGroupRates?: Record<number, number>): number {
  const override = userGroupRates?.[group.id];
  if (override != null && override > 0) return override;
  return group.rate_multiplier > 0 ? group.rate_multiplier : 0;
}

export function bestGroupForModel(
  model: MyPricingModel,
  platforms: string[],
  groups: GroupResp[],
  userGroupRates?: Record<number, number>,
): { group: GroupResp; rate: number } | null {
  let best: { group: GroupResp; rate: number } | null = null;
  for (const group of groups) {
    if (!platforms.includes(group.platform) || !groupServesModel(group.model_routing, model.id)) continue;
    const rate = effectiveGroupRate(group, userGroupRates);
    if (rate > 0 && (!best || rate < best.rate)) best = { group, rate };
  }
  return best;
}

export function deriveGroupUSDMultiplier(
  pricing: MyModelPricing | undefined,
  group: GroupResp,
  userGroupRates?: Record<number, number>,
): number | null {
  const rate = effectiveGroupRate(group, userGroupRates);
  if (!(rate > 0)) return null;

  let best = 0;
  for (const platform of pricing?.platforms ?? []) {
    if (platform.platform !== group.platform) continue;
    for (const model of platform.models ?? []) {
      if (!groupServesModel(group.model_routing, model.id)) continue;
      let multiplier = 0;
      if ((model.video_tokens && Object.keys(model.video_tokens).length > 0)
        || (model.image && Object.keys(model.image).length > 0)) {
        multiplier = rate;
      } else {
        const officialInput = model.official?.input ?? (model.currency === 'CNY' ? 0 : model.input);
        if (officialInput > 0 && model.input > 0) multiplier = rate * model.input / officialInput;
      }
      if (multiplier > 0 && (best === 0 || multiplier < best)) best = multiplier;
    }
  }
  return best > 0 ? best : null;
}
