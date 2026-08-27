import { describe, expect, it } from 'vitest';
import { normalizeLanguage } from './index';

describe('normalizeLanguage', () => {
  it('maps public blog keys to console locale keys', () => {
    expect(normalizeLanguage('zh-Hant')).toBe('zh-HK');
    expect(normalizeLanguage('zh-TW')).toBe('zh-HK');
    expect(normalizeLanguage('zh-Hans')).toBe('zh');
    expect(normalizeLanguage('zh-CN')).toBe('zh');
    expect(normalizeLanguage('en-US')).toBe('en');
    expect(normalizeLanguage('es')).toBe('es');
    expect(normalizeLanguage('es-MX')).toBe('es');
  });

  it('rejects unsupported keys', () => {
    expect(normalizeLanguage('fr')).toBeNull();
  });
});
