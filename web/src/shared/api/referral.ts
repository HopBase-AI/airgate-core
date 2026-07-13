import { get, post, put } from './client';
import type { PagedData, PageReq } from '../types';

// 用户侧「我的邀请」概览
export interface MyReferralResp {
  invite_code: string;
  // 邀请链接前缀（后台可配）；空 = 用当前控制台域名
  link_base_url: string;
  enabled: boolean;
  invitee_count: number;
  total_rebate: number;
  total_reversed: number;
}

// 用户侧返利流水（被邀请人邮箱已脱敏）
export interface MyReferralCommissionResp {
  id: number;
  invitee_email: string;
  paid_amount: number;
  rate: number;
  amount: number;
  status: 'settled' | 'reversed';
  created_at: string;
  reversed_at?: string;
}

// 管理端返利流水（完整字段）
export interface ReferralCommissionResp {
  id: number;
  inviter_id: number;
  inviter_email: string;
  invitee_id: number;
  invitee_email: string;
  out_trade_no: string;
  kind: 'rebate' | 'first_bonus';
  paid_amount: number;
  rate: number;
  amount: number;
  status: 'settled' | 'reversed';
  created_at: string;
  reversed_at?: string;
}

// 推广官汇总行（对账报表）
export interface ReferralPromoterResp {
  user_id: number;
  email: string;
  username: string;
  referral_rate: number | null;
  invitee_count: number;
  total_rebate: number;
  total_reversed: number;
  first_bonus_total: number;
}

export const referralApi = {
  // 用户接口
  me: () => get<MyReferralResp>('/api/v1/referral/me'),
  myCommissions: (params: PageReq) =>
    get<PagedData<MyReferralCommissionResp>>('/api/v1/referral/commissions', params),

  // 管理员接口
  summary: () => get<ReferralPromoterResp[]>('/api/v1/admin/referral/summary'),
  commissions: (params: PageReq & { inviter_id?: number; invitee_id?: number; kind?: string; status?: string }) =>
    get<PagedData<ReferralCommissionResp>>('/api/v1/admin/referral/commissions', params),
  reverse: (id: number) =>
    post<ReferralCommissionResp>(`/api/v1/admin/referral/commissions/${id}/reverse`),
  // rate 传 null 清除用户级覆盖（回落全局默认）
  setUserRate: (userId: number, rate: number | null) =>
    put<void>(`/api/v1/admin/referral/users/${userId}/rate`, { rate }),
};
