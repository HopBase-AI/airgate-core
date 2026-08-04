import { post } from './client';
import type { LoginReq, LoginResp, RegisterReq, RefreshResp, APIKeyLoginReq, APIKeyLoginResp } from '../types';

export const authApi = {
  login: (data: LoginReq, signal?: AbortSignal) =>
    post<LoginResp>('/api/v1/auth/login', data, { signal }),
  loginByAPIKey: (data: APIKeyLoginReq, signal?: AbortSignal) =>
    post<APIKeyLoginResp>('/api/v1/auth/login-apikey', data, { signal }),
  register: (data: RegisterReq, signal?: AbortSignal) =>
    post<LoginResp>('/api/v1/auth/register', data, { signal }),
  refresh: () => post<RefreshResp>('/api/v1/auth/refresh'),
  sendVerifyCode: (email: string) => post<void>('/api/v1/auth/send-verify-code', { email }),
  verifyCode: (email: string, code: string) => post<void>('/api/v1/auth/verify-code', { email, code }),
};
