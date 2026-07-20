// 博客注册墙只需要知道「当前浏览器是否已有有效控制台会话」，不承担鉴权。
// JWT 仍只保存在控制台 localStorage；这里只把一个无敏感信息的 UX 标记同步到
// 根域 Cookie，让 essevin.com 的文章页能感知 api/console 子域的登录与退出。
const COOKIE_NAME = 'airgate_reader_session';
const DEFAULT_MAX_AGE = 24 * 60 * 60;
const MAX_MAX_AGE = 365 * 24 * 60 * 60;

function sharedCookieDomain(hostname: string): string {
  const host = hostname.toLowerCase();
  if (host === 'essevin.com' || host.endsWith('.essevin.com')) return '.essevin.com';
  if (host === 'hop-base.com' || host.endsWith('.hop-base.com')) return '.hop-base.com';
  return '';
}

function markerMaxAge(expiresAt?: number): number {
  if (!expiresAt || !Number.isFinite(expiresAt)) return DEFAULT_MAX_AGE;
  return Math.max(0, Math.min(MAX_MAX_AGE, Math.floor(expiresAt - Date.now() / 1000)));
}

export function syncBlogReaderSession(active: boolean, expiresAt?: number) {
  if (typeof document === 'undefined' || typeof window === 'undefined') return;

  const domain = sharedCookieDomain(window.location.hostname);
  const secure = window.location.protocol === 'https:' ? '; secure' : '';
  const domainPart = domain ? `; domain=${domain}` : '';
  const maxAge = active ? markerMaxAge(expiresAt) : 0;
  const value = active && maxAge > 0 ? '1' : '';

  document.cookie = `${COOKIE_NAME}=${value}; path=/; max-age=${maxAge}; samesite=lax${secure}${domainPart}`;
}
