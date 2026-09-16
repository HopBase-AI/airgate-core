import { get, getToken } from './client';
import { acceptLanguage } from '../../i18n';
import type { UsageLogResp, UserUsageLogResp, CustomerUsageLogResp, UsageQuery, UsageStatsResp, UsageTrendBucket, PagedData } from '../types';

type UsageRequestOptions = {
  signal?: AbortSignal;
};

export const usageApi = {
  // 用户接口
  list: (params: UsageQuery, options?: UsageRequestOptions) =>
    get<PagedData<UserUsageLogResp | CustomerUsageLogResp>>('/api/v1/usage', params, options),
  userStats: (params: Omit<UsageQuery, 'page' | 'page_size'> & { breakdown?: string }, options?: UsageRequestOptions) =>
    get<UsageStatsResp>('/api/v1/usage/stats', params, options),
  userTrend: (params: { granularity: string; start_date?: string; end_date?: string; api_key_id?: number; member_id?: number; department_id?: number }, options?: UsageRequestOptions) =>
    get<UsageTrendBucket[]>('/api/v1/usage/trend', params, options),

  // 导出当前筛选范围内的使用明细 CSV。
  // 后端要求 start_time(RFC3339)，end_time 缺省为「现在」；筛选参数与列表页同名同义，
  // 所以「页面上筛了哪个成员，导出就只有那个成员」。
  // 返回 CSV 而非 JSON，不能走 get<T>：这里自己 fetch 拿 blob，并沿用同一套鉴权头。
  // 表头 / 说明栏 / 汇总行由后端按 Accept-Language 渲染（i18n.Tc(c, "export.*")，缺省英文），
  // 所以必须显式带上控制台当前的界面语言：不带的话浏览器的 Accept-Language 说了算，
  // 「控制台切成日文、浏览器是 zh-CN」的用户会拿到一份中文表头的账单。
  // 同口径修复见 airgate-epay #11。
  exportCsv: async (params: {
    start_time: string;
    end_time?: string;
    api_key_id?: number;
    member_id?: number;
    department_id?: number;
    tz?: string;
  }): Promise<{ blob: Blob; filename: string }> => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== '') query.set(key, String(value));
    });
    const token = getToken();
    const headers: Record<string, string> = { 'Accept-Language': acceptLanguage() };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const resp = await fetch(`/api/v1/usage/export?${query.toString()}`, { headers });
    if (!resp.ok) {
      // 失败时后端回的是 JSON 错误体，尽量把 message 抛出去而不是丢个裸状态码
      let message = `HTTP ${resp.status}`;
      try {
        const body: unknown = await resp.json();
        if (body && typeof body === 'object' && 'message' in body && typeof body.message === 'string') {
          message = body.message;
        }
      } catch {
        // 非 JSON 响应体，保留状态码文案
      }
      throw new Error(message);
    }
    const disposition = resp.headers.get('Content-Disposition') ?? '';
    const matched = /filename="?([^";]+)"?/.exec(disposition);
    return { blob: await resp.blob(), filename: matched?.[1] ?? 'usage.csv' };
  },

  // 管理员接口
  adminList: (params: UsageQuery, options?: UsageRequestOptions) =>
    get<PagedData<UsageLogResp>>('/api/v1/admin/usage', params, options),
  stats: (params: { group_by: string; start_date?: string; end_date?: string; platform?: string; model?: string; user_id?: number; api_key_id?: number }, options?: UsageRequestOptions) =>
    get<UsageStatsResp>('/api/v1/admin/usage/stats', params, options),
  trend: (params: { granularity: string; start_date?: string; end_date?: string; platform?: string; model?: string; user_id?: number; api_key_id?: number }, options?: UsageRequestOptions) =>
    get<UsageTrendBucket[]>('/api/v1/admin/usage/trend', params, options),
};
