import { get, post, put, del } from './client';
import type { Host } from '../types/infra';

export const hostsApi = {
  list: () => get<Host[]>('/hosts'),
  get: (id: string) => get<Host>(`/hosts/${id}`),
  create: (data: Partial<Host>) => post<Host>('/hosts', data),
  update: (id: string, data: Partial<Host>) => put<Host>(`/hosts/${id}`, data),
  delete: (id: string) => del(`/hosts/${id}`),
};
