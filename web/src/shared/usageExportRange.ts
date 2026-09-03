// 用量筛选的时间值 → 导出端点要的 RFC3339。
//
// 筛选控件给的是本地时间串（UsageDateRangeFilter 是秒粒度，形如 "2026-09-03T14:30:05"；
// 历史值/预设可能是纯日期 "2026-09-03"）。纯日期按本地零点 / 当日 23:59:59 补齐，
// 再由 Date 按本地时区解析成 UTC——纯日期串若直接交给 Date 会被当成 UTC，差一个时区。
//
// 右边界另有一层：导出端点是左闭右开 [start, end)（给充值区间用，相邻两笔不能重复计），
// 而列表的右界含所选那一秒。所以导出时把右界推后 1 秒，两边的集合才一致。

/** 导出端点允许的最长区间（后端 exportMaxWindow = 400 天）。 */
export const EXPORT_MAX_WINDOW_MS = 400 * 24 * 60 * 60 * 1000;

function parseLocal(raw: string | undefined, fallbackTime: string): Date | null {
  if (!raw) return null;
  const normalized = raw.includes('T') ? raw : `${raw}T${fallbackTime}`;
  const parsed = new Date(normalized);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

export function usageFilterStartToRFC3339(raw: string | undefined): string | undefined {
  return parseLocal(raw, '00:00:00')?.toISOString();
}

export function usageFilterEndToRFC3339(raw: string | undefined): string | undefined {
  const parsed = parseLocal(raw, '23:59:59');
  if (!parsed) return undefined;
  return new Date(parsed.getTime() + 1000).toISOString();
}
