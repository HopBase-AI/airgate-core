// Only explicit authentication rejections may destroy a browser session.
// Transport failures and server-side outages must remain retryable.
export function isExplicitSessionRejection(httpStatus: number): boolean {
  return httpStatus === 401;
}

export function isExplicitRefreshRejection(httpStatus: number): boolean {
  return httpStatus === 401 || httpStatus === 403;
}
