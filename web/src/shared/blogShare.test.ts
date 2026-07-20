import { describe, expect, it } from 'vitest';
import { buildBlogShareURL, publicBlogBase } from './blogShare';

describe('blog share URLs', () => {
  it('maps API and console origins back to the public first-party blog', () => {
    expect(publicBlogBase('https://api.essevin.com')).toBe('https://essevin.com');
    expect(publicBlogBase('https://console.essevin.com/')).toBe('https://essevin.com');
    expect(publicBlogBase('https://late.essevin.com')).toBe('https://late.essevin.com');
    expect(publicBlogBase('not a URL/')).toBe('not a URL');
  });

  it.each([
    ['zh-Hant', '繁體文章'],
    ['en', 'English article'],
    ['zh', '简体文章'],
  ] as const)('keeps the %s language key and promoter invite code', (lang, slug) => {
    const url = new URL(buildBlogShareURL('https://essevin.com/', slug, lang, '  Vip8  '));
    expect(url.origin).toBe('https://essevin.com');
    expect(decodeURIComponent(url.pathname)).toBe(`/blog/${slug}`);
    expect(url.searchParams.get('lang')).toBe(lang);
    expect(url.searchParams.get('inv')).toBe('Vip8');
  });

  it('defaults to Traditional Chinese and omits a blank invite code', () => {
    const url = new URL(buildBlogShareURL('https://essevin.com', 'default-post', undefined, '  '));
    expect(url.searchParams.get('lang')).toBe('zh-Hant');
    expect(url.searchParams.has('inv')).toBe(false);
  });
});
