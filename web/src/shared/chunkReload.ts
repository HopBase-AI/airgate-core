// 发版后哈希 chunk 被替换,已打开的旧页面切换路由时动态 import 404,
// 对用户表现为「页面出现了错误 / Failed to fetch dynamically imported module」。
// 这里统一识别此类错误并自动整页刷新一次拿新版本;sessionStorage 时间戳
// 护栏保证 60 秒内只自动刷一次——真故障(刷新后仍失败)交回错误 UI,不死循环。
const RELOAD_GUARD_KEY = 'ag_chunk_reload_at';
const RELOAD_GUARD_WINDOW_MS = 60_000;

export function isChunkLoadError(err: unknown): boolean {
  const msg = err instanceof Error ? err.message : String(err ?? '');
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS/i.test(msg);
}

// 触发整页刷新返回 true;护栏期内(刚刷过仍失败)返回 false。
export function tryReloadForStaleChunk(): boolean {
  let last = 0;
  try {
    last = Number(sessionStorage.getItem(RELOAD_GUARD_KEY) || 0);
  } catch {
    // sessionStorage 不可用(隐私模式等)时放弃护栏记录,仍允许刷新一次
  }
  const now = Date.now();
  if (last > 0 && now - last < RELOAD_GUARD_WINDOW_MS) return false;
  try {
    sessionStorage.setItem(RELOAD_GUARD_KEY, String(now));
  } catch {
    // 同上
  }
  window.location.reload();
  return true;
}
