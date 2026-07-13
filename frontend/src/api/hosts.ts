import { get, post, put, del, pageQuery, type PageParams } from './client';
import type { Host, HostCreatePayload } from '../types/infra';

export const hostsApi = {
  list: (page?: PageParams) => get<Host[]>(`/hosts${pageQuery(page)}`),
  get: (id: string) => get<Host>(`/hosts/${id}`),
  create: (data: HostCreatePayload) => post<Host>('/hosts', data),
  update: (id: string, data: HostCreatePayload) => put<Host>(`/hosts/${id}`, data),
  delete: (id: string) => del(`/hosts/${id}`),
};
