const STORAGE_KEY = 'ag_auth_return_to';

function trustedRoot(hostname: string): string {
  const host = hostname.toLowerCase();
  for (const root of ['essevin.com', 'hop-base.com']) {
    if (host === root || host.endsWith(`.${root}`)) return root;
  }
  return host;
}

/**
 * Only allow a login on one first-party subdomain to return to a public blog on
 * the same site family. This keeps the UX continuous without introducing an
 * open redirect through the public /login route.
 */
export function safeAuthReturnTo(raw: string | null | undefined, currentOrigin: string): string {
  if (!raw) return '';
  try {
    const current = new URL(currentOrigin);
    const target = new URL(raw, current);
    const secure = target.protocol === 'https:' || (target.protocol === 'http:' && target.hostname === current.hostname);
    if (!secure || trustedRoot(target.hostname) !== trustedRoot(current.hostname)) return '';
    if (!/^\/blog(?:\/|$)/.test(target.pathname)) return '';
    target.hash = '';
    return target.toString();
  } catch {
    return '';
  }
}

/** Capture return_to before password/OAuth navigation mutates the login URL. */
export function captureAuthReturnTo(): void {
  if (typeof window === 'undefined') return;
  try {
    const raw = new URLSearchParams(window.location.search).get('return_to');
    if (!raw) {
      const oauthCallback = /(?:^|[#&])oauth_token=/.test(window.location.hash)
        || new URLSearchParams(window.location.search).has('oauth_error');
      if (!oauthCallback) window.sessionStorage.removeItem(STORAGE_KEY);
      return;
    }
    const safe = safeAuthReturnTo(raw, window.location.origin);
    if (safe) window.sessionStorage.setItem(STORAGE_KEY, safe);
    else window.sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Restricted storage degrades to the normal dashboard destination.
  }
}

/** Read once after successful authentication so stale links cannot affect later logins. */
export function consumeAuthReturnTo(): string {
  if (typeof window === 'undefined') return '';
  try {
    const queryValue = new URLSearchParams(window.location.search).get('return_to');
    const storedValue = window.sessionStorage.getItem(STORAGE_KEY);
    window.sessionStorage.removeItem(STORAGE_KEY);
    return safeAuthReturnTo(queryValue || storedValue, window.location.origin);
  } catch {
    return '';
  }
}
