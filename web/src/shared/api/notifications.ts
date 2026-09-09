import { get, post } from './client';
import type {
  MarkNotificationsReadReq,
  MarkNotificationsReadResp,
  PagedData,
  UserNotificationListParams,
  UserNotificationResp,
  UserNotificationUnreadCountResp,
} from '../types';

type NotificationRequestOptions = {
  signal?: AbortSignal;
};

// 站内个人通知（额度预警 / 余额预警 / 系统消息）：当前登录身份（含成员账号）自己的通知
export const notificationsApi = {
  listMine: (params?: UserNotificationListParams, options?: NotificationRequestOptions) =>
    get<PagedData<UserNotificationResp>>('/api/v1/notifications/me', params, options),
  unreadCount: (options?: NotificationRequestOptions) =>
    get<UserNotificationUnreadCountResp>('/api/v1/notifications/me/unread-count', undefined, options),
  markRead: (data: MarkNotificationsReadReq) =>
    post<MarkNotificationsReadResp>('/api/v1/notifications/me/read', data),
};
