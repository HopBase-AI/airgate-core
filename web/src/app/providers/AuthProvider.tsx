import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { UserResp } from '../../shared/types';
import {
  setToken,
  getToken,
  setSessionAPIKey,
  getTokenAPIKeyID,
  getTokenRole,
  isSameTokenSession,
  ApiError,
} from '../../shared/api/client';
import { isExplicitSessionRejection } from '../../shared/authSessionPolicy';
import { recoverAuthSession } from '../../shared/authSessionRecovery';
import { usersApi } from '../../shared/api/users';
import { syncBlogSession } from '../../shared/blogSession';
import { adoptOriginSite } from '../../shared/originSite';
import { clearInviteCode } from '../../shared/inviteCode';
import { resetAdminCache } from '../routeGuards';
import {
  clearAllOnboardingSessions,
  clearOtherOnboardingSessions,
} from '../../shared/onboarding/storage';

interface AuthContextType {
  user: UserResp | null;
  loading: boolean;
  /** 是否为 API Key 登录 */
  isAPIKeySession: boolean;
  login: (token: string, user: UserResp) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  isAPIKeySession: false,
  login: () => {},
  logout: () => {},
});

function normalizeSessionUser(user: UserResp, token = getToken()): UserResp {
  const role = getTokenRole(token);
  const apiKeyID = getTokenAPIKeyID(token);
  const effectiveRole: UserResp['role'] = apiKeyID ? 'api_key' : (role ?? user.role);

  return {
    ...user,
    role: effectiveRole,
    ...(apiKeyID ? { api_key_id: apiKeyID } : {}),
  };
}

// 邀请码是「待完成的新账户归因」而非长期偏好。确认进入普通用户/管理员账户后即结束本次归因；
// API Key 只读会话不代表用户已选择登录或注册账户，不能误清。
function clearSettledInviteAttribution(token: string) {
  if (getTokenRole(token) !== 'api_key') clearInviteCode();
}

function syncAccountOnlySessionState(user: UserResp, token: string) {
  if (getTokenRole(token) === 'api_key') return;
  clearOtherOnboardingSessions(user.id);
  syncBlogSession(user, token);
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<UserResp | null>(null);
  const [loading, setLoading] = useState(true);
  const authRevisionRef = useRef(0);
  const authRecoveryControllerRef = useRef<AbortController | null>(null);

  // 换设备/清缓存后 localStorage 无 ?site= 归因时，用注册归因兜底来源站，
  // 让品牌与文档链接仍跟随该用户注册时的落地页。
  useEffect(() => {
    if (user?.signup_source) adoptOriginSite(user.signup_source);
  }, [user?.signup_source]);

  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();
    const revision = authRevisionRef.current;
    const token = getToken();
    if (token) {
      authRecoveryControllerRef.current = controller;
      void recoverAuthSession({
        load: usersApi.me,
        signal: controller.signal,
        isExplicitRejection: (error) => error instanceof ApiError
          && isExplicitSessionRejection(error.httpStatus),
      }).then((result) => {
        if (authRecoveryControllerRef.current === controller) {
          authRecoveryControllerRef.current = null;
        }
        if (result.kind === 'cancelled' || cancelled || authRevisionRef.current !== revision) return;
        const currentToken = getToken();
        if (result.kind === 'restored') {
          if (currentToken && isSameTokenSession(token, currentToken)) {
            clearSettledInviteAttribution(currentToken);
            const sessionUser = normalizeSessionUser(result.value, currentToken);
            syncAccountOnlySessionState(sessionUser, currentToken);
            setUser(sessionUser);
            setLoading(false);
          }
          return;
        }
        if (result.kind === 'rejected' && (
          currentToken === null || isSameTokenSession(token, currentToken)
        )) {
          resetAdminCache();
          setToken(null);
          clearAllOnboardingSessions();
          setUser(null);
          setLoading(false);
        }
      });
    } else {
      setLoading(false);
    }

    return () => {
      cancelled = true;
      if (authRecoveryControllerRef.current === controller) {
        authRecoveryControllerRef.current = null;
      }
      controller.abort();
    };
  }, []);

  const login = useCallback((token: string, userData: UserResp) => {
    authRecoveryControllerRef.current?.abort();
    authRecoveryControllerRef.current = null;
    authRevisionRef.current += 1;
    const revision = authRevisionRef.current;
    resetAdminCache();
    setToken(token);
    clearSettledInviteAttribution(token);
    const sessionUser = normalizeSessionUser(userData, token);
    syncAccountOnlySessionState(sessionUser, token);
    setUser(sessionUser);
    setLoading(false);
    // 登录响应可能不包含全部用户字段（例如 API Key 登录时缺少 quota / expires_at），
    // 异步用 /me 拉一次完整数据补齐，避免首屏额度等信息显示不准。
    usersApi.me()
      .then((freshUser) => {
        const currentToken = getToken();
        if (
          authRevisionRef.current === revision
          && currentToken
          && isSameTokenSession(token, currentToken)
        ) {
          const freshSessionUser = normalizeSessionUser(freshUser, currentToken);
          syncAccountOnlySessionState(freshSessionUser, currentToken);
          setUser(freshSessionUser);
        }
      })
      .catch(() => {});
  }, []);

  const logout = useCallback(() => {
    authRecoveryControllerRef.current?.abort();
    authRecoveryControllerRef.current = null;
    authRevisionRef.current += 1;
    setToken(null);
    setSessionAPIKey(null);
    clearAllOnboardingSessions();
    setUser(null);
    resetAdminCache();
    window.location.href = '/login';
  }, []);

  const currentToken = getToken();
  const isAPIKeySession = getTokenRole(currentToken) === 'api_key'
    || !!getTokenAPIKeyID(currentToken)
    || user?.role === 'api_key'
    || !!(user?.api_key_id && user.api_key_id > 0);
  const value = useMemo(
    () => ({ user, loading, isAPIKeySession, login, logout }),
    [isAPIKeySession, loading, login, logout, user],
  );

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
