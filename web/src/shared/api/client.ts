import type { ApiResponse, SessionRole } from '../types';
import i18n from '../../i18n';
import { syncBlogReaderSession } from '../blogReaderSession';
import { clearBlogSession, refreshBlogSessionExpiry } from '../blogSession';
import { isExplicitRefreshRejection } from '../authSessionPolicy';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || '';

// 需要绕过 request() 自己发请求的场景（如导出 CSV 拿 blob）复用同一个 base，
// 免得两条路径的地址前缀不一致。
export const API_BASE_URL = BASE_URL;
const API_KEY_SECRET_STORAGE = 'apikey_session_secret';

// Token 管理
function readBrowserStorage(kind: 'localStorage' | 'sessionStorage', key: string): string | null {
  if (typeof window === 'undefined') return null;
  try {
    return window[kind].getItem(key);
  } catch {
    return null;
  }
}

function writeBrowserStorage(kind: 'localStorage' | 'sessionStorage', key: string, value: string | null) {
  if (typeof window === 'undefined') return;
  try {
    if (value == null) window[kind].removeItem(key);
    else window[kind].setItem(key, value);
  } catch {
    // Storage can be unavailable in private mode or locked-down browsers.
  }
}

let accessToken: string | null = readBrowserStorage('localStorage', 'token');
let sessionEpoch = Symbol('auth-session');

interface TokenClaims {
  user_id?: number;
  role?: string;
  api_key_id?: number;
  exp?: number;
}

function persistToken(token: string | null) {
  const previousAPIKeyID = getTokenRole(accessToken) === 'api_key'
    ? getTokenAPIKeyID(accessToken)
    : null;
  const nextAPIKeyID = getTokenRole(token) === 'api_key'
    ? getTokenAPIKeyID(token)
    : null;
  if (previousAPIKeyID === null || previousAPIKeyID !== nextAPIKeyID) {
    setSessionAPIKey(null);
  }
  accessToken = token;
  writeBrowserStorage('localStorage', 'token', token);
  syncBlogReaderSession(!!token, getTokenClaims(token)?.exp);
  if (token) refreshBlogSessionExpiry(token);
  else {
    clearBlogSession();
    setSessionAPIKey(null);
  }
}

// Login/logout always starts a new client session, even when the JWT belongs to
// the same user. Only a successful refresh may preserve the current epoch.
export function setToken(token: string | null) {
  sessionEpoch = Symbol('auth-session');
  persistToken(token);
}

export function getToken(): string | null {
  return accessToken;
}

export type SessionIdentity = symbol;

export function getSessionIdentity(): SessionIdentity {
  return sessionEpoch;
}

export function isSessionIdentityCurrent(identity: SessionIdentity): boolean {
  return sessionEpoch === identity;
}

export function clearTokenIfSessionCurrent(identity: SessionIdentity): boolean {
  if (!isSessionIdentityCurrent(identity)) return false;
  setToken(null);
  return true;
}

// A login or registration submit owns a fresh anonymous epoch immediately,
// so an older auth flow cannot win while the newer request is still pending.
export function beginAuthenticationAttempt(): SessionIdentity {
  setToken(null);
  return sessionEpoch;
}

export function getTokenClaims(token = accessToken): TokenClaims | null {
  if (!token) return null;

  const payload = token.split('.')[1];
  if (!payload) return null;

  try {
    const base64 = payload.replace(/-/g, '+').replace(/_/g, '/');
    const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, '=');
    const json = new TextDecoder().decode(
      Uint8Array.from(atob(padded), (char) => char.charCodeAt(0)),
    );
    return JSON.parse(json) as TokenClaims;
  } catch {
    return null;
  }
}

export function getTokenRole(token = accessToken): SessionRole | null {
  const role = getTokenClaims(token)?.role;
  return role === 'admin' || role === 'user' || role === 'api_key' ? role : null;
}

export function getTokenAPIKeyID(token = accessToken): number | null {
  const id = getTokenClaims(token)?.api_key_id;
  return typeof id === 'number' && id > 0 ? id : null;
}

// Token identity is used for display/session metadata only. Request replay uses
// the stricter client session epoch below so a same-user login is never treated
// as a token refresh.
export function isSameTokenSession(left: string | null, right: string | null): boolean {
  if (!left || !right) return false;
  if (left === right) return true;
  const leftClaims = getTokenClaims(left);
  const rightClaims = getTokenClaims(right);
  if (!leftClaims || !rightClaims || leftClaims.role !== rightClaims.role) return false;
  if (leftClaims.role === 'api_key') {
    return typeof leftClaims.api_key_id === 'number'
      && leftClaims.api_key_id > 0
      && leftClaims.api_key_id === rightClaims.api_key_id;
  }
  return typeof leftClaims.user_id === 'number'
    && leftClaims.user_id > 0
    && leftClaims.user_id === rightClaims.user_id;
}

