import { get, post } from './client';

// 一键接入（Claude Code）：签发一次性 setup token + 轮询接入状态。
// 后端对应 /api/v1/oneclick/*（JWT 鉴权）；脚本侧的 /oneclick/* 公开端点前端不直接调用。

export interface OneClickIssueResp {
  token: string;
  expires_in_seconds: number;
  base_url: string;
  command_bash: string;
  command_powershell: string;
  command_codex_bash: string;
  command_codex_powershell: string;
}

export type OneClickStatus = 'pending' | 'exchanged' | 'verified' | 'expired';

export interface OneClickStatusResp {
  status: OneClickStatus;
}

export const oneclickApi = {
  issue: (keyId: number) =>
    post<OneClickIssueResp>('/api/v1/oneclick/setup-token', { key_id: keyId }),
  status: (token: string) =>
    get<OneClickStatusResp>(`/api/v1/oneclick/setup-token/${token}`),
};
