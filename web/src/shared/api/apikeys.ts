import { get, post, put, del } from './client';
import type { APIKeyResp, CreateAPIKeyReq, UpdateAPIKeyReq, PageReq, PagedData } from '../types';

type APIKeyRequestOptions = {
  signal?: AbortSignal;
};

type APIKeyListParams = PageReq & {
  search_scope?: 'api_key';
  /** 只看某个团队成员名下的 key */
  member_id?: number;
  /** 只看未归属团队成员的 key（与 member_id 互斥） */
  member_unassigned?: boolean;
  group_id?: number;
  /** 展示态口径：过期优先于启用/停用 */
  status?: 'active' | 'disabled' | 'expired';
};

export const apikeysApi = {
  // 用户接口
  list: (params?: APIKeyListParams, options?: APIKeyRequestOptions) =>
    get<PagedData<APIKeyResp>>('/api/v1/api-keys', params, options),
  create: (data: CreateAPIKeyReq) => post<APIKeyResp>('/api/v1/api-keys', data),
  update: (id: number, data: UpdateAPIKeyReq) => put<void>(`/api/v1/api-keys/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/api-keys/${id}`),
  reveal: (id: number) => get<APIKeyResp>(`/api/v1/api-keys/${id}/reveal`),

  // 管理员接口
  adminList: (params?: APIKeyListParams, options?: APIKeyRequestOptions) =>
    get<PagedData<APIKeyResp>>('/api/v1/admin/api-keys', params, options),
  adminUpdate: (id: number, data: UpdateAPIKeyReq) =>
    put<void>(`/api/v1/admin/api-keys/${id}`, data),
};
