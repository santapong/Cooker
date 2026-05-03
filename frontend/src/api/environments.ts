import { get, post, put, del } from './client';
import type { Environment } from '../types/environment';

export interface SecretMeta {
  key: string;
  updatedAt?: string;
}

export const environmentsApi = {
  list: () => get<Environment[]>('/environments'),
  create: (data: Partial<Environment>) => post<Environment>('/environments', data),
  update: (id: string, data: Partial<Environment>) =>
    put<Environment>(`/environments/${id}`, data),
  delete: (id: string) => del(`/environments/${id}`),

  // Secrets — all admin+MFA gated on the backend.
  revealSecret: (envId: string, key: string) =>
    get<{ key: string; value: string }>(
      `/environments/${envId}/secrets/${encodeURIComponent(key)}`,
    ),
  putSecret: (envId: string, key: string, value: string) =>
    put<{ status: string }>(
      `/environments/${envId}/secrets/${encodeURIComponent(key)}`,
      { value },
    ),
  deleteSecret: (envId: string, key: string) =>
    del(`/environments/${envId}/secrets/${encodeURIComponent(key)}`),
  promoteSecrets: (envId: string, toEnvironmentId: string) =>
    post<{ status: string; promoted: string[] }>(
      `/environments/${envId}/secrets/promote`,
      { toEnvironmentId },
    ),
};