// 兼容升级前已登录的浏览器：新 bundle 首次启动时即可补写跨子域阅读标记。
syncBlogReaderSession(!!accessToken, getTokenClaims(accessToken)?.exp);
if (!accessToken) {
  clearBlogSession();
}
if (getTokenRole(accessToken) !== 'api_key') setSessionAPIKey(null);

// API Key 登录场景下用户输入的原文 Key，仅保留在 sessionStorage 内，
// 退出登录或关闭浏览器即清除。供 CCS 导入等需要原文 Key 的客户端功能使用。
export function setSessionAPIKey(key: string | null) {
  writeBrowserStorage('sessionStorage', API_KEY_SECRET_STORAGE, key);
}

export function getSessionAPIKey(): string | null {
  return readBrowserStorage('sessionStorage', API_KEY_SECRET_STORAGE);
}

// 查询参数类型
type QueryParams = Record<string, any>;
type RequestOptions = {
  signal?: AbortSignal;
};

// 当前浏览器时区（IANA 名，例如 "Asia/Shanghai"、"America/New_York"）。
// 自动附加到 GET 请求，保证后端按用户本地时区计算"今天 / 7 天"等边界。
function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || '';
  } catch {
    return '';
  }
}

// 构建请求头
function buildHeaders(includeContentType: boolean, token = accessToken): Record<string, string> {
  const headers: Record<string, string> = {};
  if (includeContentType) {
    headers['Content-Type'] = 'application/json';
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

// Token 自动刷新：在过期前 30 分钟内首次请求时静默续期。
// 明确失效和临时故障必须分开，避免 DB/网络抖动把有效会话永久删除。
type RefreshResult =
  | { kind: 'refreshed'; token: string }
  | { kind: 'rejected'; error: ApiError }
  | { kind: 'retryable'; error: ApiError }
  | { kind: 'superseded'; error: SessionSupersededError };

const REFRESH_TIMEOUT_MS = 15_000;

let refreshInFlight: {
  token: string;
  epoch: symbol;
  id: symbol;
  promise: Promise<RefreshResult>;
} | null = null;

function tokenExpiresWithin(token: string, seconds: number): boolean {
  const claims = getTokenClaims(token);
  if (!claims?.exp) return false;
  return claims.exp - Date.now() / 1000 < seconds;
}

function sessionSupersededError(): SessionSupersededError {
  return new SessionSupersededError();
}

function supersededRefresh(): RefreshResult {
  return { kind: 'superseded', error: sessionSupersededError() };
}

function refreshFailure(error: ApiError): RefreshResult {
  return isExplicitRefreshRejection(error.httpStatus)
    ? { kind: 'rejected', error }
    : { kind: 'retryable', error };
}

async function refreshResponseError(res: Response): Promise<ApiError> {
  try {
    const json = await res.json() as Partial<ApiResponse<unknown>>;
    return new ApiError(
      typeof json.code === 'number' ? json.code : -1,
      typeof json.message === 'string'
        ? json.message
        : i18n.t('common.server_error', { status: res.status }),
      res.status,
    );
  } catch {
    return new ApiError(-1, i18n.t('common.server_error', { status: res.status }), res.status);
  }
}

function refreshSessionIsCurrent(token: string, epoch: symbol): boolean {
  return accessToken === token && sessionEpoch === epoch;
}

function replaceRefreshedToken(token: string, epoch: symbol, refreshedToken: string): boolean {
  if (!refreshSessionIsCurrent(token, epoch)) return false;
  persistToken(refreshedToken);
  return true;
}

function abortError(): Error {
  const error = new Error('request cancelled');
  error.name = 'AbortError';
  return error;
}

function waitForRefresh(
  promise: Promise<RefreshResult>,
  signal?: AbortSignal,
): Promise<RefreshResult> {
  if (!signal) return promise;
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise((resolve, reject) => {
    const abort = () => {
      cleanup();
      reject(abortError());
    };
    const cleanup = () => signal.removeEventListener('abort', abort);
    signal.addEventListener('abort', abort, { once: true });
    promise.then(
      (result) => {
        cleanup();
        resolve(result);
      },
      (error: unknown) => {
        cleanup();
        reject(error instanceof Error ? error : new Error(String(error)));
      },
    );
  });
}

async function tryRefreshToken(token: string | null, epoch: symbol): Promise<RefreshResult> {
  if (!token) {
    return {
      kind: 'rejected',
      error: new ApiError(-1, i18n.t('common.unauthorized', 'Unauthorized'), 401),
    };
  }
  if (!refreshSessionIsCurrent(token, epoch)) return supersededRefresh();
  if (refreshInFlight?.token === token && refreshInFlight.epoch === epoch) {
    return refreshInFlight.promise;
  }

  const refreshID = Symbol('token-refresh');
  const promise: Promise<RefreshResult> = (async () => {
    const controller = new AbortController();
    const timeout = globalThis.setTimeout(() => controller.abort(), REFRESH_TIMEOUT_MS);
    try {
      const res = await fetch(`${BASE_URL}/api/v1/auth/refresh`, {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token}`, 'Content-Type': 'application/json' },
        signal: controller.signal,
      });
      if (!refreshSessionIsCurrent(token, epoch)) return supersededRefresh();
      if (!res.ok) {
        const error = await refreshResponseError(res);
        if (!refreshSessionIsCurrent(token, epoch)) return supersededRefresh();
        return refreshFailure(error);
      }

      let json: ApiResponse<{ token: string }>;
      try {
        json = await res.json() as ApiResponse<{ token: string }>;
      } catch {
        if (!refreshSessionIsCurrent(token, epoch)) return supersededRefresh();
        return {
          kind: 'retryable',
          error: new ApiError(-1, i18n.t('common.server_error', { status: res.status }), res.status),
        };
      }
      if (!refreshSessionIsCurrent(token, epoch)) return supersededRefresh();
      if (json.code !== 0 || !json.data?.token) {
        return refreshFailure(new ApiError(json.code, json.message, res.status));
      }
      const refreshedToken = json.data.token;
      if (!replaceRefreshedToken(token, epoch, refreshedToken)) return supersededRefresh();
      return { kind: 'refreshed', token: refreshedToken };
    } catch {
      if (!refreshSessionIsCurrent(token, epoch)) return supersededRefresh();
      return {
        kind: 'retryable',
        error: new ApiError(-1, i18n.t('common.network_error'), 0),
      };
    } finally {
      globalThis.clearTimeout(timeout);
      if (refreshInFlight?.id === refreshID) refreshInFlight = null;
    }
  })();
  refreshInFlight = { token, epoch, id: refreshID, promise };

  return promise;
}

async function awaitRefreshToken(
  token: string | null,
  epoch: symbol,
  signal?: AbortSignal,
): Promise<RefreshResult> {
  if (signal?.aborted) throw abortError();
  return waitForRefresh(tryRefreshToken(token, epoch), signal);
}

function invalidateSession(expectedToken: string | null, expectedEpoch: symbol): boolean {
  if (!expectedToken || accessToken !== expectedToken || sessionEpoch !== expectedEpoch) return false;
  setToken(null);
  if (typeof window !== 'undefined') window.location.href = '/login';
  return true;
}

function assertRequestSession(requestEpoch: SessionIdentity) {
  if (sessionEpoch !== requestEpoch) throw sessionSupersededError();
}

// 统一响应处理
async function handleResponse<T>(
  res: Response,
  requestToken: string | null,
  requestEpoch: symbol,
): Promise<T> {
  assertRequestSession(requestEpoch);
  let json: ApiResponse<T>;
  try {
    json = await res.json();
  } catch (err) {
    assertRequestSession(requestEpoch);
    if (isAbortError(err)) throw err;
    throw new ApiError(-1, i18n.t('common.server_error', { status: res.status }), res.status);
  }
  assertRequestSession(requestEpoch);

  if (json.code !== 0) {
    if (res.status === 401) {
      invalidateSession(requestToken, requestEpoch);
    }
    throw new ApiError(json.code, json.message, res.status);
  }

  return json.data;
}

// 执行 fetch 请求
function isAbortError(err: unknown): boolean {
  return typeof err === 'object'
    && err !== null
    && 'name' in err
    && (err as { name?: unknown }).name === 'AbortError';
}

async function doFetch(
  url: string,
  init: RequestInit,
  requestEpoch: SessionIdentity,
): Promise<Response> {
  let response: Response;
  try {
    response = await fetch(url, init);
  } catch (err) {
    assertRequestSession(requestEpoch);
    if (isAbortError(err)) {
      throw err;
    }
    throw new ApiError(-1, i18n.t('common.network_error'), 0);
  }
  assertRequestSession(requestEpoch);
  return response;
}

// 统一请求方法
async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  params?: QueryParams,
  options?: RequestOptions,
): Promise<T> {
  const initialToken = accessToken;
  const requestEpoch = sessionEpoch;
  let proactiveRefresh: RefreshResult | null = null;
  // 过期前 30 分钟自动刷新
  if (initialToken && tokenExpiresWithin(initialToken, 1800)) {
    proactiveRefresh = await awaitRefreshToken(initialToken, requestEpoch, options?.signal);
    if (proactiveRefresh.kind === 'rejected') {
      invalidateSession(initialToken, requestEpoch);
      throw proactiveRefresh.error;
    }
    if (proactiveRefresh.kind === 'superseded') throw proactiveRefresh.error;
  }

  const requestToken = proactiveRefresh?.kind === 'refreshed'
    ? proactiveRefresh.token
    : initialToken;
  assertRequestSession(requestEpoch);

  const url = new URL(`${BASE_URL}${path}`, window.location.origin);

  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== '') {
        url.searchParams.set(key, String(value));
      }
    });
  }

  // 给 GET 请求自动附加浏览器时区，后端用它计算"今天 / 7 天"等边界以及解析
  // YYYY-MM-DD 形式的 start_date / end_date。调用方显式提供的 tz 不会被覆盖。
  if (method === 'GET' && !url.searchParams.has('tz')) {
    const tz = browserTimezone();
    if (tz) {
      url.searchParams.set('tz', tz);
    }
  }

  const res = await doFetch(url.toString(), {
    method,
    headers: buildHeaders(true, requestToken),
    body: body ? JSON.stringify(body) : undefined,
    signal: options?.signal,
  }, requestEpoch);

  // 401 时尝试刷新 token 并重试一次
  if (res.status === 401 && requestToken) {
    // 另一请求已刷新了同一逻辑会话时，直接用新 Token 重试；旧 401 不能清掉新 Token。
    if (accessToken !== requestToken) {
      const retryToken = accessToken;
      if (!retryToken) return handleResponse<T>(res, requestToken, requestEpoch);
      const retryRes = await doFetch(url.toString(), {
        method,
        headers: buildHeaders(true, retryToken),
        body: body ? JSON.stringify(body) : undefined,
        signal: options?.signal,
      }, requestEpoch);
      return handleResponse<T>(retryRes, retryToken, requestEpoch);
    }
    // 新 Token 已经被服务端拒绝时无需再次刷新，按明确 401 结束会话。
    if (proactiveRefresh?.kind === 'refreshed') {
      return handleResponse<T>(res, requestToken, requestEpoch);
    }

    const refreshResult = proactiveRefresh
      ?? await awaitRefreshToken(requestToken, requestEpoch, options?.signal);
    if (refreshResult.kind === 'refreshed') {
      const retryToken = refreshResult.token;
      const retryRes = await doFetch(url.toString(), {
        method,
        headers: buildHeaders(true, retryToken),
        body: body ? JSON.stringify(body) : undefined,
        signal: options?.signal,
      }, requestEpoch);
      return handleResponse<T>(retryRes, retryToken, requestEpoch);
    }
    if (refreshResult.kind === 'rejected') invalidateSession(requestToken, requestEpoch);
    throw refreshResult.error;
  }

  return handleResponse<T>(res, requestToken, requestEpoch);
}

// API 错误类
export class ApiError extends Error {
  constructor(
    public code: number,
    message: string,
    public httpStatus: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export class SessionSupersededError extends Error {
  constructor() {
    super('request belongs to a superseded session');
    this.name = 'SessionSupersededError';
  }
}

// 导出快捷方法
export function get<T>(path: string, params?: QueryParams, options?: RequestOptions): Promise<T> {
  return request<T>('GET', path, undefined, params, options);
}

export function post<T>(path: string, body?: unknown, options?: RequestOptions): Promise<T> {
  return request<T>('POST', path, body, undefined, options);
}

export function put<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PUT', path, body);
}

export function del<T>(path: string): Promise<T> {
  return request<T>('DELETE', path);
}

export function patch<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PATCH', path, body);
}

// 文件上传（multipart/form-data）
export async function upload<T>(
  path: string,
  formData: FormData,
  options?: RequestOptions,
): Promise<T> {
  const url = new URL(`${BASE_URL}${path}`, window.location.origin);
  let requestToken = accessToken;
  const requestEpoch = sessionEpoch;
  const send = (token: string | null) => doFetch(url.toString(), {
    method: 'POST',
    headers: buildHeaders(false, token),
    body: formData,
    signal: options?.signal,
  }, requestEpoch);

  let res = await send(requestToken);
  if (res.status === 401 && requestToken) {
    if (accessToken !== requestToken) {
      requestToken = accessToken;
      if (!requestToken) return handleResponse<T>(res, requestToken, requestEpoch);
      res = await send(requestToken);
    } else {
      const refreshResult = await awaitRefreshToken(requestToken, requestEpoch, options?.signal);
      if (refreshResult.kind === 'refreshed') {
        requestToken = refreshResult.token;
        res = await send(requestToken);
      } else {
        if (refreshResult.kind === 'rejected') invalidateSession(requestToken, requestEpoch);
        throw refreshResult.error;
      }
    }
  }

  return handleResponse<T>(res, requestToken, requestEpoch);
}
