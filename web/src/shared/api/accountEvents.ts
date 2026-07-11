import { get } from './client';
import type { AccountEventResp, PageReq, PagedData } from '../types';

export const accountEventsApi = {
  // 管理员接口：账号异常事件流（异常监控页）
  list: (params: PageReq & {
    account_id?: number;
    group_id?: number;
    event_type?: string;
    platform?: string;
  }) => get<PagedData<AccountEventResp>>('/api/v1/admin/account-events', params),
};
