// 分销邀请码归因：推广链接经 ?inv=<code> 进入控制台（任意路由），首次捕获后写入
// localStorage 持久保存，注册（含 OAuth 发起）时随请求上报绑定邀请关系。
// 注意：?ref= 已被站点归因（originSite.ts）占用，邀请码固定用 ?inv=。
const STORAGE_KEY = 'ag_invite_code';

// 与后端 sanitizeInviteCode 保持一致：4~16 位字母数字。
const INVITE_CODE_PATTERN = /^[a-z0-9]{4,16}$/i;

function normalizeInviteCode(raw: string | null): string {
  const trimmed = (raw || '').trim();
  return INVITE_CODE_PATTERN.test(trimmed) ? trimmed.toLowerCase() : '';
}

/** 返回当前地址明确携带的邀请码；不会回退到历史暂存。 */
export function getInviteCodeFromURL(): string {
  try {
    return normalizeInviteCode(new URLSearchParams(window.location.search).get('inv'));
  } catch {
    return '';
  }
}

/** 从当前 URL 捕获邀请码并持久化；非法值静默忽略。 */
export function captureInviteCode(): void {
  try {
    const code = getInviteCodeFromURL();
    if (code) window.localStorage.setItem(STORAGE_KEY, code);
  } catch {
    // localStorage 不可用（隐私模式等）时静默降级为无归因
  }
}

/** 返回已捕获的邀请码，无则返回空串。 */
export function getInviteCode(): string {
  try {
    return window.localStorage.getItem(STORAGE_KEY) || '';
  } catch {
    return '';
  }
}

/**
 * 清除尚未消费的注册归因。
 * 仅应在确认进入一个已有/新建的用户账户会话后调用；登录或注册失败时必须保留，方便重试。
 */
export function clearInviteCode(): void {
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // localStorage 不可用时本来也无法持久化邀请码，静默降级即可。
  }
}
