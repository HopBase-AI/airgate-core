import { get, post, put } from './client';
import type {
  SubscriptionResp,
  AssignSubscriptionReq, BulkAssignReq, AdjustSubscriptionReq,
  PlanResp, SubscriptionProgressResp, PurchaseSubscriptionReq,
  PageReq, PagedData,
} from '../types';

export const subscriptionsApi = {
  // 用户接口：套餐与自助购买
  plans: () => get<PlanResp[]>('/api/v1/account/plans'),
  active: () => get<SubscriptionResp[]>('/api/v1/account/subscriptions/active'),
  progress: () => get<SubscriptionProgressResp[]>('/api/v1/account/subscriptions/progress'),
  purchase: (data: PurchaseSubscriptionReq) => post<SubscriptionResp>('/api/v1/account/subscriptions/purchase', data),
  topup: (id: number) => post<SubscriptionResp>(`/api/v1/account/subscriptions/${id}/topup`),

  // 管理员接口
  adminList: (params: PageReq & { user_id?: number; group_id?: number; status?: string }) =>
    get<PagedData<SubscriptionResp>>('/api/v1/admin/subscriptions', params),
  assign: (data: AssignSubscriptionReq) => post<void>('/api/v1/admin/subscriptions/assign', data),
  bulkAssign: (data: BulkAssignReq) => post<void>('/api/v1/admin/subscriptions/bulk-assign', data),
  adjust: (id: number, data: AdjustSubscriptionReq) =>
    put<void>(`/api/v1/admin/subscriptions/${id}/adjust`, data),
};
