import type { UserResp } from './types';

// 公开博客与控制台通常位于同一主域的不同子域（例如 essevin.com 与
// console.essevin.com）。localStorage 无法跨源读取，因此只用父域 Cookie 同步一份
// 不含 Token 的展示提示；博客仍会通过控制台 session bridge 校验真实会话。
export const BLOG_SESSION_COOKIE = 'airgate_blog_session_v1';

interface BlogSessionHint {
  v: 1;
  name: string;
  email: string;
  exp: number;
}

interface TokenPayload {
  exp?: number;
}

let inMemoryHint: BlogSessionHint | null = null;

function tokenExpiry(token: string): number {
  const payload = token.split('.')[1];
  if (!payload) return Math.floor(Date.now() / 1000) + 24 * 60 * 60;

  try {
    const base64 = payload.replace(/-/g, '+').replace(/_/g, '/');
    const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, '=');
    const decoded = Uint8Array.from(atob(padded), (char) => char.charCodeAt(0));
    const parsed = JSON.parse(new TextDecoder().decode(decoded)) as TokenPayload;
    return typeof parsed.exp === 'number'
      ? parsed.exp
      : Math.floor(Date.now() / 1000) + 24 * 60 * 60;
  } catch {
    return Math.floor(Date.now() / 1000) + 24 * 60 * 60;
  }
}

function sharedCookieDomain(hostname: string): string | null {
  const host = hostname.toLowerCase().replace(/^\.+|\.+$/g, '');
  if (!host || host === 'localhost' || host.includes(':') || /^\d+(?:\.\d+){3}$/.test(host)) {
    return null;
  }

  // 控制台部署约定为主域的一层子域：console.essevin.com / api.hop-base.com。
  // 去掉这一层即可覆盖主站和同主域博客；两段式主机名则保留 host-only Cookie。
  const labels = host.split('.');
  return labels.length >= 3 ? labels.slice(1).join('.') : null;
}

function cookieAttributes(maxAge: number, domain?: string, path = '/blog'): string {
  const secure = window.location.protocol === 'https:' ? '; Secure' : '';
  const domainAttr = domain ? `; Domain=${domain}` : '';
  return `; Path=${path}; Max-Age=${Math.max(0, Math.floor(maxAge))}; SameSite=Lax${secure}${domainAttr}`;
}

function clearCookieVariants(domain?: string) {
  document.cookie = `${BLOG_SESSION_COOKIE}=${cookieAttributes(0, domain, '/blog')}`;
  // 清理会话桥旧版本写到根路径的 Cookie，避免退出后博客继续读到旧头像。
  document.cookie = `${BLOG_SESSION_COOKIE}=${cookieAttributes(0, domain, '/')}`;
}

function readBlogSessionHint(): BlogSessionHint | null {
  if (inMemoryHint) return inMemoryHint;
  if (typeof document === 'undefined') return null;
  try {
    const prefix = `${BLOG_SESSION_COOKIE}=`;
    const raw = document.cookie.split(';').map((part) => part.trim())
      .find((part) => part.startsWith(prefix))?.slice(prefix.length);
    if (!raw) return null;
    const parsed = JSON.parse(decodeURIComponent(raw)) as BlogSessionHint;
    return parsed.v === 1 && typeof parsed.exp === 'number' ? parsed : null;
  } catch {
    return null;
  }
}

function writeBlogSessionHint(hint: BlogSessionHint) {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  const maxAge = hint.exp - Math.floor(Date.now() / 1000);
  if (maxAge <= 0) {
    clearBlogSession();
    return;
  }

  const value = encodeURIComponent(JSON.stringify(hint));
  inMemoryHint = hint;
  const domain = sharedCookieDomain(window.location.hostname);
  clearCookieVariants();
  if (domain) clearCookieVariants(domain);
  document.cookie = `${BLOG_SESSION_COOKIE}=${value}${cookieAttributes(maxAge, domain ?? undefined)}`;
}

export function syncBlogSession(user: UserResp, token: string) {
  const email = user.email?.trim() ?? '';
  const name = user.username?.trim() || user.api_key_name?.trim() || email.split('@')[0] || 'User';
  writeBlogSessionHint({
    v: 1,
    name: name.slice(0, 80),
    email: email.slice(0, 160),
    exp: tokenExpiry(token),
  });
}

// 静默刷新 Token 时 AuthProvider 不会重新拉取用户资料；沿用现有展示信息，仅更新到期时间。
export function refreshBlogSessionExpiry(token: string) {
  const current = readBlogSessionHint();
  if (!current) return;
  writeBlogSessionHint({ ...current, exp: tokenExpiry(token) });
}

export function clearBlogSession() {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  inMemoryHint = null;
  clearCookieVariants();
  const domain = sharedCookieDomain(window.location.hostname);
  if (domain) clearCookieVariants(domain);
}
