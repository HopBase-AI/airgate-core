import { ApiError } from '../../shared/api/client';

// A transient network/server failure must not destroy a valid stored session.
// The API client already retries refreshable 401s before surfacing the error.
export function shouldInvalidateStoredSession(error: unknown): boolean {
  return error instanceof ApiError && error.httpStatus === 401;
}
