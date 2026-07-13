// 分销邀请码归因：推广链接经 ?inv=<code> 进入控制台（任意路由），首次捕获后写入
// localStorage 持久保存，注册（含 OAuth 发起）时随请求上报绑定邀请关系。
// 注意：?ref= 已被站点归因（originSite.ts）占用，邀请码固定用 ?inv=。
const STORAGE_KEY = 'ag_invite_code';

// 与后端 sanitizeInviteCode 保持一致：4~16 位字母数字。
const INVITE_CODE_PATTERN = /^[a-z0-9]{4,16}$/i;

/** 从当前 URL 捕获邀请码并持久化；非法值静默忽略。 */
export function captureInviteCode(): void {
  try {
    const params = new URLSearchParams(window.location.search);
    const raw = (params.get('inv') || '').trim();
    if (raw && INVITE_CODE_PATTERN.test(raw)) {
      window.localStorage.setItem(STORAGE_KEY, raw.toLowerCase());
    }
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
