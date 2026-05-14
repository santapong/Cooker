import { get, post, put, del } from './client';
import type { Host, HostCreatePayload } from '../types/infra';

export const hostsApi = {
  list: () => get<Host[]>('/hosts'),
  get: (id: string) => get<Host>(`/hosts/${id}`),
  create: (data: HostCreatePayload) => post<Host>('/hosts', data),
  update: (id: string, data: HostCreatePayload) => put<Host>(`/hosts/${id}`, data),
  delete: (id: string) => del(`/hosts/${id}`),
};
