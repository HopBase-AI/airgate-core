import { get, post, put, del, upload } from './client';
import type {
  BlogPostResp, CreateBlogPostReq, UpdateBlogPostReq, BlogUploadResp,
  PageReq, PagedData,
} from '../types';

export interface BlogListParams extends PageReq {
  status?: 'draft' | 'published';
  lang?: string;
}

export const blogApi = {
  list: (params?: BlogListParams) =>
    get<PagedData<BlogPostResp>>('/api/v1/admin/blog/posts', params),
  get: (id: number) => get<BlogPostResp>(`/api/v1/admin/blog/posts/${id}`),
  create: (data: CreateBlogPostReq) => post<BlogPostResp>('/api/v1/admin/blog/posts', data),
  update: (id: number, data: UpdateBlogPostReq) =>
    put<BlogPostResp>(`/api/v1/admin/blog/posts/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/admin/blog/posts/${id}`),
  upload: (file: File) => {
    const fd = new FormData();
    fd.append('file', file);
    return upload<BlogUploadResp>('/api/v1/admin/blog/upload', fd);
  },
};
