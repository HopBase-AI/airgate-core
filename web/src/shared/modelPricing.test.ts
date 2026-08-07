import { describe, expect, it } from 'vitest';
import { groupServesModel, groupUSDMultiplierForDisplay } from './modelPricing';
import type { MyModelPricing } from './api/models';
import type { GroupResp } from './types';

describe('groupServesModel', () => {
  it('treats null and empty route account lists as unavailable', () => {
    expect(groupServesModel({ 'gpt-*': null }, 'gpt-5.6-pro')).toBe(false);
    expect(groupServesModel({ 'gpt-*': [] }, 'gpt-5.6-pro')).toBe(false);
  });

  it('accepts matching routes with account IDs', () => {
    expect(groupServesModel({ 'gpt-*': [1] }, 'gpt-5.6-pro')).toBe(true);
  });

  it('does not fall through when an exact route is explicitly disabled', () => {
    expect(groupServesModel({
      'gpt-5.6-pro': null,
      'gpt-*': [1],
    }, 'gpt-5.6-pro')).toBe(false);
  });

  it('leaves an empty routing map unrestricted', () => {
    expect(groupServesModel(null, 'gpt-5.6-pro')).toBe(true);
  });

  it('uses the same deterministic overlapping-glob precedence as the scheduler', () => {
    expect(groupServesModel({
      'gemini-*': [1],
      'gemini-*-image': [],
    }, 'gemini-3-pro-image')).toBe(false);
    expect(groupServesModel({
      'g?mini-*': [1],
      'gemini-?': [],
    }, 'gemini-x')).toBe(true);
  });
});

describe('groupUSDMultiplierForDisplay', () => {
  const group = {
    id: 7,
    name: 'Image route',
    platform: 'openai',
    rate_multiplier: 0.6,
    is_exclusive: false,
    status_visible: true,
    delisted: false,
    subscription_type: 'standard',
    model_routing: { 'gpt-*': [11] },
    sort_weight: 0,
    account_active: 1,
    account_error: 0,
    account_disabled: 0,
    account_total: 1,
    capacity_used: 0,
    capacity_total: 1,
    today_cost: 0,
    total_cost: 0,
    created_at: '2026-08-03T00:00:00Z',
    updated_at: '2026-08-03T00:00:00Z',
  } satisfies GroupResp;
  const platforms: MyModelPricing['platforms'] = [{
    platform: 'openai',
    models: [{ id: 'gpt-5.6-pro', input: 5, output: 30, user_rate: 0.6 }],
  }];

  it('treats a zero backend quote as authoritative for an offline route', () => {
    const pricing: MyModelPricing = {
      platforms,
      groups: [{
        id: 7,
        name: 'Image route',
        platform: 'openai',
        group_rate: 0.6,
        effective_rate: 0.6,
        usd_multiplier: 0,
      }],
    };

    expect(groupUSDMultiplierForDisplay(pricing, group)).toBeNull();
  });

  it('does not reconstruct a missing group from an authoritative groups list', () => {
    expect(groupUSDMultiplierForDisplay({ platforms, groups: [] }, group)).toBeNull();
  });

  it('keeps model-derived pricing only for legacy responses without groups', () => {
    const legacyPricing = { platforms } as MyModelPricing;
    expect(groupUSDMultiplierForDisplay(legacyPricing, group)).toBe(0.6);
  });
});
