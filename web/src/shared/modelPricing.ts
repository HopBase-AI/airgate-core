import type { MyModelPricing } from './api/models';
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

type ModelRouting = Record<string, number[] | null>;

export function groupServesModel(
  routing: GroupResp['model_routing'] | ModelRouting | null,
  modelID: string,
): boolean {
  const entries = Object.entries(routing ?? {});
  if (entries.length === 0) return true;
  const exact = routing?.[modelID];
  if (Object.prototype.hasOwnProperty.call(routing, modelID)) {
    return Array.isArray(exact) && exact.length > 0;
  }
  const matched = entries
    .filter(([pattern]) => globMatches(pattern, modelID))
    .sort(([left], [right]) => right.length - left.length || (left < right ? -1 : left > right ? 1 : 0))[0];
  return !!matched && Array.isArray(matched[1]) && matched[1].length > 0;
}

export function effectiveGroupRate(group: GroupResp, userGroupRates?: Record<number, number>): number {
  const override = userGroupRates?.[group.id];
  if (override != null && override > 0) return override;
  return group.rate_multiplier > 0 ? group.rate_multiplier : 0;
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

export function groupUSDMultiplierForDisplay(
  pricing: MyModelPricing | undefined,
  group: GroupResp,
  userGroupRates?: Record<number, number>,
): number | null {
  if (!pricing) return null;
  if (Object.prototype.hasOwnProperty.call(pricing, 'groups')) {
    const quote = pricing.groups?.find((item) => item.id === group.id);
    const multiplier = quote?.usd_multiplier;
    return quote && quote.effective_rate > 0 && typeof multiplier === 'number'
      && Number.isFinite(multiplier) && multiplier > 0
      ? multiplier
      : null;
  }
  return deriveGroupUSDMultiplier(pricing, group, userGroupRates);
}
