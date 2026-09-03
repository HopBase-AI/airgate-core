import { get, post, put, del } from './client';
import type { EntryCodeResp, CreateEntryCodeReq, UpdateEntryCodeReq } from '../types';

export const entryCodesApi = {
  list: () => get<EntryCodeResp[]>('/api/v1/admin/entry-codes'),
  create: (data: CreateEntryCodeReq) => post<EntryCodeResp>('/api/v1/admin/entry-codes', data),
  update: (code: string, data: UpdateEntryCodeReq) =>
    put<EntryCodeResp>(`/api/v1/admin/entry-codes/${code}`, data),
  delete: (code: string) => del<void>(`/api/v1/admin/entry-codes/${code}`),
};
