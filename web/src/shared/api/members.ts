import { get, post, put, del } from './client';
import type { MemberResp, CreateMemberReq, UpdateMemberReq, PageReq, PagedData } from '../types';

type MemberRequestOptions = {
  signal?: AbortSignal;
};

type MemberListParams = PageReq & {
  status?: 'active' | 'disabled';
};

// 团队成员（企业子账号）：主账号侧接口
export const membersApi = {
  list: (params?: MemberListParams, options?: MemberRequestOptions) =>
    get<PagedData<MemberResp>>('/api/v1/members', params, options),
  create: (data: CreateMemberReq) => post<MemberResp>('/api/v1/members', data),
  update: (id: number, data: UpdateMemberReq) => put<MemberResp>(`/api/v1/members/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/members/${id}`),
  resetPeriod: (id: number) => post<MemberResp>(`/api/v1/members/${id}/reset-period`),
};
