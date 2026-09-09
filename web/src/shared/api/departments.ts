import { get, post, put, del } from './client';
import type {
  CreateDepartmentReq,
  DepartmentResp,
  PageReq,
  PagedData,
  TeamAuditLogResp,
  TeamOverviewResp,
  UpdateDepartmentReq,
} from '../types';

type RequestOptions = { signal?: AbortSignal };

export type TeamAuditListParams = PageReq & {
  target_type?: 'department' | 'member' | 'apikey' | 'team';
  target_id?: number;
  action?: string;
  start_date?: string;
  end_date?: string;
  tz?: string;
};

// 企业组织（部门）+ 企业总览 / 账期 / 操作审计：企业主侧接口
export const departmentsApi = {
  list: (params?: PageReq, options?: RequestOptions) =>
    get<PagedData<DepartmentResp>>('/api/v1/departments', params, options),
  create: (data: CreateDepartmentReq) => post<DepartmentResp>('/api/v1/departments', data),
  update: (id: number, data: UpdateDepartmentReq) => put<DepartmentResp>(`/api/v1/departments/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/departments/${id}`),
  resetPeriod: (id: number) => post<DepartmentResp>(`/api/v1/departments/${id}/reset-period`),
  overview: (options?: RequestOptions) => get<TeamOverviewResp>('/api/v1/team/overview', undefined, options),
  updateBillingPeriod: (billingDay: number) => put<TeamOverviewResp>('/api/v1/team/billing-period', { billing_day: billingDay }),
  auditLogs: (params: TeamAuditListParams, options?: RequestOptions) =>
    get<PagedData<TeamAuditLogResp>>('/api/v1/team/audit-logs', params, options),
};
