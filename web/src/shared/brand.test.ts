import { describe, expect, it } from 'vitest';
import { defaultThemeForBrand, isHostUnder, provisionalBrand, resolveBrand } from './brand';

describe('resolveBrand', () => {
  it('站名 HopBase 或为空即 HopBase', () => {
    expect(resolveBrand({ siteId: '', siteName: 'HopBase' })).toBe('hopbase');
    expect(resolveBrand({ siteName: 'hop-base' })).toBe('hopbase');
    expect(resolveBrand({})).toBe('hopbase');
  });

  it('ink 来源站或站名 Essevin 归 essevin', () => {
    expect(resolveBrand({ siteId: 'ink', siteName: 'HopBase' })).toBe('essevin');
    expect(resolveBrand({ siteId: '', siteName: 'Essevin' })).toBe('essevin');
    expect(resolveBrand({ siteId: '', siteName: 'essevin' })).toBe('essevin');
  });

  it('下架的落地页不再写死成品牌:删掉 sites_branding 条目就不再顶站', () => {
    // 老用户的注册归因仍是已下架的站,但控制台跟着站点设置走,不再被写死的品牌顶住。
    expect(resolveBrand({ siteId: 'kite', siteName: 'Essevin' })).toBe('essevin');
    expect(resolveBrand({ siteId: '', siteName: 'Kite' })).toBeNull();
    expect(resolveBrand({ siteId: '', siteName: 'HopBase' })).toBe('hopbase');
  });

  it('有 sites_branding 配置的来源站才算品牌;?ref= 之类未配置的来源站不剥 HopBase 皮肤', () => {
    expect(resolveBrand({ siteId: 'partner-site', siteName: 'HopBase', hasSiteBranding: true })).toBe('partner-site');
    expect(resolveBrand({ siteId: 'producthunt', siteName: 'HopBase', hasSiteBranding: false })).toBe('hopbase');
    expect(resolveBrand({ siteId: 'internal-deploy-probe', siteName: 'HopBase' })).toBe('hopbase');
  });

  it('认不出的站名不猜,返回 null 让调用方保持现状', () => {
    expect(resolveBrand({ siteId: '', siteName: 'Essevin AI' })).toBeNull();
    expect(resolveBrand({ siteId: '', siteName: '萃灵 Essevin' })).toBeNull();
  });
});

describe('isHostUnder', () => {
  it('根域与子域算,后缀碰瓷不算', () => {
    expect(isHostUnder('hop-base.com', 'hop-base.com')).toBe(true);
    expect(isHostUnder('api.hop-base.com', 'hop-base.com')).toBe(true);
    expect(isHostUnder('API.HOP-BASE.COM', 'hop-base.com')).toBe(true);
    expect(isHostUnder('nothop-base.com', 'hop-base.com')).toBe(false);
    expect(isHostUnder('hop-base.com.evil.example', 'hop-base.com')).toBe(false);
  });
});

describe('provisionalBrand', () => {
  it('域名能判定时以域名为准,不信记忆', () => {
    expect(provisionalBrand('api.hop-base.com', 'essevin')).toBe('hopbase');
    expect(provisionalBrand('localhost', null)).toBe('hopbase');
    expect(provisionalBrand('127.0.0.1', 'kite')).toBe('hopbase');
  });

  it('域名判定不了时用记住的,记忆非法则不猜', () => {
    expect(provisionalBrand('console.essevin.example', 'essevin')).toBe('essevin');
    expect(provisionalBrand('console.essevin.example', null)).toBeNull();
    expect(provisionalBrand('console.essevin.example', 'bad value!')).toBeNull();
    expect(provisionalBrand('nothop-base.com', null)).toBeNull();
  });
});

describe('defaultThemeForBrand', () => {
  it('HopBase 默认浅色,其余沿用深色', () => {
    expect(defaultThemeForBrand('hopbase')).toBe('light');
    expect(defaultThemeForBrand('essevin')).toBe('dark');
    expect(defaultThemeForBrand(null)).toBe('dark');
  });
});
