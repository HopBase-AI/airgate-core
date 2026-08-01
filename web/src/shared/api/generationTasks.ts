import { get } from './client';
import type {
  GenerationTaskResp,
  GenerationTaskStatus,
  GenerationTaskSummaryResp,
  PageReq,
  PagedData,
} from '../types';

export interface GenerationTaskQuery extends PageReq {
  status?: GenerationTaskStatus;
  kind?: string;
  plugin_id?: string;
  task_type?: string;
  user_id?: number;
}

export const generationTasksApi = {
  list: (params: GenerationTaskQuery) =>
    get<PagedData<GenerationTaskResp>>('/api/v1/admin/generation-tasks', params),
  summary: () =>
    get<GenerationTaskSummaryResp>('/api/v1/admin/generation-tasks/summary'),
};
