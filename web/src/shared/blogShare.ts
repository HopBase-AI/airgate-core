import type { BlogLanguage } from './types';

const DEFAULT_BLOG_LANGUAGE: BlogLanguage = 'zh-Hant';

export function publicBlogBase(origin: string): string {
  try {
    const url = new URL(origin);
    url.hostname = url.hostname.replace(/^(?:api|console)\./, '');
    return url.origin;
  } catch {
    return origin.replace(/\/+$/, '');
  }
}

export function buildBlogShareURL(
  base: string,
  slug: string,
  lang: BlogLanguage = DEFAULT_BLOG_LANGUAGE,
  inviteCode?: string,
): string {
  const query = new URLSearchParams();
  if (inviteCode?.trim()) query.set('inv', inviteCode.trim());
  query.set('lang', lang || DEFAULT_BLOG_LANGUAGE);
  return `${base.replace(/\/+$/, '')}/blog/${encodeURIComponent(slug)}?${query.toString()}`;
}
