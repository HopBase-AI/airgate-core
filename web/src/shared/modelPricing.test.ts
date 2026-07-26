import { describe, expect, it } from 'vitest';
import { groupServesModel } from './modelPricing';

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
});
