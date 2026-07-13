// 来源站点归因：ToC 落地页跳转控制台时通过 ?site=<siteId>（兼容 ?ref=）标识来源站。
// 首次捕获后写入 localStorage 持久保存，供品牌解析（登录页/AppShell logo 跟随来源站）
// 与注册归因（source_site 落库 users.signup_source）使用。
const STORAGE_KEY = 'ag_origin_site';

// 与后端 sanitizeSiteID 保持一致：字母数字开头，可含 - _，最长 64。
const SITE_ID_PATTERN = /^[a-z0-9][a-z0-9_-]{0,63}$/i;

/** 从当前 URL 捕获来源站点参数并持久化；非法值静默忽略。 */
export function captureOriginSite(): void {
  try {
    const params = new URLSearchParams(window.location.search);
    const raw = (params.get('site') || params.get('ref') || '').trim();
    if (raw && SITE_ID_PATTERN.test(raw)) {
      window.localStorage.setItem(STORAGE_KEY, raw.toLowerCase());
    }
  } catch {
    // localStorage 不可用（隐私模式等）时静默降级为无来源
  }
}

/** 返回已捕获的来源站点 ID，无来源返回空串。 */
export function getOriginSite(): string {
  try {
    return window.localStorage.getItem(STORAGE_KEY) || '';
  } catch {
    return '';
  }
}

// adoptOriginSite 在 React 渲染之后才可能补写来源，订阅机制让品牌解析（useSyncExternalStore）随之重算。
const listeners = new Set<() => void>();

export function subscribeOriginSite(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/**
 * 用登录用户的注册归因（users.signup_source）兜底来源站：换设备/清缓存后 localStorage
 * 里没有 ?site= 归因时，品牌与文档链接仍能跟随用户注册时的来源站。
 * 本次已带显式来源（?site= 进入）时不覆盖。
 */
export function adoptOriginSite(site: string | undefined | null): void {
  const normalized = (site ?? '').trim().toLowerCase();
  if (!normalized || !SITE_ID_PATTERN.test(normalized)) return;
  try {
    if (window.localStorage.getItem(STORAGE_KEY)) return;
    window.localStorage.setItem(STORAGE_KEY, normalized);
    listeners.forEach((listener) => listener());
  } catch {
    // localStorage 不可用（隐私模式等）时静默降级为无来源
  }
}
